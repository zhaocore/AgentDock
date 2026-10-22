package api

import (
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/rustfs"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

// libraryReady 文件库只依赖私有对象存储，不要求实时 Confluence Cookie。
func (h *Handler) libraryReady(c *gin.Context) bool {
	if h.ConfluenceLibrary == nil {
		c.JSON(503, ErrorResponse{Error: "Confluence library requires RUSTFS configuration"})
		return false
	}
	return true
}

func libraryError(c *gin.Context, err error) {
	status := http.StatusBadGateway
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) || errors.Is(err, rustfs.ErrArchiveTooLarge) {
		status = http.StatusRequestEntityTooLarge
	} else if errors.Is(err, sql.ErrNoRows) {
		status = 404
	} else if errors.Is(err, worker.ErrInvalidArchive) {
		status = 400
	} else if errors.Is(err, worker.ErrLibraryChanged) {
		status = 409
	}
	if status == http.StatusBadGateway {
		logLibraryFailure(c, err)
	}
	c.JSON(status, ErrorResponse{Error: err.Error()})
}

// logLibraryFailure 记录失败原因，不包含鉴权头和正文。
func logLibraryFailure(c *gin.Context, err error) {
	slog.ErrorContext(c.Request.Context(), "Confluence 文件库请求失败",
		"request_id", c.GetString("request_id"), "method", c.Request.Method,
		"path", c.Request.URL.Path, "space_id", c.Query("space_id"),
		"directory", c.Query("path"), "error", err)
}

// validLibraryExportQuery 统一下载与预检的目录参数校验。
func validLibraryExportQuery(c *gin.Context) bool {
	directory, spaceID := c.Query("path"), c.Query("space_id")
	if (directory != "" && (spaceID == "" || !domain.ValidConfluencePath(directory))) || (spaceID != "" && !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(spaceID)) {
		c.JSON(400, ErrorResponse{Error: "invalid Space or directory"})
		return false
	}
	return true
}

// CheckConfluenceArchive 完整验证导出到丢弃流，让下载错误先返回页面。
// @Summary 检查 Confluence ZIP 下载
// @Tags Confluence Library
// @Security JWTAuth
// @Param space_id query string false "Space ID"
// @Param path query string false "Space 内目录路径"
// @Success 204
// @Router /confluence-library/archive-check [get]
func (h *Handler) CheckConfluenceArchive(c *gin.Context) {
	if !h.libraryReady(c) || !validLibraryExportQuery(c) {
		return
	}
	if err := h.ConfluenceLibrary.Export(c.Request.Context(), c.GetString("user_id"), c.Query("space_id"), c.Query("path"), io.Discard); err != nil {
		libraryError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// GetConfluenceLibrary 查看按用户隔离的多 Space 文件库。
// @Summary 查看 Confluence 文件库
// @Tags Confluence Library
// @Security JWTAuth
// @Produce json
// @Success 200 {object} domain.ConfluenceLibrary
// @Router /confluence-library [get]
func (h *Handler) GetConfluenceLibrary(c *gin.Context) {
	if !h.libraryReady(c) {
		return
	}
	catalog, err := h.ConfluenceLibrary.Catalog(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		libraryError(c, err)
		return
	}
	c.JSON(200, catalog)
}

// ExportConfluenceLibrary 导出整库、Space 或选定子目录。
// @Summary 下载 Confluence 目录 ZIP
// @Tags Confluence Library
// @Security JWTAuth
// @Produce application/zip
// @Param space_id query string false "Space ID；留空导出全部"
// @Param path query string false "Space 内目录路径"
// @Success 200 {file} file
// @Router /confluence-library/archive [get]
func (h *Handler) ExportConfluenceLibrary(c *gin.Context) {
	if !h.libraryReady(c) || !validLibraryExportQuery(c) {
		return
	}
	c.Header("Content-Disposition", `attachment; filename="confluence-library.zip"`)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Type", "application/zip")
	if err := h.ConfluenceLibrary.Export(c.Request.Context(), c.GetString("user_id"), c.Query("space_id"), c.Query("path"), c.Writer); err != nil {
		if c.Writer.Written() {
			// 已开始传输时不能追加 JSON，否则会把错误伪装成完整 ZIP。
			logLibraryFailure(c, err)
			c.Abort()
			return
		}
		c.Writer.Header().Del("Content-Disposition")
		c.Writer.Header().Del("Content-Type")
		libraryError(c, err)
	}
}

// PreviewConfluenceImport 分块暂存并校验 ZIP，返回逐页覆盖检查，不发布目录。
// @Summary 上传 ZIP 并检查目录合并
// @Tags Confluence Library
// @Security JWTAuth
// @Accept application/zip
// @Produce json
// @Param body body string true "本模块导出的 ZIP 字节"
// @Success 200 {object} domain.ConfluenceImportReport
// @Router /confluence-library/imports [post]
func (h *Handler) PreviewConfluenceImport(c *gin.Context) {
	if !h.libraryReady(c) {
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, domain.ConfluenceZIPLimit)
	defer body.Close()
	report, err := h.ConfluenceLibrary.Preview(c.Request.Context(), c.GetString("user_id"), body)
	if err != nil {
		libraryError(c, err)
		return
	}
	c.JSON(200, report)
}

// ApplyConfluenceImport 仅将检查过且基线未变化的目录合并发布。
// @Summary 确认合并 Confluence ZIP
// @Tags Confluence Library
// @Security JWTAuth
// @Produce json
// @Param import_id path string true "导入检查 ID"
// @Success 200 {object} domain.ConfluenceImportReport
// @Router /confluence-library/imports/{import_id}/apply [post]
func (h *Handler) ApplyConfluenceImport(c *gin.Context) {
	if !h.libraryReady(c) || !validateID(c, c.Param("import_id"), "import_id") {
		return
	}
	report, err := h.ConfluenceLibrary.Apply(c.Request.Context(), c.GetString("user_id"), c.Param("import_id"))
	if err != nil {
		libraryError(c, err)
		return
	}
	c.JSON(200, report)
}

// ConfluenceLibraryFile 通过稳定 Space/页面身份读取已发布内容。
// @Summary 读取文件库 Markdown 或附件
// @Tags Confluence Library
// @Security JWTAuth
// @Param space_id path string true "Space ID"
// @Param page_id path string true "页面 ID"
// @Param name query string true "page.md 或 assets/哈希附件名"
// @Success 200 {string} string
// @Router /confluence-library/spaces/{space_id}/pages/{page_id}/file [get]
func (h *Handler) ConfluenceLibraryFile(c *gin.Context) {
	if !h.libraryReady(c) {
		return
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(c.Param("space_id")) || !regexp.MustCompile(`^\d+$`).MatchString(c.Param("page_id")) || !worker.ValidConfluenceFile(c.Query("name")) {
		c.JSON(400, ErrorResponse{Error: "invalid library file identity"})
		return
	}
	data, err := h.ConfluenceLibrary.File(c.Request.Context(), c.GetString("user_id"), c.Param("space_id"), c.Param("page_id"), c.Query("name"))
	if err != nil {
		libraryError(c, err)
		return
	}
	sendConfluenceFile(c, c.Query("name"), data)
}
