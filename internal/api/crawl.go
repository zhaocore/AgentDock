package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/worker"
)

// CrawlTaskRequest 描述任务配置，凭据值不通过用户 API 提交。
type CrawlTaskRequest struct {
	Name           string   `json:"name" binding:"required,max=200"`
	AgentID        string   `json:"agent_id" binding:"required,uuid"`
	ProfileID      string   `json:"profile_id"`
	ProfileIDs     []string `json:"profile_ids" binding:"max=100"`
	Hops           int      `json:"hops" binding:"min=0,max=5"`
	IncludePattern string   `json:"include_pattern" binding:"max=2048"`
	URLs           []string `json:"urls" binding:"required,min=1,max=20"`
	Enabled        *bool    `json:"enabled" binding:"required"`
}

// crawlReady 缺少采集或存储配置时返回明确的功能不可用状态。
func (h *Handler) crawlReady(c *gin.Context) bool {
	if h.Crawl == nil {
		c.JSON(503, ErrorResponse{Error: "crawl requires RustFS configuration"})
		return false
	}
	return true
}

// crawlError 保持用户隔离、占用冲突和依赖失败的 HTTP 语义。
func crawlError(c *gin.Context, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, sql.ErrNoRows) {
		status = 404
	}
	if errors.Is(err, mysql.ErrCrawlBusy) || errors.Is(err, mysql.ErrCrawlDuplicate) {
		status = 409
	}
	c.JSON(status, ErrorResponse{Error: err.Error()})
}

// CrawlProfiles 只返回当前用户获授权登录配置的非敏感描述。
// @Summary 爬虫登录配置
// @Tags Crawl
// @Security JWTAuth
// @Success 200 {object} object
// @Router /crawl-profiles [get]
func (h *Handler) CrawlProfiles(c *gin.Context) {
	if !h.crawlReady(c) {
		return
	}
	type profile struct {
		ID     string `json:"id"`
		Origin string `json:"origin"`
	}
	list := []profile{}
	for _, p := range h.Crawl.Profiles {
		if _, err := config.CrawlAccess([]config.CrawlProfile{p}, c.GetString("user_id"), p.ID, []string{p.Origin + "/"}); err == nil {
			list = append(list, profile{p.ID, p.Origin})
		}
	}
	c.JSON(200, gin.H{"data": list})
}

// ListCrawlTasks 返回当前用户的任务配置。
// @Summary 爬虫任务列表
// @Tags Crawl
// @Security JWTAuth
// @Success 200 {object} object{data=[]domain.CrawlTask}
// @Router /crawl-tasks [get]
func (h *Handler) ListCrawlTasks(c *gin.Context) {
	if !h.crawlReady(c) {
		return
	}
	list, err := mysql.ListCrawlTasks(c.Request.Context(), h.DB, c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": list})
}

// CreateCrawlTask 创建每日九点执行的任务。
// @Summary 创建爬虫任务
// @Tags Crawl
// @Security JWTAuth
// @Accept json
// @Param body body CrawlTaskRequest true "任务配置"
// @Success 201 {object} domain.CrawlTask
// @Router /crawl-tasks [post]
func (h *Handler) CreateCrawlTask(c *gin.Context) {
	t, ok := h.bindCrawlTask(c, uuid.NewString())
	if !ok {
		return
	}
	if !h.crawlTaskAccess(c, t) {
		return
	}
	if err := mysql.CreateCrawlTask(c.Request.Context(), h.DB, t); err != nil {
		crawlError(c, err)
		return
	}
	saved, err := mysql.GetCrawlTask(c.Request.Context(), h.DB, t.ID, t.UserID)
	if err != nil {
		crawlError(c, err)
		return
	}
	c.JSON(201, saved)
}

// UpdateCrawlTask 不改变历史运行快照，停用仅阻止未来自动执行。
// @Summary 编辑爬虫任务
// @Tags Crawl
// @Security JWTAuth
// @Accept json
// @Param task_id path string true "任务 ID"
// @Param body body CrawlTaskRequest true "任务配置"
// @Success 200 {object} domain.CrawlTask
// @Router /crawl-tasks/{task_id} [put]
func (h *Handler) UpdateCrawlTask(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	current, err := mysql.GetCrawlTask(c.Request.Context(), h.DB, c.Param("task_id"), c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	t, ok := h.bindCrawlTask(c, c.Param("task_id"))
	if !ok {
		return
	}
	// 仅停用原配置不需要已失效的执行权限，其他编辑仍完整校验。
	if t.Enabled || t.Name != current.Name || t.AgentID != current.AgentID || t.ProfileID != current.ProfileID || !slices.Equal(t.URLs, current.URLs) || !slices.Equal(t.ProfileIDs, current.ProfileIDs) || t.Hops != current.Hops || t.IncludePattern != current.IncludePattern {
		if !h.crawlTaskAccess(c, t) {
			return
		}
	}
	if err := mysql.UpdateCrawlTask(c.Request.Context(), h.DB, t); err != nil {
		crawlError(c, err)
		return
	}
	h.GetCrawlTask(c)
}

// bindCrawlTask 只解析外部配置，执行权限由调用者单独校验。
func (h *Handler) bindCrawlTask(c *gin.Context, id string) (domain.CrawlTask, bool) {
	var request CrawlTaskRequest
	if !h.crawlReady(c) {
		return domain.CrawlTask{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(400, ErrorResponse{Error: "crawl task requires name, agent_id, 1–20 URLs, enabled, hops 0–5 and valid profile_ids"})
		return domain.CrawlTask{}, false
	}
	user := c.GetString("user_id")
	if strings.TrimSpace(request.Name) == "" {
		c.JSON(400, ErrorResponse{Error: "crawl task name cannot be blank"})
		return domain.CrawlTask{}, false
	}
	return domain.CrawlTask{ID: id, UserID: user, Name: strings.TrimSpace(request.Name), AgentID: request.AgentID, ProfileID: request.ProfileID, ProfileIDs: append([]string{}, request.ProfileIDs...), Hops: request.Hops, IncludePattern: request.IncludePattern, URLs: request.URLs, Enabled: *request.Enabled}, true
}

// crawlTaskAccess 校验来源授权与 Agent 可访问性。
func (h *Handler) crawlTaskAccess(c *gin.Context, task domain.CrawlTask) bool {
	if _, err := config.CrawlTaskProfiles(h.Crawl.Profiles, task); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return false
	}
	if _, err := mysql.GetAgent(c.Request.Context(), h.DB, task.AgentID, task.UserID); err != nil {
		crawlError(c, err)
		return false
	}
	return true
}

// GetCrawlTask 查询当前用户任务详情。
// @Summary 爬虫任务详情
// @Tags Crawl
// @Security JWTAuth
// @Param task_id path string true "任务 ID"
// @Success 200 {object} domain.CrawlTask
// @Router /crawl-tasks/{task_id} [get]
func (h *Handler) GetCrawlTask(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	t, err := mysql.GetCrawlTask(c.Request.Context(), h.DB, c.Param("task_id"), c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	c.JSON(200, t)
}

// RunCrawlTask 手动创建独立运行，不能绕过登录配置授权。
// @Summary 执行爬虫任务
// @Tags Crawl
// @Security JWTAuth
// @Param task_id path string true "任务 ID"
// @Success 202 {object} domain.CrawlRun
// @Router /crawl-tasks/{task_id}/runs [post]
func (h *Handler) RunCrawlTask(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	t, err := mysql.GetCrawlTask(c.Request.Context(), h.DB, c.Param("task_id"), c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	if _, err = config.CrawlTaskProfiles(h.Crawl.Profiles, t); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	r, err := h.Crawl.Dispatch(c.Request.Context(), t.ID, t.UserID, "manual", "")
	if err != nil {
		crawlError(c, err)
		return
	}
	c.JSON(202, r)
}

// ListCrawlRuns 列出任务最近一百轮状态和进度。
// @Summary 爬虫运行历史
// @Tags Crawl
// @Security JWTAuth
// @Param task_id path string true "任务 ID"
// @Success 200 {object} object{data=[]domain.CrawlRun}
// @Router /crawl-tasks/{task_id}/runs [get]
func (h *Handler) ListCrawlRuns(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	user := c.GetString("user_id")
	if _, err := mysql.GetCrawlTask(c.Request.Context(), h.DB, c.Param("task_id"), user); err != nil {
		crawlError(c, err)
		return
	}
	list, err := mysql.ListCrawlRuns(c.Request.Context(), h.DB, c.Param("task_id"), user)
	if err != nil {
		crawlError(c, err)
		return
	}
	c.JSON(200, gin.H{"data": list})
}

// CrawlReport 仅在成功发布后读取受当前用户保护的报告对象。
// @Summary 读取或下载采集报告
// @Tags Crawl
// @Security JWTAuth
// @Param run_id path string true "运行 ID"
// @Success 200 {string} string
// @Router /crawl-runs/{run_id}/report [get]
func (h *Handler) CrawlReport(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("run_id"), "run_id") {
		return
	}
	r, err := mysql.GetCrawlRun(c.Request.Context(), h.DB, c.Param("run_id"), c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	if r.Status != "succeeded" && r.Status != "partial" {
		c.JSON(409, ErrorResponse{Error: "crawl report is not published"})
		return
	}
	data, err := h.Crawl.Objects.Get(c.Request.Context(), worker.CrawlPrefix(r)+"report.md")
	if err != nil {
		crawlError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s.md"`, r.ID))
	}
	c.Data(200, "text/plain; charset=utf-8", data)
}

// CrawlMarkdown 下载完整采集文档，Agent 失败不影响已完成的原文转换。
// @Summary 下载完整采集 Markdown
// @Tags Crawl
// @Security JWTAuth
// @Param run_id path string true "运行 ID"
// @Success 200 {string} string
// @Router /crawl-runs/{run_id}/markdown [get]
func (h *Handler) CrawlMarkdown(c *gin.Context) {
	if !h.crawlReady(c) || !validateID(c, c.Param("run_id"), "run_id") {
		return
	}
	r, err := mysql.GetCrawlRun(c.Request.Context(), h.DB, c.Param("run_id"), c.GetString("user_id"))
	if err != nil {
		crawlError(c, err)
		return
	}
	if r.Stage != "report" && r.Stage != "publish" && r.Stage != "published" {
		c.JSON(409, ErrorResponse{Error: "crawl document capture is not complete"})
		return
	}
	document, err := h.Crawl.SourceMarkdown(c.Request.Context(), r)
	if err != nil {
		crawlError(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="sources-%s.md"`, r.ID))
	c.Data(200, "text/plain; charset=utf-8", []byte(document))
}
