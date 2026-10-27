package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

// confluenceReady 配置缺失时明确拒绝，不影响已有功能。
func (h *Handler) confluenceReady(c *gin.Context) bool {
	if h.ConfluenceObjects == nil || h.Config.Confluence.Cookie == "" {
		c.JSON(503, ErrorResponse{Error: "Confluence export requires CONFLUENCE_BASE_URL/COOKIE and RUSTFS configuration"})
		return false
	}
	return true
}

// confluenceTask 统一校验用户范围与任务 ID。
func (h *Handler) confluenceTask(c *gin.Context) (domain.ConfluenceTask, bool) {
	if !h.confluenceReady(c) || !validateID(c, c.Param("task_id"), "task_id") {
		return domain.ConfluenceTask{}, false
	}
	task, err := mysql.GetConfluenceTask(c.Request.Context(), h.DB, c.Param("task_id"), c.GetString("user_id"))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(404, ErrorResponse{Error: "Confluence task not found"})
		return task, false
	}
	if err != nil {
		c.JSON(500, ErrorResponse{Error: fmt.Sprintf("read Confluence task: %v", err)})
		return task, false
	}
	return task, true
}

// CreateConfluenceTaskRequest 指定部署实例中的源页面。
type CreateConfluenceTaskRequest struct {
	URL string `json:"url" binding:"required"`
}

// SelectConfluenceExportRequest 空节点 ID 表示明确选择全部叶子页。
type SelectConfluenceExportRequest struct {
	NodeID *string `json:"node_id" binding:"required"`
}

// SelectConfluenceExport 目录就绪后选择子树，后台冻结范围并开始阶段二。
// @Summary 选择 Confluence Markdown 导出范围
// @Tags Confluence
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param task_id path string true "任务 ID"
// @Param body body SelectConfluenceExportRequest true "节点 ID；空字符串表示全部"
// @Success 202 {object} domain.ConfluenceTask
// @Router /confluence-tasks/{task_id}/export [post]
func (h *Handler) SelectConfluenceExport(c *gin.Context) {
	task, ok := h.confluenceTask(c)
	if !ok {
		return
	}
	var request SelectConfluenceExportRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	service := worker.ConfluenceWorker{DB: h.DB, Objects: h.ConfluenceObjects}
	updated, err := service.SelectExport(c.Request.Context(), task, *request.NodeID, h.AsynqClient)
	if err != nil {
		status := 502
		if errors.Is(err, worker.ErrInvalidConfluenceSelection) {
			status = 400
		}
		if errors.Is(err, worker.ErrConfluenceSelectionUnavailable) {
			status = 409
		}
		c.JSON(status, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(202, updated)
}

// CreateConfluenceTask 接受 URL 后立即入队，文档读取在 Worker 执行。
// @Summary 生成 Confluence 目录树
// @Tags Confluence
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param body body CreateConfluenceTaskRequest true "Confluence 页面地址"
// @Success 202 {object} domain.ConfluenceTask
// @Router /confluence-tasks [post]
func (h *Handler) CreateConfluenceTask(c *gin.Context) {
	if !h.confluenceReady(c) {
		return
	}
	var request CreateConfluenceTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	if err := worker.ValidateConfluenceURL(h.Config.Confluence.BaseURL, request.URL); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	now := time.Now().UTC()
	task := domain.ConfluenceTask{ID: uuid.NewString(), UserID: c.GetString("user_id"), URL: request.URL, Status: "queued", Stage: "tree", CreatedAt: now, UpdatedAt: now}
	if err := mysql.CreateConfluenceTask(c.Request.Context(), h.DB, task); err != nil {
		c.JSON(500, ErrorResponse{Error: fmt.Sprintf("create Confluence task: %v", err)})
		return
	}
	if err := worker.EnqueueConfluence(c.Request.Context(), h.AsynqClient, task.ID, task.UserID); err != nil {
		recordErr := mysql.FailConfluenceTask(c.Request.Context(), h.DB, task.ID, task.UserID, err.Error())
		c.JSON(503, ErrorResponse{Error: fmt.Sprintf("enqueue Confluence task: %v", errors.Join(err, recordErr))})
		return
	}
	c.JSON(http.StatusAccepted, task)
}

// ListConfluenceTasks 列出当前用户最近结果。
// @Summary 当前用户最近导出任务
// @Tags Confluence
// @Security JWTAuth
// @Produce json
// @Success 200 {object} object{data=[]domain.ConfluenceTask}
// @Router /confluence-tasks [get]
func (h *Handler) ListConfluenceTasks(c *gin.Context) {
	if !h.confluenceReady(c) {
		return
	}
	tasks, err := mysql.ListConfluenceTasks(c.Request.Context(), h.DB, c.GetString("user_id"))
	if err != nil {
		c.JSON(500, ErrorResponse{Error: fmt.Sprintf("list Confluence tasks: %v", err)})
		return
	}
	c.JSON(200, gin.H{"data": tasks})
}

// GetConfluenceTask 提供手动和自动进度刷新。
// @Summary 查询导出进度
// @Tags Confluence
// @Security JWTAuth
// @Produce json
// @Param task_id path string true "任务 ID"
// @Success 200 {object} domain.ConfluenceTask
// @Router /confluence-tasks/{task_id} [get]
func (h *Handler) GetConfluenceTask(c *gin.Context) {
	if task, ok := h.confluenceTask(c); ok {
		c.JSON(200, task)
	}
}

// StopConfluenceTask 运行中的任务确认取消后才标记 stopped。
// @Summary 停止导出并保留检查点
// @Tags Confluence
// @Security JWTAuth
// @Produce json
// @Param task_id path string true "任务 ID"
// @Success 202 {object} domain.ConfluenceTask
// @Router /confluence-tasks/{task_id}/stop [post]
func (h *Handler) StopConfluenceTask(c *gin.Context) {
	task, ok := h.confluenceTask(c)
	if !ok {
		return
	}
	next := "stopping"
	if task.Status == "queued" {
		next = "stopped"
	} else if task.Status != "running" {
		c.JSON(409, ErrorResponse{Error: "only queued/running tasks can be stopped"})
		return
	}
	changed, err := mysql.TransitionConfluenceTask(c.Request.Context(), h.DB, task.ID, task.UserID, task.Status, next)
	if err != nil {
		c.JSON(500, ErrorResponse{Error: err.Error()})
		return
	}
	if !changed {
		c.JSON(409, ErrorResponse{Error: "task changed; refresh progress"})
		return
	}
	task.Status = next
	c.JSON(202, task)
}

// StartConfluenceTask 继续冻结检查点，不重建已导出的内容。
// @Summary 继续停止或失败的导出
// @Tags Confluence
// @Security JWTAuth
// @Produce json
// @Param task_id path string true "任务 ID"
// @Success 202 {object} domain.ConfluenceTask
// @Router /confluence-tasks/{task_id}/start [post]
func (h *Handler) StartConfluenceTask(c *gin.Context) {
	task, ok := h.confluenceTask(c)
	if !ok {
		return
	}
	if task.Stage == "selection" {
		c.JSON(409, ErrorResponse{Error: "select a subtree or export all pages before starting Markdown export"})
		return
	}
	if task.Status != "stopped" && task.Status != "failed" {
		c.JSON(409, ErrorResponse{Error: "only stopped/failed tasks can be resumed"})
		return
	}
	changed, err := mysql.TransitionConfluenceTask(c.Request.Context(), h.DB, task.ID, task.UserID, task.Status, "queued")
	if err != nil {
		c.JSON(500, ErrorResponse{Error: err.Error()})
		return
	}
	if !changed {
		c.JSON(409, ErrorResponse{Error: "task changed; refresh progress"})
		return
	}
	if err = worker.EnqueueConfluence(c.Request.Context(), h.AsynqClient, task.ID, task.UserID); err != nil {
		recordErr := mysql.FailConfluenceTask(c.Request.Context(), h.DB, task.ID, task.UserID, err.Error())
		c.JSON(503, ErrorResponse{Error: errors.Join(err, recordErr).Error()})
		return
	}
	task.Status = "queued"
	task.Error = ""
	c.JSON(202, task)
}

// ConfluenceTree 返回最新成功抓取的完整目录，不暴露对象存储凭据。
// @Summary 查看完整导出目录
// @Tags Confluence
// @Security JWTAuth
// @Produce json
// @Param task_id path string true "任务 ID"
// @Success 200 {object} domain.ConfluenceTree
// @Router /confluence-tasks/{task_id}/tree [get]
func (h *Handler) ConfluenceTree(c *gin.Context) {
	task, ok := h.confluenceTask(c)
	if !ok {
		return
	}
	if task.Stage == "tree" {
		c.JSON(409, ErrorResponse{Error: "page tree is not ready"})
		return
	}
	tree, err := worker.ReadConfluencePlan(c.Request.Context(), h.ConfluenceObjects, task)
	if err != nil {
		c.JSON(502, ErrorResponse{Error: err.Error()})
		return
	}
	c.JSON(200, tree)
}

// ConfluenceFile 仅允许读取已计入完成量的页面和其受限附件。
// @Summary 读取已发布 Markdown 或附件
// @Tags Confluence
// @Security JWTAuth
// @Produce octet-stream
// @Param task_id path string true "任务 ID"
// @Param page_id path string true "页面 ID"
// @Param name query string true "page.md 或 assets/哈希附件名"
// @Success 200 {string} string
// @Router /confluence-tasks/{task_id}/pages/{page_id}/file [get]
func (h *Handler) ConfluenceFile(c *gin.Context) {
	task, ok := h.confluenceTask(c)
	if !ok {
		return
	}
	pageID := c.Param("page_id")
	name := c.Query("name")
	if !regexp.MustCompile(`^\d+$`).MatchString(pageID) || !worker.ValidConfluenceFile(name) {
		c.JSON(400, ErrorResponse{Error: "invalid page ID or export filename"})
		return
	}
	if task.Stage != "export" && task.Stage != "markdown" {
		c.JSON(404, ErrorResponse{Error: "page not exported"})
		return
	}
	tree, err := worker.ReadConfluencePlan(c.Request.Context(), h.ConfluenceObjects, task)
	if err != nil {
		c.JSON(502, ErrorResponse{Error: err.Error()})
		return
	}
	published := false
	for index, page := range tree.Pages {
		if page.ID == pageID && index < task.Completed {
			published = true
			break
		}
	}
	if !published {
		c.JSON(404, ErrorResponse{Error: "page not exported"})
		return
	}
	data, err := h.ConfluenceObjects.Get(c.Request.Context(), worker.ConfluencePrefix(task)+pageID+"/"+name)
	if err != nil {
		c.JSON(502, ErrorResponse{Error: err.Error()})
		return
	}
	sendConfluenceFile(c, name, data)
}

// sendConfluenceFile 为文档与附件设置相同的私有缓存和沙箱策略。
func sendConfluenceFile(c *gin.Context, name string, data []byte) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	kind := "application/octet-stream"
	if name == "page.md" {
		kind = "text/plain; charset=utf-8"
	} else {
		detected := http.DetectContentType(data)
		if strings.HasPrefix(detected, "image/") && detected != "image/svg+xml" {
			kind = detected
		} else {
			c.Header("Content-Disposition", "attachment")
		}
	}
	c.Data(200, kind, data)
}
