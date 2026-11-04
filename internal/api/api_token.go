package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// tokenAlphabet 是令牌本体的 base62 字符集。
const tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// tokenSecretLength 是 gk_ 前缀之后的随机字符数（~190 bit 熵）。
const tokenSecretLength = 32

// generateAPIToken 生成令牌本体、展示前缀与存储哈希。
func generateAPIToken() (full, prefix, hash string, err error) {
	secret := make([]byte, tokenSecretLength)
	for i := range secret {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(tokenAlphabet))))
		if err != nil {
			return "", "", "", fmt.Errorf("generate api token entropy: %w", err)
		}
		secret[i] = tokenAlphabet[index.Int64()]
	}
	full = "gk_" + string(secret)
	prefix = full[:12]
	sum := sha256.Sum256([]byte(full))
	return full, prefix, hex.EncodeToString(sum[:]), nil
}

// @Summary 创建 API 令牌
// @Description 创建面向外部调用方的令牌；密钥本体仅在响应中出现一次。仅 JWT 可调用。
// @Tags APITokens
// @Security JWTAuth
// @Accept json
// @Produce json
// @Param body body CreateAPITokenRequest true "令牌信息"
// @Success 201 {object} CreateAPITokenResponse
// @Failure 400 {object} ErrorResponse
// @Router /v1/api-tokens [post]
func (h *Handler) CreateAPIToken(c *gin.Context) {
	if denyAPIToken(c) {
		return
	}
	userID := c.GetString("user_id")

	var req CreateAPITokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	if len(req.Label) == 0 || len(req.Label) > 200 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "label must be 1-200 characters"})
		return
	}

	// 范围：资源全局共享 / 按用户隔离，仅保留 "all" 全范围
	if req.KBScope != "all" {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "kb_scope must be \"all\""})
		return
	}

	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "expires_at must be in the future"})
		return
	}

	full, prefix, hash, err := generateAPIToken()
	if err != nil {
		log.Printf("generate api token: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create api token"})
		return
	}

	kbID := domain.APITokenAllKBs
	if req.KBScope != "all" {
		kbID = req.KBScope
	}
	token := domain.APIToken{
		ID:          uuid.New().String(),
		TenantID:    userID,
		Label:       req.Label,
		TokenPrefix: prefix,
		Scopes:      []domain.APITokenScope{{KBID: kbID, CanWrite: req.CanWrite}},
		ExpiresAt:   req.ExpiresAt,
	}
	if err := mysql.CreateAPIToken(c.Request.Context(), h.DB, token, hash); err != nil {
		log.Printf("create api token: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to create api token"})
		return
	}
	token.Scopes[0].KBID = req.KBScope

	c.JSON(http.StatusCreated, CreateAPITokenResponse{APIToken: token, Token: full})
}

// @Summary 列出 API 令牌
// @Description 返回租户全部令牌（含已撤销），不含密钥本体。仅 JWT 可调用。
// @Tags APITokens
// @Security JWTAuth
// @Produce json
// @Success 200 {object} ListAPITokensResponse
// @Router /v1/api-tokens [get]
func (h *Handler) ListAPITokens(c *gin.Context) {
	if denyAPIToken(c) {
		return
	}
	tokens, err := mysql.ListAPITokens(c.Request.Context(), h.DB, c.GetString("user_id"))
	if err != nil {
		log.Printf("list api tokens: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to list api tokens"})
		return
	}
	c.JSON(http.StatusOK, ListAPITokensResponse{Data: tokens})
}

// @Summary 撤销 API 令牌
// @Tags APITokens
// @Security JWTAuth
// @Param token_id path string true "令牌 ID"
// @Success 204
// @Failure 404 {object} ErrorResponse
// @Router /v1/api-tokens/{token_id} [delete]
func (h *Handler) RevokeAPIToken(c *gin.Context) {
	if denyAPIToken(c) {
		return
	}
	tokenID := c.Param("token_id")
	if !validateID(c, tokenID, "token_id") {
		return
	}
	if err := mysql.RevokeAPIToken(c.Request.Context(), h.DB, c.GetString("user_id"), tokenID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, ErrorResponse{Error: "resource not found"})
			return
		}
		log.Printf("revoke api token: %v", err)
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: "failed to revoke api token"})
		return
	}
	c.Status(http.StatusNoContent)
}

// --- 请求/响应结构 ---

// CreateAPITokenRequest 创建令牌请求。
type CreateAPITokenRequest struct {
	Label     string     `json:"label" binding:"required"`
	KBScope   string     `json:"kb_scope" binding:"required"`
	CanWrite  bool       `json:"can_write"`
	ExpiresAt *time.Time `json:"expires_at"`
}

// CreateAPITokenResponse 创建响应，Token 仅此一次返回。
type CreateAPITokenResponse struct {
	domain.APIToken
	Token string `json:"token"`
}

// ListAPITokensResponse 令牌列表响应。
type ListAPITokensResponse struct {
	Data []domain.APIToken `json:"data"`
}
