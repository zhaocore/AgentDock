package api

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/zhaocore/agentdock/docs/swagger"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/agent"
	"github.com/zhaocore/agentdock/internal/api/middleware"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/rustfs"
	"github.com/zhaocore/agentdock/internal/worker"
)

// InstanceManager Agent 实例管理器接口（*agent.Manager 实现）。
type InstanceManager interface {
	Start(ctx context.Context, inst domain.AgentInstance, agent domain.Agent) error
	Stop(ctx context.Context, instanceID string) error
	IsRunning(instanceID string) bool
	SendMessage(ctx context.Context, inst domain.AgentInstance, agent domain.Agent, history []domain.Message, userMessage string) (string, json.RawMessage, error)
	ShutdownAll()
}

// Handler 持有所有外部依赖，供 API 处理器使用。
type Handler struct {
	Activity          *account.Activity
	Crawl             *worker.CrawlWorker
	ConfluenceObjects rustfs.Objects
	ConfluenceLibrary *worker.ConfluenceLibrary
	Config            *config.Config
	DB                *sql.DB
	AsynqClient       worker.Enqueuer // 仓库任务入队客户端
	Instances         InstanceManager // Agent 实例管理器
	Runtime           agent.Runtime   // Agent 构建/执行运行时
	RedisHealth       func(ctx context.Context) error
	SyncAgents        func(ctx context.Context) error
}

// NewRouter 创建并配置 gin 路由。
func NewRouter(h *Handler) *gin.Engine {
	if h.Activity == nil {
		h.Activity = account.NewActivity()
	}
	if h.Config.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		panic(err)
	}
	r.Use(middleware.RequestID())

	// Swagger 文档 UI：生成物由 make swagger 产出（docs/swagger），不手改
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// 全局中间件
	r.Use(middleware.CORSMiddleware())

	base := r.Group(h.Config.Server.BasePath)

	// 健康检查（无需鉴权）
	base.GET("/health", h.Health)

	// 公开路由
	auth := base.Group("/auth")
	auth.Use(middleware.RateLimit(h.Config.Auth.RateLimit))
	{
		auth.POST("/register", h.Register)
		auth.POST("/login", h.Login)
		auth.GET("/registration", h.RegistrationStatus)
	}

	// 需要鉴权的路由
	authed := base.Group("")
	authed.Use(middleware.CombinedAuth(h.Config.Auth.JWTSecret, h.DB))
	authed.Use(middleware.AccountAuth(h.DB, h.Activity))
	authed.Use(middleware.RateLimit(h.Config.Auth.RateLimit))

	// 写操作（API 令牌要求 can_write）
	writes := authed.Group("", RequireTokenWrite())
	{
		writes.POST("/crawl-tasks", h.CreateCrawlTask)
		writes.PUT("/crawl-tasks/:task_id", h.UpdateCrawlTask)
		writes.POST("/crawl-tasks/:task_id/runs", h.RunCrawlTask)
		writes.POST("/confluence-library/imports", h.PreviewConfluenceImport)
		writes.POST("/confluence-library/imports/:import_id/apply", h.ApplyConfluenceImport)
		writes.POST("/confluence-tasks", h.CreateConfluenceTask)
		writes.POST("/confluence-tasks/:task_id/start", h.StartConfluenceTask)
		writes.POST("/confluence-tasks/:task_id/export", h.SelectConfluenceExport)
		writes.POST("/confluence-tasks/:task_id/stop", h.StopConfluenceTask)
		writes.POST("/repositories", h.CreateRepository)
		writes.POST("/repositories/:repo_id/sync", h.SyncRepository)
		writes.DELETE("/repositories/:repo_id", h.DeleteRepository)
		writes.POST("/agents", h.CreateAgent)
		writes.PUT("/agents/:agent_id", h.UpdateAgent)
		writes.DELETE("/agents/:agent_id", h.DeleteAgent)
		writes.POST("/agents/:agent_id/instances", h.CreateInstance)
		writes.POST("/instances/:instance_id/start", h.StartInstance)
		writes.POST("/instances/:instance_id/stop", h.StopInstance)
		writes.POST("/instances/:instance_id/sessions", h.CreateSession)
		writes.POST("/sessions/:session_id/messages", h.SendMessage)
		writes.DELETE("/sessions/:session_id", h.DeleteSession)
	}
	{
		authed.GET("/confluence-library", h.GetConfluenceLibrary)
		authed.GET("/crawl-profiles", h.CrawlProfiles)
		authed.GET("/crawl-tasks", h.ListCrawlTasks)
		authed.GET("/crawl-tasks/:task_id", h.GetCrawlTask)
		authed.GET("/crawl-tasks/:task_id/runs", h.ListCrawlRuns)
		authed.GET("/crawl-runs/:run_id/report", h.CrawlReport)
		authed.GET("/crawl-runs/:run_id/markdown", h.CrawlMarkdown)
		authed.GET("/confluence-library/archive", h.ExportConfluenceLibrary)
		authed.GET("/confluence-library/archive-check", h.CheckConfluenceArchive)
		authed.GET("/confluence-library/spaces/:space_id/pages/:page_id/file", h.ConfluenceLibraryFile)
		authed.GET("/confluence-tasks", h.ListConfluenceTasks)
		authed.GET("/confluence-tasks/:task_id", h.GetConfluenceTask)
		authed.GET("/confluence-tasks/:task_id/tree", h.ConfluenceTree)
		authed.GET("/confluence-tasks/:task_id/pages/:page_id/file", h.ConfluenceFile)
		// 用户
		authed.POST("/auth/change-password", h.ChangePassword)
		authed.GET("/auth/me", h.CurrentUser)

		// 仓库（全局共享资源）
		authed.GET("/repositories", h.ListRepositories)
		authed.GET("/repositories/:repo_id", h.GetRepository)

		// 公共预设全用户可读；私有定义与实例仍按 owner 隔离
		authed.GET("/agents", h.ListAgents)
		authed.GET("/agents/:agent_id", h.GetAgent)
		authed.GET("/agents/:agent_id/instances", h.ListInstances)

		// 会话历史（用户隔离）
		authed.GET("/sessions", h.ListSessions)
		authed.GET("/sessions/:session_id/messages", h.ListSessionMessages)

		// API 令牌管理（仅 JWT 鉴权可用，handler 内部拒绝令牌请求）
		authed.POST("/api-tokens", h.CreateAPIToken)
		authed.GET("/api-tokens", h.ListAPITokens)
		authed.DELETE("/api-tokens/:token_id", h.RevokeAPIToken)
	}

	h.registerAdministration(authed)
	return r
}
