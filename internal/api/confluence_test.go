package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/config"
)

type confluenceObjects struct{}

func (confluenceObjects) Put(context.Context, string, []byte, string) error { return nil }
func (confluenceObjects) Get(_ context.Context, key string) ([]byte, error) {
	if strings.HasSuffix(key, "tree.json") {
		return []byte(`{"nodes":[],"pages":[{"id":"1","title":"One","version":1,"path":"One"}]}`), nil
	}
	if strings.HasSuffix(key, "page.md") {
		return []byte("# Exported"), nil
	}
	return []byte{137, 80, 78, 71, 13, 10, 26, 10}, nil
}

type confluenceQueue struct{ fail bool }

func (q confluenceQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	if q.fail {
		return nil, errors.New("queue down")
	}
	return &asynq.TaskInfo{}, nil
}
func (confluenceQueue) Close() error { return nil }

const exportTaskID = "11111111-1111-1111-1111-111111111111"

var exportCols = []string{"id", "user_id", "url", "status", "stage", "completed", "total", "error", "created_at", "updated_at", "storage_user_id"}

func exportRows(status, stage string, completed int) *sqlmock.Rows {
	return sqlmock.NewRows(exportCols).AddRow(exportTaskID, "owner", "https://example.com/page", status, stage, completed, 1, "", time.Now(), time.Now(), "")
}

func TestConfluenceAPI(t *testing.T) {
	for _, mode := range []string{"list", "get", "not-owner", "tree", "tree-pending", "file", "file-pending", "asset", "bad-file", "bad-id", "stop-running", "stop-queued", "stop-conflict", "resume", "resume-conflict", "create", "bad-url", "unconfigured", "queue-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			h := &Handler{DB: db, Config: &config.Config{Confluence: config.ConfluenceConfig{BaseURL: "https://example.com", Cookie: "session=test"}}, ConfluenceObjects: confluenceObjects{}, AsynqClient: confluenceQueue{fail: mode == "queue-error"}}
			record := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(record)
			c.Set("user_id", "owner")
			c.Params = gin.Params{{Key: "task_id", Value: exportTaskID}, {Key: "page_id", Value: "1"}}
			c.Request = httptest.NewRequest("GET", "/?name=page.md", nil)
			want := 200
			switch mode {
			case "unconfigured":
				h.ConfluenceObjects = nil
				want = 503
				h.ListConfluenceTasks(c)
			case "list":
				mock.ExpectQuery("SELECT .* WHERE user_id=\\?").WithArgs("owner").WillReturnRows(exportRows("completed", "export", 1))
				h.ListConfluenceTasks(c)
			case "create", "bad-url", "queue-error":
				raw := "https://example.com/page"
				if mode == "bad-url" {
					raw = "https://attacker.com/page"
					want = 400
				} else {
					mock.ExpectExec("INSERT INTO confluence_tasks").WillReturnResult(sqlmock.NewResult(1, 1))
					want = 202
				}
				if mode == "queue-error" {
					mock.ExpectExec("UPDATE confluence_tasks SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
					want = 503
				}
				body, _ := json.Marshal(map[string]string{"url": raw})
				c.Request = httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
				c.Request.Header.Set("Content-Type", "application/json")
				h.CreateConfluenceTask(c)
			case "bad-id":
				c.Params[0].Value = "invalid"
				want = 400
				h.GetConfluenceTask(c)
			default:
				status, stage, completed := "completed", "export", 1
				if mode == "tree-pending" {
					stage = "tree"
				}
				if mode == "file-pending" {
					completed = 0
				}
				if mode == "stop-running" {
					status = "running"
				}
				if mode == "stop-queued" {
					status = "queued"
				}
				if mode == "resume" {
					status = "stopped"
				}
				expectation := mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs(exportTaskID, "owner")
				if mode == "not-owner" {
					expectation.WillReturnRows(sqlmock.NewRows(exportCols))
				} else {
					expectation.WillReturnRows(exportRows(status, stage, completed))
				}
				switch mode {
				case "not-owner":
					want = 404
					h.GetConfluenceTask(c)
				case "get":
					h.GetConfluenceTask(c)
				case "tree", "tree-pending":
					if mode == "tree-pending" {
						want = 409
					}
					h.ConfluenceTree(c)
				case "file", "file-pending", "asset", "bad-file":
					if mode == "file-pending" {
						want = 404
					}
					if mode == "asset" {
						c.Request = httptest.NewRequest("GET", "/?name=assets/asset-0123456789abcdef01234567.png", nil)
					}
					if mode == "bad-file" {
						c.Request = httptest.NewRequest("GET", "/?name=../../secret", nil)
						want = 400
					}
					h.ConfluenceFile(c)
					if mode == "file" && record.Body.String() != "# Exported" {
						t.Fatal(record.Body.String())
					}
				case "stop-running", "stop-queued":
					next := "stopping"
					if mode == "stop-queued" {
						next = "stopped"
					}
					mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs(next, exportTaskID, "owner", status).WillReturnResult(sqlmock.NewResult(0, 1))
					want = 202
					h.StopConfluenceTask(c)
				case "stop-conflict":
					want = 409
					h.StopConfluenceTask(c)
				case "resume":
					mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs("queued", exportTaskID, "owner", "stopped").WillReturnResult(sqlmock.NewResult(0, 1))
					want = 202
					h.StartConfluenceTask(c)
				case "resume-conflict":
					want = 409
					h.StartConfluenceTask(c)
				}
			}
			if record.Code != want {
				t.Fatalf("got %d want %d: %s", record.Code, want, record.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
