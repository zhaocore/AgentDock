// Package middleware 提供 gin 中间件：JWT / API 令牌鉴权、限流、日志。
package middleware

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

// APITokenPrefix 是 API 令牌的固定前缀，用于与 JWT 区分分发。
const APITokenPrefix = "gk_"

// apiTokenContextKeys 是令牌鉴权注入的上下文键。
const (
	// APITokenIDKey 令牌 ID；存在即表示本请求由令牌鉴权
	APITokenIDKey = "api_token_id"
	// APITokenScopesKey 令牌范围列表（[]domain.APITokenScope）
	APITokenScopesKey = "api_token_scopes"
)

// CombinedAuth 组合鉴权：Bearer 以 gk_ 开头走 API 令牌校验，否则走既有 JWT 校验。
// JWT 路径行为与 JWTAuth 完全一致，契约不变。
func CombinedAuth(secret string, pg *sql.DB) gin.HandlerFunc {
	jwtHandler := JWTAuth(secret)
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer "+APITokenPrefix) {
			apiKeyAuth(c, pg)
			return
		}
		jwtHandler(c)
	}
}

// apiKeyAuth 校验 API 令牌：按 SHA-256 哈希查库，拒绝撤销/过期令牌，
// 注入令牌身份并异步语义地更新 last_used_at（失败只记日志，不影响请求）。
func apiKeyAuth(c *gin.Context, pg *sql.DB) {
	tokenStr := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenStr)))
	token, err := mysql.GetAPITokenByHash(c.Request.Context(), pg, hash)
	if err == sql.ErrNoRows {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if err != nil {
		log.Printf("api token auth: %v", err)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if token.ExpiresAt != nil && !token.ExpiresAt.After(time.Now()) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
		return
	}
	// 令牌即租户身份：复用 user_id 键，业务路由的租户过滤无需感知鉴权方式
	c.Set("user_id", token.TenantID)
	c.Set(APITokenIDKey, token.ID)
	c.Set(APITokenScopesKey, token.Scopes)
	if err := mysql.TouchAPIToken(c.Request.Context(), pg, token.ID); err != nil {
		log.Printf("touch api token: %v", err)
	}
	c.Next()
}
