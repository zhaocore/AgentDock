package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/telemetry"
)

type slowCrawlSource struct{ active, maximum atomic.Int32 }

func (s *slowCrawlSource) Capture(ctx context.Context, _ config.CrawlProfile, raw string) (domain.CrawlPage, error) {
	n := s.active.Add(1)
	defer s.active.Add(-1)
	for {
		old := s.maximum.Load()
		if n <= old || s.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	select {
	case <-ctx.Done():
		return domain.CrawlPage{}, ctx.Err()
	case <-time.After(20 * time.Millisecond):
	}
	return domain.CrawlPage{URL: raw, Title: "page", Markdown: "fact", CapturedAt: time.Now()}, nil
}

func TestCrawlTaskConcurrencyMetrics(t *testing.T) {
	for _, limit := range []int{1, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			for range 6 {
				mock.ExpectExec("INSERT INTO crawl_pages").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE crawl_runs SET stage").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			source := &slowCrawlSource{}
			limits := config.ConcurrencyConfig{Task: limit}
			w := &CrawlWorker{DB: db, Source: source, Objects: &crawlReportObjects{}, Concurrency: limits}
			r := domain.CrawlRun{ID: "run", UserID: "owner", Total: 6}
			for i := range 6 {
				r.Task.URLs = append(r.Task.URLs, fmt.Sprintf("https://example.com/%d", i))
			}
			ctx, recorder := telemetry.New(context.Background(), "run", "owner", "task", time.Now(), limits)
			started := time.Now()
			pages, _, _, err := w.capture(ctx, r, nil)
			snapshot := recorder.Snapshot("succeeded")
			if err != nil || len(pages) != 6 || int(source.maximum.Load()) != limit || snapshot.Timings["page_capture"].Count != 6 || snapshot.Counters["markdown_chars"] != 24 {
				t.Fatal(pages, snapshot, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			t.Logf("controlled pages=6 task_limit=%d wall_ms=%.1f summed_page_ms=%.1f max_capture=%d", limit, float64(time.Since(started))/float64(time.Millisecond), snapshot.Timings["page_capture"].TotalMS, source.maximum.Load())
		})
	}
	if _, _, _, err := (&CrawlWorker{}).capture(context.Background(), domain.CrawlRun{}, nil); err == nil {
		t.Fatal("invalid concurrency accepted")
	}
}

func TestCaptureSlotWaitCancellationMetrics(t *testing.T) {
	source := NewBrowserCrawlSource(t.TempDir(), 2)
	source.slots <- struct{}{}
	source.slots <- struct{}{}
	ctx, recorder := telemetry.New(context.Background(), "run", "owner", "task", time.Now(), config.ConcurrencyConfig{Capture: 2})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err := source.Capture(ctx, config.CrawlProfile{}, "unused")
	snapshot := recorder.Snapshot("canceled")
	if !errors.Is(err, context.DeadlineExceeded) || snapshot.Timings["capture_wait"].Failures != 1 || snapshot.Timings["capture_wait"].TotalMS < 15 || snapshot.Timings["browser_attempt"].Count != 0 {
		t.Fatal(snapshot, err)
	}
}

// cancelCrawlObjects 在初始快照后触发正常取消，终结指标必须脱离已取消上下文。
type cancelCrawlObjects struct {
	crawlTestObjects
	cancel  context.CancelFunc
	cancels bool
}

func (o *cancelCrawlObjects) Put(ctx context.Context, key string, data []byte, contentType string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := o.crawlTestObjects.Put(ctx, key, data, contentType); err != nil {
		return err
	}
	if len(o.metrics) == 1 && o.cancels {
		o.cancel()
		o.cancels = false
	}
	return nil
}

func TestCrawlCanceledRunKeepsMetrics(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT .*crawl_runs").WillReturnRows(crawlWorkerRunRows("queued"))
	mock.ExpectExec("UPDATE crawl_runs SET status='running'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("failed", sqlmock.AnyArg(), "failed", "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	objects := &cancelCrawlObjects{cancel: cancel, cancels: true}
	w := &CrawlWorker{DB: db, Objects: objects, Concurrency: config.ConcurrencyConfig{Task: 1}, Profiles: []config.CrawlProfile{{ID: "profile", Origin: "https://docs.example", UserIDs: []string{"owner"}}}}
	payload, _ := json.Marshal(crawlPayload{"run", "owner"})
	if err := w.Handle(ctx, asynq.NewTask(TypeCrawlRun, payload)); err == nil {
		t.Fatal("cancel accepted")
	}
	if len(objects.metrics) != 2 || objects.metrics[1].Status != "canceled" {
		t.Fatal(objects.metrics)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
