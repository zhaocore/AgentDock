// Package middleware 提供 gin 中间件：JWT 鉴权、限流、日志。
package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// userLimiter 记录一个限流键（用户或 IP）的令牌桶与最近活跃时间。
type userLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimit 返回按用户限流的 gin 中间件。
// limitPerMinute 为每用户每分钟允许的请求数；limitPerMinute <= 0 视为显式关闭限流。
// 必须挂在 JWTAuth 之后：从上下文读取 user_id 作为限流键；
// user_id 缺失（中间件被单独挂载时）退化为按客户端 IP 限流。
func RateLimit(limitPerMinute int) gin.HandlerFunc {
	var mu sync.Mutex
	limiters := make(map[string]*userLimiter)

	getLimiter := func(key string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		// 惰性清理：限流键超过 10000 个时，删除 10 分钟未活跃的条目，避免内存无限增长
		if len(limiters) > 10000 {
			now := time.Now()
			for k, ul := range limiters {
				if now.Sub(ul.lastSeen) > 10*time.Minute {
					delete(limiters, k)
				}
			}
		}

		ul, ok := limiters[key]
		if !ok {
			ul = &userLimiter{
				// 桶容量 limitPerMinute，稳态补充速率 limitPerMinute/60 每秒
				limiter:  rate.NewLimiter(rate.Limit(float64(limitPerMinute)/60.0), limitPerMinute),
				lastSeen: time.Now(),
			}
			limiters[key] = ul
		}
		ul.lastSeen = time.Now()
		return ul.limiter
	}

	return func(c *gin.Context) {
		if limitPerMinute <= 0 {
			c.Next()
			return
		}

		key := c.GetString("user_id")
		if key == "" {
			key = c.ClientIP()
		}

		if !getLimiter(key).Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}

		c.Next()
	}
}
