package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
)

func crawlTaskRows() *sqlmock.Rows {
	return sqlmock.NewRows(strings.Split(crawlTaskColumns, ",")).AddRow("task", "owner", "Daily", "agent", "profile", `["https://docs.example/a"]`, true, "[]", 0, "", time.Now(), time.Now())
}
func crawlRunRows(status string) *sqlmock.Rows {
	snapshot, _ := json.Marshal(domain.CrawlTask{ID: "task", UserID: "owner", URLs: []string{"https://docs.example/a"}})
	return sqlmock.NewRows(strings.Split(crawlRunColumns, ",")).AddRow("run", "owner", "task", snapshot, "manual", status, "capture", 0, 1, "", time.Now(), time.Now())
}

func TestCrawlStoreLifecycle(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	task := domain.CrawlTask{ID: "task", UserID: "owner", Name: "Daily", AgentID: "agent", ProfileID: "profile", URLs: []string{"https://docs.example/a"}, Enabled: true}
	mock.ExpectExec("INSERT INTO crawl_tasks").WillReturnResult(sqlmock.NewResult(1, 1))
	if err = CreateCrawlTask(ctx, db, task); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .*crawl_tasks WHERE id").WithArgs("task", "owner").WillReturnRows(crawlTaskRows())
	if got, err := GetCrawlTask(ctx, db, "task", "owner"); err != nil || got.Name != "Daily" {
		t.Fatal(got, err)
	}
	mock.ExpectQuery("SELECT .*crawl_tasks WHERE id").WithArgs("task", "other").WillReturnError(sql.ErrNoRows)
	if _, err := GetCrawlTask(ctx, db, "task", "other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE crawl_tasks SET").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = UpdateCrawlTask(ctx, db, task); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE crawl_tasks SET").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(crawlTaskRows())
	if err = UpdateCrawlTask(ctx, db, task); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .*crawl_tasks WHERE user_id").WithArgs("owner").WillReturnRows(crawlTaskRows())
	if list, err := ListCrawlTasks(ctx, db, "owner"); err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	mock.ExpectQuery("SELECT .*crawl_tasks WHERE enabled").WillReturnRows(crawlTaskRows())
	if list, err := EnabledCrawlTasks(ctx, db); err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	mock.ExpectQuery("SELECT .*crawl_runs WHERE id").WithArgs("run", "owner").WillReturnRows(crawlRunRows("queued"))
	if run, err := GetCrawlRun(ctx, db, "run", "owner"); err != nil || run.Task.UserID != "owner" {
		t.Fatal(run, err)
	}
	mock.ExpectQuery("SELECT .*crawl_runs WHERE task_id").WithArgs("task", "owner").WillReturnRows(crawlRunRows("queued"))
	if runs, err := ListCrawlRuns(ctx, db, "task", "owner"); err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
	mock.ExpectQuery("SELECT .*crawl_runs WHERE status").WillReturnRows(crawlRunRows("queued"))
	if runs, err := UnfinishedCrawlRuns(ctx, db); err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
	mock.ExpectExec("UPDATE crawl_runs SET status='running'").WillReturnResult(sqlmock.NewResult(0, 1))
	if started, err := StartCrawlRun(ctx, db, "run", "owner"); err != nil || !started {
		t.Fatal(started, err)
	}
	mock.ExpectExec("UPDATE crawl_runs SET stage").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = CrawlProgress(ctx, db, "run", "owner", "report", 1); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE crawl_runs SET stage").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT .*crawl_runs").WillReturnRows(crawlRunRows("failed"))
	if err = CrawlProgress(ctx, db, "run", "owner", "report", 1); err == nil {
		t.Fatal("terminal revived")
	}
	mock.ExpectExec("INSERT INTO crawl_pages").WillReturnResult(sqlmock.NewResult(1, 1))
	if err = RecordCrawlPage(ctx, db, "run", "owner", domain.CrawlPageProgress{Position: 0, Status: "captured"}); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("succeeded", "", "succeeded", "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = FinishCrawlRun(ctx, db, "run", "owner", "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE crawl_runs SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = ResetCrawlRuns(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlClaims(t *testing.T) {
	for _, mode := range []string{"manual", "scheduled", "overlap", "skipped", "duplicate", "missing", "insert-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			mock.ExpectBegin()
			query := mock.ExpectQuery("SELECT .*crawl_tasks.*FOR UPDATE").WithArgs("task", "owner")
			if mode == "missing" {
				query.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
			} else {
				query.WillReturnRows(crawlTaskRows())
				scheduled := mode == "scheduled" || mode == "skipped" || mode == "duplicate"
				if scheduled {
					count := 0
					if mode == "duplicate" {
						count = 1
					}
					mock.ExpectQuery("SELECT COUNT.*scheduled_slot").WithArgs("task", "2026-10-30").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				}
				if mode == "duplicate" {
					mock.ExpectRollback()
				} else {
					count := 0
					if mode == "overlap" || mode == "skipped" {
						count = 1
					}
					mock.ExpectQuery("SELECT COUNT.*status IN").WithArgs("task").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
					if mode == "overlap" {
						mock.ExpectRollback()
					} else {
						insert := mock.ExpectExec("INSERT INTO crawl_runs")
						if mode == "insert-error" {
							insert.WillReturnError(errors.New("write failed"))
							mock.ExpectRollback()
						} else {
							insert.WillReturnResult(sqlmock.NewResult(1, 1))
							mock.ExpectCommit()
						}
					}
				}
			}
			trigger, slot := "manual", ""
			if mode == "scheduled" || mode == "skipped" || mode == "duplicate" {
				trigger, slot = "scheduled", "2026-10-30"
			}
			run, err := ClaimCrawlRun(context.Background(), db, "task", "owner", trigger, slot)
			switch mode {
			case "manual", "scheduled":
				if err != nil || run.Status != "queued" {
					t.Fatal(run, err)
				}
			case "skipped":
				if err != nil || run.Status != "skipped" {
					t.Fatal(run, err)
				}
			default:
				if err == nil {
					t.Fatal("expected rejection")
				}
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCrawlStoreReadFailures(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	bad := errors.New("database unavailable")
	mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnError(bad)
	if _, err := ListCrawlTasks(ctx, db, "owner"); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnError(bad)
	if _, err := EnabledCrawlTasks(ctx, db); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectQuery("SELECT .*crawl_runs").WillReturnError(bad)
	if _, err := ListCrawlRuns(ctx, db, "task", "owner"); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectQuery("SELECT .*crawl_runs").WillReturnError(bad)
	if _, err := UnfinishedCrawlRuns(ctx, db); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectQuery("SELECT .*crawl_tasks").WillReturnRows(sqlmock.NewRows(strings.Split(crawlTaskColumns, ",")).AddRow("task", "owner", "Daily", "a", "p", "{", true, "[]", 0, "", time.Now(), time.Now()))
	if _, err := GetCrawlTask(ctx, db, "task", "owner"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	mock.ExpectQuery("SELECT .*crawl_runs").WillReturnRows(sqlmock.NewRows(strings.Split(crawlRunColumns, ",")).AddRow("r", "o", "t", "{", "manual", "queued", "capture", 0, 1, "", time.Now(), time.Now()))
	if _, err := GetCrawlRun(ctx, db, "r", "o"); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCrawlPublicationTerminalState 防止重启后的旧 Worker 把失败轮次重新发布。
func TestCrawlPublicationTerminalState(t *testing.T) {
	for _, status := range []string{"failed", "succeeded"} {
		t.Run(status, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("succeeded", "", "succeeded", "run", "owner").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT .*crawl_runs").WithArgs("run", "owner").WillReturnRows(crawlRunRows(status))
			err = FinishCrawlRun(context.Background(), db, "run", "owner", "succeeded", "")
			if (status == "failed") != (err != nil) {
				t.Fatal("unexpected publication result", status, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestCrawlCapturedPositions 校验原文读取按用户过滤，保留位置顺序和数据库错误。
func TestCrawlCapturedPositions(t *testing.T) {
	for _, mode := range []string{"captured", "empty", "query-error", "scan-error", "rows-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			query := mock.ExpectQuery("SELECT p.position FROM crawl_pages p JOIN crawl_runs r ON r.id=p.run_id WHERE r.id=\\? AND r.user_id=\\? AND p.status='captured' ORDER BY p.position LIMIT 101").WithArgs("run", "owner")
			rows := sqlmock.NewRows([]string{"position"})
			switch mode {
			case "captured":
				rows.AddRow(0).AddRow(2)
			case "query-error":
				query.WillReturnError(errors.New("database unavailable"))
			case "scan-error":
				rows.AddRow("invalid")
			case "rows-error":
				rows.AddRow(0).RowError(0, errors.New("read interrupted"))
			}
			if mode != "query-error" {
				query.WillReturnRows(rows)
			}
			positions, err := CrawlCapturedPositions(context.Background(), db, "run", "owner")
			if mode == "captured" {
				if err != nil || len(positions) != 2 || positions[1] != 2 {
					t.Fatal(positions, err)
				}
			} else if mode == "empty" {
				if err != nil || len(positions) != 0 {
					t.Fatal(positions, err)
				}
			} else if err == nil {
				t.Fatal("database failure hidden")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
