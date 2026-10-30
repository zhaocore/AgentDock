//go:build integration

package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

// TestCrawlControlQueue 在真实 Redis 中验证四个长任务不能阻塞九点派发。
func TestCrawlControlQueue(t *testing.T) {
	addr := os.Getenv("CRAWL_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CRAWL_TEST_REDIS_ADDR must select an isolated Redis")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatal("requires explicitly isolated loopback Redis")
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("SELECT .*crawl_tasks WHERE enabled=TRUE").WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "agent_id", "profile_id", "urls", "enabled", "profile_ids", "hops", "include_pattern", "created_at", "updated_at"}))
	control, err := StartCrawlDispatcher(addr, "", &CrawlWorker{DB: db}, func() time.Time { return time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC) })
	if err != nil {
		t.Fatal(err)
	}
	defer control.Shutdown()
	started, release := make(chan struct{}, 4), make(chan struct{})
	load := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 4})
	mux := asynq.NewServeMux()
	jobType := "crawl-test:block-" + uuid.NewString()
	mux.HandleFunc(jobType, func(ctx context.Context, _ *asynq.Task) error {
		started <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err = load.Start(mux); err != nil {
		t.Fatal(err)
	}
	defer func() { close(release); load.Shutdown() }()
	client := NewClient(addr, "")
	defer client.Close()
	for range 4 {
		if _, err = client.EnqueueContext(context.Background(), asynq.NewTask(jobType, nil), asynq.MaxRetry(0)); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("execution pool did not saturate")
		}
	}
	id := uuid.NewString()
	if _, err = client.EnqueueContext(context.Background(), asynq.NewTask(TypeCrawlDispatch, nil), asynq.Queue(CrawlControlQueue), asynq.TaskID(id), asynq.Retention(time.Minute), asynq.MaxRetry(0)); err != nil {
		t.Fatal(err)
	}
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
	defer inspector.Close()
	completed := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		info, err := inspector.GetTaskInfo(CrawlControlQueue, id)
		if err != nil {
			t.Fatal(err)
		}
		if info.State == asynq.TaskStateCompleted {
			completed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !completed {
		t.Fatal("saturated execution pool blocked control dispatch")
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCrawlArchivedRecovery 验证数据库恢复后释放归档消息对应的占用，不重跑流水线。
func TestCrawlArchivedRecovery(t *testing.T) {
	addr := os.Getenv("CRAWL_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("CRAWL_TEST_REDIS_ADDR must select an isolated Redis")
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatal("requires explicitly isolated loopback Redis")
	}
	for _, mode := range []string{"read", "claim", "finalize"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			id := uuid.NewString()
			read := mock.ExpectQuery("SELECT .*crawl_runs").WithArgs(id, "owner")
			if mode == "read" {
				read.WillReturnError(errors.New("transient database read failure"))
			} else {
				rows := crawlWorkerRunRowsForID(id, "queued")
				read.WillReturnRows(rows)
				start := mock.ExpectExec("UPDATE crawl_runs SET status='running'").WithArgs(id, "owner")
				if mode == "claim" {
					start.WillReturnError(errors.New("transient claim failure"))
				} else {
					start.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("failed", sqlmock.AnyArg(), "failed", id, "owner").WillReturnError(errors.New("transient finalize failure"))
				}
			}
			client := NewClient(addr, "")
			defer client.Close()
			inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: addr})
			defer inspector.Close()
			w := &CrawlWorker{DB: db, Objects: &crawlTestObjects{}, Client: client, Inspector: inspector}
			server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr}, asynq.Config{Concurrency: 1})
			mux := asynq.NewServeMux()
			mux.HandleFunc(TypeCrawlRun, w.Handle)
			if err = server.Start(mux); err != nil {
				t.Fatal(err)
			}
			defer server.Shutdown()
			payload, _ := json.Marshal(crawlPayload{ID: id, UserID: "owner"})
			if _, err = client.EnqueueContext(context.Background(), asynq.NewTask(TypeCrawlRun, payload), asynq.TaskID(id), asynq.MaxRetry(0)); err != nil {
				t.Fatal(err)
			}
			archived := false
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
				info, err := inspector.GetTaskInfo("default", id)
				if err == nil && info.State == asynq.TaskStateArchived {
					archived = true
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
			if !archived {
				t.Fatal("failed message was not archived")
			}
			status := "queued"
			if mode == "finalize" {
				status = "running"
			}
			mock.ExpectQuery("SELECT .*crawl_runs WHERE status").WillReturnRows(crawlWorkerRunRowsForID(id, status))
			mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("failed", sqlmock.AnyArg(), "failed", id, "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			if err = w.RecoverDispatch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err = inspector.DeleteTask("default", id); err != nil {
				t.Fatal(err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
