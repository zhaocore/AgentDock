package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/zhaocore/agentdock/internal/api/middleware"
	"github.com/zhaocore/agentdock/internal/domain"
)

// apiTokenScopesFromContext 取当前请求的令牌范围；JWT 请求返回 nil。
func apiTokenScopesFromContext(c *gin.Context) []domain.APITokenScope {
	scopes, ok := c.Get(middleware.APITokenScopesKey)
	if !ok {
		return nil
	}
	typed, ok := scopes.([]domain.APITokenScope)
	if !ok {
		return nil
	}
	return typed
}

// scopeAllowsWrite 判断范围列表是否允许写操作。
// "all"（存储层归一化）与全零哨兵均表示全部资源；写要求 can_write。
// 仓库为全局共享、Agent 与会话按用户隔离，范围不再细分到单个资源。
func scopeAllowsWrite(scopes []domain.APITokenScope) bool {
	for _, scope := range scopes {
		if scope.KBID == "all" || scope.KBID == domain.APITokenAllKBs {
			if scope.CanWrite {
				return true
			}
		}
	}
	return false
}

// authorizeAPITokenWrite 校验令牌请求的写权限，未授权时写 403 并返回 false。
// JWT 请求（上下文无令牌身份）直接放行。
func authorizeAPITokenWrite(c *gin.Context) bool {
	scopes := apiTokenScopesFromContext(c)
	if scopes == nil {
		return true
	}
	if !scopeAllowsWrite(scopes) {
		c.JSON(http.StatusForbidden, gin.H{"error": "api token scope does not allow this operation"})
		return false
	}
	return true
}

// RequireTokenWrite 写路由中间件：API 令牌请求要求 can_write，JWT 请求放行。
func RequireTokenWrite() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authorizeAPITokenWrite(c) {
			c.Abort()
			return
		}
		c.Next()
	}
}

// denyAPIToken 拒绝令牌请求访问令牌管理类接口，防止令牌自我管理形成提权链。
func denyAPIToken(c *gin.Context) bool {
	if _, ok := c.Get(middleware.APITokenIDKey); ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "api tokens cannot access this endpoint"})
		return true
	}
	return false
}
