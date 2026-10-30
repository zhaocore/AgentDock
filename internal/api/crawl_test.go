package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

const crawlAPIID = "11111111-1111-1111-1111-111111111111"

func crawlAPITaskRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "name", "agent_id", "profile_id", "urls", "enabled", "created_at", "updated_at"}).AddRow(crawlAPIID, "owner", "Daily", crawlAPIID, "docs", `["https://docs.example/page"]`, true, time.Now(), time.Now())
}
func crawlAPIRunRows(status string) *sqlmock.Rows {
	snapshot, _ := json.Marshal(domain.CrawlTask{ID: crawlAPIID, AgentID: crawlAPIID, ProfileID: "docs", URLs: []string{"https://docs.example/page"}})
	return sqlmock.NewRows([]string{"id", "user_id", "task_id", "snapshot", "trigger_type", "status", "stage", "completed", "total", "error", "created_at", "updated_at"}).AddRow(crawlAPIID, "owner", crawlAPIID, snapshot, "manual", status, "report", 1, 1, "", time.Now(), time.Now())
}
func crawlAPIAgentRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "owner", "name", "prompt", "model", "skills", "mcp", "subagents", "created", "updated"}).AddRow(crawlAPIID, nil, "Reporter", "prompt", "model", "[]", "[]", "[]", time.Now(), time.Now())
}

func TestCrawlAPI(t *testing.T) {
	for _, mode := range []string{"profiles", "unconfigured", "list", "get", "missing", "bad-id", "bad-body", "blank", "denied", "create", "edit", "run", "busy", "runs", "report", "not-published", "object-failure", "list-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			h := &Handler{DB: db, Crawl: &worker.CrawlWorker{DB: db, Objects: &apiCrawlObjects{failure: mode == "object-failure"}, Client: confluenceQueue{}, Profiles: []config.CrawlProfile{{ID: "docs", Origin: "https://docs.example", Cookie: "private-cookie-value", UserIDs: []string{"owner"}, ReadySelector: "main"}}}}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Set("user_id", "owner")
			c.Params = gin.Params{{Key: "task_id", Value: crawlAPIID}, {Key: "run_id", Value: crawlAPIID}}
			body := `{"name":"Daily","agent_id":"` + crawlAPIID + `","profile_id":"docs","urls":["https://docs.example/page"],"enabled":true}`
			if mode == "bad-body" {
				body = "{"
			}
			if mode == "blank" {
				body = strings.Replace(body, "Daily", " ", 1)
			}
			if mode == "denied" {
				body = strings.Replace(body, "docs\"", "other\"", 1)
			}
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			expected := 200
			switch mode {
			case "profiles":
				h.CrawlProfiles(c)
			case "unconfigured":
				h.Crawl = nil
				h.ListCrawlTasks(c)
				expected = 503
			case "list", "list-failure":
				q := mock.ExpectQuery("SELECT .*crawl_tasks WHERE user_id").WithArgs("owner")
				if mode == "list-failure" {
					q.WillReturnError(errors.New("db down"))
					expected = 502
				} else {
					q.WillReturnRows(crawlAPITaskRows())
				}
				h.ListCrawlTasks(c)
			case "get", "missing":
				q := mock.ExpectQuery("SELECT .*crawl_tasks WHERE id").WithArgs(crawlAPIID, "owner")
				if mode == "missing" {
					q.WillReturnError(sql.ErrNoRows)
					expected = 404
				} else {
					q.WillReturnRows(crawlAPITaskRows())
				}
				h.GetCrawlTask(c)
			case "bad-id":
				c.Params[0].Value = "bad"
				h.GetCrawlTask(c)
				expected = 400
			case "bad-body", "blank", "denied":
				h.CreateCrawlTask(c)
				expected = 400
			case "create", "edit":
				if mode == "edit" {
					mock.ExpectQuery("SELECT .*crawl_tasks").WithArgs(crawlAPIID, "owner").WillReturnRows(crawlAPITaskRows())
				}
				mock.ExpectQuery("SELECT .*agents").WithArgs(crawlAPIID, "owner").WillReturnRows(crawlAPIAgentRows())
				if mode == "create" {
					mock.ExpectExec("INSERT INTO crawl_tasks").WillReturnResult(sqlmock.NewResult(1, 1))
				} else {
					mock.ExpectExec("UPDATE crawl_tasks SET").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(crawlAPITaskRows())
				if mode == "create" {
					h.CreateCrawlTask(c)
					expected = 201
				} else {
					h.UpdateCrawlTask(c)
				}
			case "run", "busy":
				mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(crawlAPITaskRows())
				mock.ExpectBegin()
				mock.ExpectQuery("SELECT .*crawl_tasks.*FOR UPDATE").WillReturnRows(crawlAPITaskRows())
				count := 0
				if mode == "busy" {
					count = 1
				}
				mock.ExpectQuery("SELECT COUNT.*crawl_runs").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if mode == "busy" {
					mock.ExpectRollback()
					expected = 409
				} else {
					mock.ExpectExec("INSERT INTO crawl_runs").WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectCommit()
					mock.ExpectQuery("SELECT .*crawl_runs").WillReturnRows(crawlAPIRunRows("queued"))
					expected = 202
				}
				h.RunCrawlTask(c)
			case "runs":
				mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(crawlAPITaskRows())
				mock.ExpectQuery("SELECT .*crawl_runs").WithArgs(crawlAPIID, "owner").WillReturnRows(crawlAPIRunRows("running"))
				h.ListCrawlRuns(c)
			case "report", "not-published", "object-failure":
				status := "succeeded"
				if mode == "not-published" {
					status = "failed"
					expected = 409
				}
				if mode == "object-failure" {
					expected = 502
				}
				mock.ExpectQuery("SELECT .*crawl_runs").WithArgs(crawlAPIID, "owner").WillReturnRows(crawlAPIRunRows(status))
				c.Request = httptest.NewRequest("GET", "/?download=1", nil)
				h.CrawlReport(c)
			}
			if response.Code != expected {
				t.Fatalf("want %d got %d: %s", expected, response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private-cookie-value") {
				t.Fatal("credential leak")
			}
			if mode == "report" && !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
				t.Fatal("missing download header")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// apiCrawlObjects 模拟对象存储边界，不让终态失败暴露报告。
type apiCrawlObjects struct{ failure bool }

func (o *apiCrawlObjects) Put(context.Context, string, []byte, string) error { return nil }
func (o *apiCrawlObjects) Get(context.Context, string) ([]byte, error) {
	if o.failure {
		return nil, errors.New("object unavailable")
	}
	return []byte("# Report"), nil
}

// TestCrawlDisableUnavailableConfig 验证失效配置仍能停用，其他修改不能绕过权限。
func TestCrawlDisableUnavailableConfig(t *testing.T) {
	for _, mode := range []string{"deleted-agent", "revoked-profile", "edit-agent", "enable", "other-user"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			h := &Handler{DB: db, Crawl: &worker.CrawlWorker{Profiles: []config.CrawlProfile{{ID: "docs", Origin: "https://docs.example", UserIDs: []string{"owner"}}}}}
			query := mock.ExpectQuery("SELECT .*crawl_tasks").WithArgs(crawlAPIID, "owner")
			if mode == "other-user" {
				query.WillReturnError(sql.ErrNoRows)
			} else {
				query.WillReturnRows(crawlAPITaskRows())
			}
			out := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(out)
			c.Set("user_id", "owner")
			c.Params = gin.Params{{Key: "task_id", Value: crawlAPIID}}
			body := `{"name":"Daily","agent_id":"` + crawlAPIID + `","profile_id":"docs","urls":["https://docs.example/page"],"enabled":false}`
			want := 200
			switch mode {
			case "deleted-agent", "revoked-profile":
				if mode == "revoked-profile" {
					h.Crawl.Profiles = nil
				}
				mock.ExpectExec("UPDATE crawl_tasks SET").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(crawlAPITaskRows())
			case "edit-agent", "enable":
				if mode == "edit-agent" {
					body = strings.Replace(body, "Daily", "Renamed", 1)
				} else {
					body = strings.Replace(body, "false", "true", 1)
				}
				mock.ExpectQuery("SELECT .*agents").WillReturnError(sql.ErrNoRows)
				want = 404
			case "other-user":
				want = 404
			}
			c.Request = httptest.NewRequest("PUT", "/", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			h.UpdateCrawlTask(c)
			if out.Code != want {
				t.Fatal(out.Code, out.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
