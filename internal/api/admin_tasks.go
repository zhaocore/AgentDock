package api

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// AdminStopCrawl 停用配置并取消现有轮次，确认退出后才收敛终态。
func (h *Handler) AdminStopCrawl(c *gin.Context) {
	user, id := c.GetString("user_id"), c.Param("task_id")
	if !validateID(c, id, "task_id") {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.accountService().StopCrawlTask(ctx, user, id); err != nil {
		adminError(c, err)
		return
	}
	c.JSON(200, gin.H{"status": "stopped"})
}
func (h *Handler) AdminDeleteCrawl(c *gin.Context) {
	if !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	if err := h.accountService().DeleteCrawlTask(c.Request.Context(), c.GetString("user_id"), c.Param("task_id")); err != nil {
		adminError(c, err)
		return
	}
	c.Status(204)
}
func (h *Handler) AdminDeleteConfluence(c *gin.Context) {
	if !validateID(c, c.Param("task_id"), "task_id") {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	if err := h.accountService().DeleteConfluenceTask(ctx, c.GetString("user_id"), c.Param("task_id")); err != nil {
		adminError(c, err)
		return
	}
	c.Status(204)
}

// AdminDeleteLibrary 删除目录可见性，保留任务原始检查点与共享对象。
func (h *Handler) AdminDeleteLibrary(c *gin.Context) {
	if err := mysql.DeleteManagedLibrary(c.Request.Context(), h.DB, c.GetString("user_id")); err != nil {
		adminError(c, err)
		return
	}
	c.Status(204)
}
