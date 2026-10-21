// Package api 提供 gin HTTP 路由和处理器。
package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- 响应结构 ---

// ErrorResponse 统一错误响应。
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// PaginatedResponse 分页响应包装。
type PaginatedResponse struct {
	Data    any  `json:"data"`
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// NewPaginatedResponse 创建分页响应。
func NewPaginatedResponse(data any, total, limit, offset int) PaginatedResponse {
	return PaginatedResponse{
		Data:    data,
		Total:   total,
		Limit:   limit,
		Offset:  offset,
		HasMore: offset+limit < total,
	}
}

// parsePagination 解析通用分页参数。
// limit 从 query string 读取，默认 20，允许范围 [1, 100]；offset 默认 0，必须非负。
// 参数非法时返回错误，由调用方决定响应（不静默修正非法值）。
func parsePagination(c *http.Request) (int, int, error) {
	limit := 20
	if raw := c.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			return 0, 0, fmt.Errorf("limit must be an integer between 1 and 100, got %q", raw)
		}
		limit = n
	}

	offset := 0
	if raw := c.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer, got %q", raw)
		}
		offset = n
	}

	return limit, offset, nil
}

// HealthResponse 明确区分依赖的实时结果。
type HealthResponse struct {
	Status     string `json:"status"`
	MySQL      string `json:"mysql"`
	MySQLError string `json:"mysql_error"`
	Redis      string `json:"redis"`
	RedisError string `json:"redis_error"`
}

// healthStatus 将探测错误映射为明确状态，不把缺失探测器当成健康。
func healthStatus(err error) (string, string) {
	if err != nil {
		return "error", err.Error()
	}
	return "connected", ""
}

// Health 并行检测依赖，避免慢服务耗尽预算后误报其他服务失败。
// @Summary 检查 MySQL 与 Redis
// @Tags Health
// @Produce json
// @Success 200 {object} HealthResponse
// @Failure 503 {object} HealthResponse
// @Router /v1/health [get]
func (h *Handler) Health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	names := [2]string{"mysql", "redis"}
	probes := [2]func(context.Context) error{h.DB.PingContext, h.RedisHealth}
	var results [2]error
	var workers sync.WaitGroup
	workers.Add(len(probes))
	for index, probe := range probes {
		go func() {
			defer workers.Done()
			if probe == nil {
				results[index] = errors.New("health probe not configured")
				return
			}
			results[index] = probe(ctx)
		}()
	}
	workers.Wait()
	response := HealthResponse{Status: "ok"}
	response.MySQL, response.MySQLError = healthStatus(results[0])
	response.Redis, response.RedisError = healthStatus(results[1])
	status := http.StatusOK
	for index, err := range results {
		if err != nil {
			status = http.StatusServiceUnavailable
			response.Status = "degraded"
			slog.Warn("health probe failed", "component", names[index], "request_id", c.GetString("request_id"), "error", err)
		}
	}
	c.JSON(status, response)
}

// validateID 校验路径参数是 UUID 格式，非法时写 400 响应并返回 false。
func validateID(c *gin.Context, id, name string) bool {	if _, err := uuid.Parse(id); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: fmt.Sprintf("invalid %s", name)})
		return false
	}
	return true
}
