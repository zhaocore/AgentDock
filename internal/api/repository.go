package api

import (
	"database/sql"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

// validRepoTypes 合法仓库类型集合。
var validRepoTypes = map[domain.RepoType]bool{
	domain.RepoTypeFrontend: true,
	domain.RepoTypeBackend:  true,
	domain.RepoTypeRN:       true,
}

// CreateRepositoryRequest 添加仓库请求。
type CreateRepositoryRequest struct {
	Name     string          `json:"name" binding:"required"`
	GitURL   string          `json:"git_url" binding:"required,url"`
	RepoType domain.RepoType `json:"repo_type" binding:"required"`
	Branch   string          `json:"branch"`
}

// @Summary 添加仓库
// @Description 登记仓库并触发后台克隆与 codegraph 初始化。仓库为全局共享资源。写操作要求 API 令牌 can_write。
// @Tags Repositories
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param body body CreateRepositoryRequest true "仓库信息"
// @Success 201 {object} domain.Repository
// @Failure 400 {object} ErrorResponse
// @Failure 409 {object} ErrorResponse
// @Router /v1/repositories [post]
func (h *Handler) CreateRepository(c *gin.Context) {
	var req CreateRepositoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if !validRepoTypes[req.RepoType] {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "repo_type must be one of frontend, backend, rn"})
		return
	}
	branch := req.Branch
	if branch == "" {
		branch = "main"
	}

	repo := domain.Repository{
		ID:       uuid.New().String(),
		Name:     req.Name,
		GitURL:   req.GitURL,
		RepoType: req.RepoType,
		Branch:   branch,
	}
	// 同名冲突先行检查，给出明确 409
	if _, err := mysql.GetRepositoryByName(c.Request.Context(), h.DB, repo.Name); err == nil {
		c.JSON(http.StatusConflict, ErrorResponse{Error: "repository name already exists"})
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		log.Printf("check repository name: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create repository"})
		return
	}

	if err := mysql.CreateRepository(c.Request.Context(), h.DB, repo); err != nil {
		log.Printf("create repository: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create repository"})
		return
	}

	// 入队克隆任务；入队失败回滚记录，避免悬挂 pending
	if err := worker.EnqueueRepoClone(c.Request.Context(), h.AsynqClient, repo.ID); err != nil {
		log.Printf("enqueue repo clone: %v", err)
		_ = mysql.DeleteRepository(c.Request.Context(), h.DB, repo.ID)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to schedule repository clone"})
		return
	}

	repo.Status = domain.RepoStatusPending
	c.JSON(http.StatusCreated, repo)
}

// @Summary 仓库列表
// @Description 全局共享仓库分页列表，所有用户可见。
// @Tags Repositories
// @Security JWTAuth
// @Produce json
// @Param limit query int false "分页大小"
// @Param offset query int false "偏移"
// @Success 200 {object} PaginatedResponse
// @Failure 400 {object} ErrorResponse
// @Router /v1/repositories [get]
func (h *Handler) ListRepositories(c *gin.Context) {
	limit, offset, err := parsePagination(c.Request)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	list, total, err := mysql.ListRepositories(c.Request.Context(), h.DB, limit, offset)
	if err != nil {
		log.Printf("list repositories: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list repositories"})
		return
	}
	c.JSON(http.StatusOK, NewPaginatedResponse(list, total, limit, offset))
}

// @Summary 仓库详情
// @Tags Repositories
// @Security JWTAuth
// @Produce json
// @Param repo_id path string true "仓库 ID"
// @Success 200 {object} domain.Repository
// @Failure 404 {object} ErrorResponse
// @Router /v1/repositories/{repo_id} [get]
func (h *Handler) GetRepository(c *gin.Context) {
	repoID := c.Param("repo_id")
	if !validateID(c, repoID, "repo_id") {
		return
	}
	repo, err := mysql.GetRepository(c.Request.Context(), h.DB, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "repository not found"})
		return
	}
	if err != nil {
		log.Printf("get repository: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to get repository"})
		return
	}
	c.JSON(http.StatusOK, repo)
}

// @Summary 同步仓库
// @Description 触发 git fetch + reset 到远端分支（202 异步执行）。
// @Tags Repositories
// @Security JWTAuth
// @Param repo_id path string true "仓库 ID"
// @Success 202
// @Failure 404 {object} ErrorResponse
// @Router /v1/repositories/{repo_id}/sync [post]
func (h *Handler) SyncRepository(c *gin.Context) {
	repoID := c.Param("repo_id")
	if !validateID(c, repoID, "repo_id") {
		return
	}
	repo, err := mysql.GetRepository(c.Request.Context(), h.DB, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "repository not found"})
		return
	}
	if err != nil {
		log.Printf("get repository: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to sync repository"})
		return
	}
	if err := worker.EnqueueRepoSync(c.Request.Context(), h.AsynqClient, repo.ID); err != nil {
		log.Printf("enqueue repo sync: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to schedule repository sync"})
		return
	}
	c.Status(http.StatusAccepted)
}

// @Summary 删除仓库
// @Description 删除仓库记录并清理本地目录。
// @Tags Repositories
// @Security JWTAuth
// @Param repo_id path string true "仓库 ID"
// @Success 204
// @Failure 404 {object} ErrorResponse
// @Router /v1/repositories/{repo_id} [delete]
func (h *Handler) DeleteRepository(c *gin.Context) {
	repoID := c.Param("repo_id")
	if !validateID(c, repoID, "repo_id") {
		return
	}
	repo, err := mysql.GetRepository(c.Request.Context(), h.DB, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: "repository not found"})
		return
	}
	if err != nil {
		log.Printf("get repository: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete repository"})
		return
	}
	if err := mysql.DeleteRepository(c.Request.Context(), h.DB, repo.ID); err != nil {
		log.Printf("delete repository: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to delete repository"})
		return
	}
	// 本地目录清理异步执行；入队失败只记日志（记录已删，残留目录可手工清理）
	if err := worker.EnqueueRepoDelete(c.Request.Context(), h.AsynqClient, repo.ID, repo.LocalPath); err != nil {
		log.Printf("enqueue repo delete: %v", err)
	}
	c.Status(http.StatusNoContent)
}
