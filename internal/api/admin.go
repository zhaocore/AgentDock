package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/api/middleware"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/worker"
	"golang.org/x/crypto/bcrypt"
)

// AdminAccountListResponse 账户列表只包含非敏感元数据。
type AdminAccountListResponse struct {
	Data []domain.Account `json:"data"`
}

// registerAdministration 显式列举跨用户允许路径，不提供任意代理或身份模拟。
func (h *Handler) registerAdministration(authed *gin.RouterGroup) {
	admin := authed.Group("/admin", middleware.RequireAdministrator(), h.adminAudit())
	admin.GET("/users", h.AdminUsers)
	admin.POST("/users", h.AdminCreateUser)
	admin.GET("/settings", h.AdminSettings)
	admin.PUT("/settings", h.AdminSetRegistration)
	users := admin.Group("/users/:user_id", h.adminTarget())
	users.PUT("/status", h.AdminStatus)
	users.POST("/password", h.AdminResetPassword)
	users.GET("/deletion-preview", h.AdminDeletionPreview)
	users.POST("/delete", h.AdminDeleteUser)
	users.GET("/deletion", h.AdminDeletion)
	users.GET("/api-tokens", h.ListAPITokens)
	users.DELETE("/api-tokens/:token_id", h.RevokeAPIToken)
	users.GET("/confluence-library", h.GetConfluenceLibrary)
	users.GET("/confluence-library/archive", h.ExportConfluenceLibrary)
	users.GET("/confluence-library/archive-check", h.CheckConfluenceArchive)
	users.GET("/confluence-library/spaces/:space_id/pages/:page_id/file", h.ConfluenceLibraryFile)
	users.DELETE("/confluence-library", h.AdminDeleteLibrary)
	users.GET("/confluence-tasks", h.ListConfluenceTasks)
	users.GET("/confluence-tasks/:task_id", h.GetConfluenceTask)
	users.GET("/confluence-tasks/:task_id/tree", h.ConfluenceTree)
	users.GET("/confluence-tasks/:task_id/pages/:page_id/file", h.ConfluenceFile)
	users.POST("/confluence-tasks/:task_id/stop", h.StopConfluenceTask)
	users.DELETE("/confluence-tasks/:task_id", h.AdminDeleteConfluence)
	users.GET("/crawl-tasks", h.ListCrawlTasks)
	users.GET("/crawl-tasks/:task_id", h.GetCrawlTask)
	users.GET("/crawl-tasks/:task_id/runs", h.ListCrawlRuns)
	users.POST("/crawl-tasks/:task_id/stop", h.AdminStopCrawl)
	users.DELETE("/crawl-tasks/:task_id", h.AdminDeleteCrawl)
	users.GET("/crawl-runs/:run_id/report", h.CrawlReport)
	users.GET("/crawl-runs/:run_id/markdown", h.CrawlMarkdown)
}

// adminTarget 保留操作者身份，仅在已列举的管理路由替换资源查询范围。
func (h *Handler) adminTarget() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := c.Param("user_id")
		if !validateID(c, user, "user_id") {
			c.Abort()
			return
		}
		c.Set("user_id", user)
		c.Next()
	}
}

// adminAudit 在执行前保存审计起点，存储故障时拒绝开始管理操作。
func (h *Handler) adminAudit() gin.HandlerFunc {
	return func(c *gin.Context) {
		target := c.Param("user_id")
		if target == "" {
			target = "administration"
		} else if !validateID(c, target, "user_id") {
			c.Abort()
			return
		}
		action := c.Request.Method + " " + c.FullPath()
		actor := c.GetString("actor_id")
		if err := mysql.Audit(c.Request.Context(), h.DB, actor, target, action, "started"); err != nil {
			adminError(c, err)
			c.Abort()
			return
		}
		c.Next()
		result := "succeeded"
		if c.Writer.Status() >= 400 {
			result = "failed"
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
		defer cancel()
		if err := mysql.Audit(ctx, h.DB, actor, target, action, result); err != nil {
			slog.Error("administrator audit completion failed", "actor_id", actor, "target_id", target, "error", err)
		}
	}
}

// RegistrationStatus 公开注册开关，不暴露管理员身份。
// @Summary 注册开关状态
// @Tags Auth
// @Success 200 {object} object{registration_enabled=bool}
// @Router /v1/auth/registration [get]
func (h *Handler) RegistrationStatus(c *gin.Context) {
	s, err := mysql.GetAdministration(c.Request.Context(), h.DB)
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"registration_enabled": s.RegistrationEnabled})
}

// @Summary 管理员账户列表
// @Tags Administration
// @Security JWTAuth
// @Success 200 {object} object{data=[]domain.Account}
// @Router /v1/admin/users [get]
func (h *Handler) AdminUsers(c *gin.Context) {
	users, err := mysql.ListAccounts(c.Request.Context(), h.DB)
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, AdminAccountListResponse{Data: users})
}

// @Summary 管理员注册设置
// @Tags Administration
// @Security JWTAuth
// @Success 200 {object} domain.Administration
// @Router /v1/admin/settings [get]
func (h *Handler) AdminSettings(c *gin.Context) {
	s, err := mysql.GetAdministration(c.Request.Context(), h.DB)
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, s)
}

// @Summary 设置公开注册开关
// @Tags Administration
// @Security JWTAuth
// @Accept json
// @Param body body object{registration_enabled=bool} true "是否允许注册"
// @Success 200 {object} object{registration_enabled=bool}
// @Router /v1/admin/settings [put]
func (h *Handler) AdminSetRegistration(c *gin.Context) {
	var r struct {
		Enabled *bool `json:"registration_enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	if err := mysql.SetRegistration(c.Request.Context(), h.DB, c.GetString("actor_id"), *r.Enabled); err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"registration_enabled": *r.Enabled})
}

// @Summary 管理员创建临时密码账户
// @Tags Administration
// @Security JWTAuth
// @Accept json
// @Param body body RegisterRequest true "邮箱与临时密码"
// @Success 201 {object} object{id=string}
// @Router /v1/admin/users [post]
func (h *Handler) AdminCreateUser(c *gin.Context) {
	var r RegisterRequest
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		adminError(c, err)
		return
	}
	id, err := mysql.CreateAccount(c.Request.Context(), h.DB, c.GetString("actor_id"), r.Email, string(hash))
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(201, gin.H{"id": id})
}
func (h *Handler) accountService() *worker.AccountService {
	return &worker.AccountService{DB: h.DB, Activity: h.Activity, Instances: h.Instances, Library: h.ConfluenceLibrary}
}

// @Summary 禁用或启用普通账户
// @Tags Administration
// @Security JWTAuth
// @Accept json
// @Param user_id path string true "账户 ID"
// @Param body body object{status=string} true "active 或 disabled"
// @Success 200 {object} object{status=string}
// @Router /v1/admin/users/{user_id}/status [put]
func (h *Handler) AdminStatus(c *gin.Context) {
	var r struct {
		Status string `json:"status" binding:"required,oneof=active disabled"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.accountService().ChangeStatus(ctx, c.GetString("actor_id"), c.GetString("user_id"), r.Status); err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"status": r.Status})
}

// @Summary 重置普通账户临时密码并撤销旧身份
// @Tags Administration
// @Security JWTAuth
// @Accept json
// @Param user_id path string true "账户 ID"
// @Param body body object{password=string} true "新临时密码"
// @Success 200 {object} object{must_change_password=bool}
// @Router /v1/admin/users/{user_id}/password [post]
func (h *Handler) AdminResetPassword(c *gin.Context) {
	var r struct {
		Password string `json:"password" binding:"required,min=6,max=72"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	if c.GetString("user_id") == c.GetString("actor_id") {
		c.JSON(403, ErrorResponse{Error: "use change-password for administrator account"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
	if err != nil {
		adminError(c, err)
		return
	}
	if err = mysql.ResetAccountPassword(c.Request.Context(), h.DB, c.GetString("actor_id"), c.GetString("user_id"), string(hash)); err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"must_change_password": true})
}

// @Summary 预览账户删除与资源转移冲突
// @Tags Administration
// @Security JWTAuth
// @Param user_id path string true "账户 ID"
// @Success 200 {object} worker.AccountPreview
// @Router /v1/admin/users/{user_id}/deletion-preview [get]
func (h *Handler) AdminDeletionPreview(c *gin.Context) {
	if _, err := mysql.GetAccount(c.Request.Context(), h.DB, c.GetString("user_id")); err != nil {
		adminError(c, err)
		return
	}
	preview, _, err := h.accountService().Preview(c.Request.Context(), c.GetString("actor_id"), c.GetString("user_id"))
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, preview)
}

// @Summary 确认删除账户并转移资源
// @Tags Administration
// @Security JWTAuth
// @Accept json
// @Param user_id path string true "账户 ID"
// @Param body body object{digest=string} true "已检查的预览摘要"
// @Success 200 {object} object{stage=string}
// @Router /v1/admin/users/{user_id}/delete [post]
func (h *Handler) AdminDeleteUser(c *gin.Context) {
	var r struct {
		Digest string `json:"digest" binding:"required,len=64,hexadecimal"`
	}
	if err := c.ShouldBindJSON(&r); err != nil {
		c.JSON(400, ErrorResponse{Error: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()
	if err := h.accountService().Delete(ctx, c.GetString("actor_id"), c.GetString("user_id"), r.Digest); err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"stage": "completed"})
}

// @Summary 查看持久化账户删除状态
// @Tags Administration
// @Security JWTAuth
// @Param user_id path string true "账户 ID"
// @Success 200 {object} domain.AccountDeletion
// @Router /v1/admin/users/{user_id}/deletion [get]
func (h *Handler) AdminDeletion(c *gin.Context) {
	job, _, err := mysql.GetAccountDeletion(c.Request.Context(), h.DB, c.GetString("user_id"))
	if err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, job)
}

func adminError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, sql.ErrNoRows):
		status = 404
	case errors.Is(err, mysql.ErrProtectedAdministrator):
		status = 403
	case isUniqueViolation(err):
		status = 409
	case errors.Is(err, context.DeadlineExceeded):
		status = 409
	}
	slog.Error("administrator operation failed", "request_id", c.GetString("request_id"), "error", err)
	c.JSON(status, ErrorResponse{Error: err.Error()})
}
