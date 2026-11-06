package middleware

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// AccountAuth 每次请求校验账户状态，登记后查询防止遗漏冻结竞态。
func AccountAuth(db *sql.DB, activity *account.Activity) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, done := activity.Begin(c.Request.Context(), c.GetString("user_id"))
		defer done()
		c.Request = c.Request.WithContext(ctx)
		u, err := mysql.GetAccount(ctx, db, c.GetString("user_id"))
		if err != nil {
			if err == sql.ErrNoRows {
				c.AbortWithStatusJSON(401, gin.H{"error": "account no longer exists"})
			} else {
				slog.Error("account authentication failed", "error", err)
				c.AbortWithStatusJSON(503, gin.H{"error": "account authentication unavailable"})
			}
			return
		}
		if ctx.Err() != nil || u.Status != "active" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "account is disabled or being deleted"})
			return
		}
		_, isToken := c.Get(APITokenIDKey)
		if !isToken && c.GetInt64("auth_version") != u.AuthVersion {
			c.AbortWithStatusJSON(401, gin.H{"error": "authentication revoked; sign in again"})
			return
		}
		c.Set("is_admin", u.IsAdmin)
		c.Set("actor_id", u.ID)
		path := c.FullPath()
		if isToken && strings.Contains(path, "/auth/") {
			c.AbortWithStatusJSON(403, gin.H{"error": "API tokens cannot access account authentication routes"})
			return
		}
		if u.MustChangePassword && (isToken || (!strings.HasSuffix(path, "/auth/me") && !strings.HasSuffix(path, "/auth/change-password"))) {
			c.AbortWithStatusJSON(403, gin.H{"error": "password change required", "code": "password_change_required"})
			return
		}
		c.Next()
	}
}

// RequireAdministrator 管理接口只接受当前管理员的 JWT。
func RequireAdministrator() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, token := c.Get(APITokenIDKey)
		if token || !c.GetBool("is_admin") {
			c.AbortWithStatusJSON(403, gin.H{"error": "administrator JWT required"})
			return
		}
		c.Next()
	}
}
