package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/worker"
)

// 导出失败必须在服务端留下可关联的原因，不记录请求凭据。
func TestConfluenceLibraryArchiveLogsFailure(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT .* FROM confluence_tasks WHERE user_id").WithArgs("owner").WillReturnError(errors.New("metadata query failed"))
	h := &Handler{ConfluenceLibrary: &worker.ConfluenceLibrary{DB: db}}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Set("user_id", "owner")
	c.Set("request_id", "archive-check")
	spaceID := domain.ConfluenceSpaceID("https://example.com", "TEAM")
	c.Request = httptest.NewRequest("GET", "/v1/confluence-library/archive?space_id="+spaceID+"&path=Root", nil)
	c.Request.Header.Set("Authorization", "Bearer private-test-token")
	h.ExportConfluenceLibrary(c)
	if response.Code != 502 || !strings.Contains(response.Body.String(), "metadata query failed") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
	var entry struct {
		Level     string `json:"level"`
		RequestID string `json:"request_id"`
		Method    string `json:"method"`
		Path      string `json:"path"`
		SpaceID   string `json:"space_id"`
		Directory string `json:"directory"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Level != "ERROR" || entry.RequestID != "archive-check" || entry.Method != "GET" || entry.Path != "/v1/confluence-library/archive" || entry.SpaceID != spaceID || entry.Directory != "Root" || entry.Error != "metadata query failed" || strings.Contains(logs.String(), "private-test-token") {
		t.Fatalf("unexpected log: %s", logs.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type libraryAPIObjects struct{ data map[string][]byte }

func (m *libraryAPIObjects) Get(_ context.Context, key string) ([]byte, error) {
	return m.data[key], nil
}
func (m *libraryAPIObjects) Put(_ context.Context, key string, data []byte, _ string) error {
	m.data[key] = bytes.Clone(data)
	return nil
}
func (m *libraryAPIObjects) List(_ context.Context, prefix string) ([]string, error) {
	keys := []string{}
	for key := range m.data {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

const libraryAPITree = `{"snapshot":{"source":{"base_url":"https://example.com","space_key":"TEAM"},"captured_at":"2026-10-22T00:00:00Z"},"nodes":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"last_modified_at":"2026-10-21T00:00:00Z","children":[]}],"pages":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"path":"Root/One"}]}`

var libraryAPIImportCols = []string{"id", "user_id", "status", "base_digest", "manifest", "report", "created_at"}

func libraryAPIExpectCatalog(mock sqlmock.Sqlmock, rows *sqlmock.Rows) {
	mock.ExpectQuery("SELECT .* FROM confluence_tasks WHERE user_id").WithArgs("owner").WillReturnRows(rows)
	mock.ExpectQuery("SELECT .* FROM confluence_library_imports WHERE user_id").WithArgs("owner").WillReturnRows(sqlmock.NewRows(libraryAPIImportCols))
}

func TestConfluenceLibraryAPI(t *testing.T) {
	for _, mode := range []string{"catalog", "archive", "archive-check", "invalid-check", "failed-check", "file", "missing-file", "invalid-archive-path", "invalid-file", "invalid-import", "invalid-import-id", "unconfigured", "stale-apply"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			objects := &libraryAPIObjects{data: map[string][]byte{"confluence/owner/" + exportTaskID + "/tree.json": []byte(libraryAPITree), "confluence/owner/" + exportTaskID + "/1/page.md": []byte("---\nconfluence_page_id: \"1\"\nconfluence_version: 1\n---\n# One")}}
			h := &Handler{DB: db, Config: &config.Config{}, ConfluenceLibrary: &worker.ConfluenceLibrary{DB: db, Objects: objects}}
			record := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(record)
			c.Set("user_id", "owner")
			spaceID := domain.ConfluenceSpaceID("https://example.com", "TEAM")
			c.Params = gin.Params{{Key: "space_id", Value: spaceID}, {Key: "page_id", Value: "1"}, {Key: "import_id", Value: exportTaskID}}
			c.Request = httptest.NewRequest("GET", "/?name=page.md", nil)
			want := 200
			switch mode {
			case "catalog", "archive", "archive-check", "file":
				libraryAPIExpectCatalog(mock, exportRows("completed", "export", 1))
				if mode == "archive-check" {
					want = 204
					h.CheckConfluenceArchive(c)
					c.Writer.WriteHeaderNow()
					if responseBody := record.Body.String(); responseBody != "" {
						t.Fatal(responseBody)
					}
				}
				if mode == "catalog" {
					h.GetConfluenceLibrary(c)
					var catalog domain.ConfluenceLibrary
					if err := json.Unmarshal(record.Body.Bytes(), &catalog); err != nil || len(catalog.Spaces) != 1 {
						t.Fatal(catalog, err)
					}
				}
				if mode == "archive" {
					h.ExportConfluenceLibrary(c)
					if record.Header().Get("Content-Type") != "application/zip" || !strings.HasPrefix(record.Body.String(), "PK") {
						t.Fatal(record.Body.String())
					}
				}
				if mode == "file" {
					h.ConfluenceLibraryFile(c)
					if !strings.Contains(record.Body.String(), "# One") || record.Header().Get("Content-Security-Policy") == "" {
						t.Fatal("file policy missing")
					}
				}
			case "missing-file":
				libraryAPIExpectCatalog(mock, sqlmock.NewRows(exportCols))
				want = 404
				h.ConfluenceLibraryFile(c)
			case "invalid-archive-path":
				c.Request = httptest.NewRequest("GET", "/?path=../secret", nil)
				want = 400
				h.ExportConfluenceLibrary(c)
			case "invalid-check":
				c.Request = httptest.NewRequest("GET", "/?path=../secret", nil)
				want = 400
				h.CheckConfluenceArchive(c)
			case "failed-check":
				mock.ExpectQuery("SELECT .* FROM confluence_tasks WHERE user_id").WithArgs("owner").WillReturnError(errors.New("object metadata unavailable"))
				want = 502
				h.CheckConfluenceArchive(c)
				if !strings.Contains(record.Body.String(), "object metadata unavailable") {
					t.Fatal(record.Body.String())
				}
			case "invalid-file":
				c.Params[1].Value = "../1"
				want = 400
				h.ConfluenceLibraryFile(c)
			case "invalid-import":
				c.Request = httptest.NewRequest("POST", "/", strings.NewReader("not ZIP"))
				want = 400
				h.PreviewConfluenceImport(c)
			case "invalid-import-id":
				c.Params[2].Value = "bad"
				want = 400
				h.ApplyConfluenceImport(c)
			case "unconfigured":
				h.ConfluenceLibrary = nil
				want = 503
				h.GetConfluenceLibrary(c)
			case "stale-apply":
				mock.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"locked"}).AddRow(1))
				mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs(exportTaskID, "owner").WillReturnRows(sqlmock.NewRows(libraryAPIImportCols).AddRow(exportTaskID, "owner", "applied", "digest", []byte("[]"), []byte(`{"items":[]}`), time.Now()))
				mock.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"released"}).AddRow(1))
				want = 409
				h.ApplyConfluenceImport(c)
			}
			if record.Code != want {
				t.Fatalf("want %d got %d: %s", want, record.Code, record.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
