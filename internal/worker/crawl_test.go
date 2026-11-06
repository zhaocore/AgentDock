package worker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/telemetry"
)

type crawlTestSource struct{ failure bool }

func (s crawlTestSource) Capture(_ context.Context, _ config.CrawlProfile, raw string) (domain.CrawlPage, error) {
	if s.failure {
		return domain.CrawlPage{}, errors.New("capture failed status=401")
	}
	return domain.CrawlPage{URL: raw, Title: "Page", CapturedAt: time.Now(), Markdown: "fact"}, nil
}

type crawlTestReporter struct{ failure bool }

func (r crawlTestReporter) Generate(context.Context, domain.Agent, []domain.CrawlPage) (string, error) {
	if r.failure {
		return "", errors.New("report failed status=503")
	}
	return "# Report", nil
}

type crawlTestObjects struct {
	keys          []string
	failure       string
	metrics       []telemetry.Snapshot
	failMetricsAt int
}

func (o *crawlTestObjects) Put(_ context.Context, key string, data []byte, _ string) error {
	o.keys = append(o.keys, key)
	if strings.HasSuffix(key, "metrics.json") {
		var snapshot telemetry.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			return err
		}
		o.metrics = append(o.metrics, snapshot)
		if o.failMetricsAt > 0 && len(o.metrics) == o.failMetricsAt {
			return errors.New("metrics store unavailable")
		}
	}
	if strings.HasSuffix(key, o.failure) && o.failure != "" {
		return errors.New("storage failed")
	}
	return nil
}
func (o *crawlTestObjects) Get(context.Context, string) ([]byte, error) {
	return []byte("# Report"), nil
}

type crawlTestQueue struct {
	err error
	ids []string
}

func (q *crawlTestQueue) EnqueueContext(_ context.Context, t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	var p crawlPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return nil, err
	}
	q.ids = append(q.ids, p.ID)
	return &asynq.TaskInfo{}, q.err
}
func (q *crawlTestQueue) Close() error { return nil }

func crawlWorkerRunRows(status string) *sqlmock.Rows {
	return crawlWorkerRunRowsForID("run", status)
}

func crawlWorkerRunRowsForID(id, status string) *sqlmock.Rows {
	task, _ := json.Marshal(domain.CrawlTask{ID: "task", AgentID: "agent", ProfileID: "profile", URLs: []string{"https://docs.example/a"}})
	return sqlmock.NewRows([]string{"id", "user_id", "task_id", "snapshot", "trigger_type", "status", "stage", "completed", "total", "error", "created_at", "updated_at", "storage_user_id"}).AddRow(id, "owner", "task", task, "manual", status, "capture", 0, 1, "", time.Now(), time.Now(), "")
}

func TestCrawlWorkerPublishing(t *testing.T) {
	for _, mode := range []string{"success", "duplicate", "capture", "report", "storage", "unauthorized", "missing", "metrics", "finalmetrics"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			objects := &crawlTestObjects{}
			if mode == "storage" {
				objects.failure = "report.md"
			}
			if mode == "metrics" {
				objects.failure = "metrics.json"
			}
			if mode == "finalmetrics" {
				objects.failMetricsAt = 4
			}
			w := &CrawlWorker{Concurrency: config.ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}, DB: db, Objects: objects, Source: crawlTestSource{failure: mode == "capture"}, Reporter: crawlTestReporter{failure: mode == "report"}, Profiles: []config.CrawlProfile{{ID: "profile", Origin: "https://docs.example", UserIDs: []string{"owner"}}}}
			query := mock.ExpectQuery("SELECT .*crawl_runs").WithArgs("run", "owner")
			if mode == "missing" {
				query.WillReturnError(errors.New("missing run"))
			} else {
				query.WillReturnRows(crawlWorkerRunRows("queued"))
				changed := int64(1)
				if mode == "duplicate" {
					changed = 0
				}
				mock.ExpectExec("UPDATE crawl_runs SET status='running'").WithArgs("run", "owner").WillReturnResult(sqlmock.NewResult(0, changed))
				if mode != "duplicate" {
					if mode == "unauthorized" {
						w.Profiles = nil
					} else if mode != "metrics" {
						mock.ExpectQuery("SELECT .* FROM agents").WithArgs("agent", "owner").WillReturnRows(sqlmock.NewRows([]string{"id", "owner", "name", "prompt", "model", "skills", "mcp", "subagents", "created", "updated"}).AddRow("agent", nil, "Reporter", "prompt", "model", "[]", "[]", "[]", time.Now(), time.Now()))
						mock.ExpectExec("INSERT INTO crawl_pages").WillReturnResult(sqlmock.NewResult(1, 1))
						if mode != "capture" {
							mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("capture", 1, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
							mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("report", 1, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
							if mode != "report" {
								mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("publish", 1, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
							}
						}
					}
					status := "failed"
					if mode == "success" || mode == "finalmetrics" {
						status = "succeeded"
					}
					mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs(status, sqlmock.AnyArg(), status, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			payload, _ := json.Marshal(crawlPayload{"run", "owner"})
			err := w.Handle(context.Background(), asynq.NewTask(TypeCrawlRun, payload))
			if (mode == "success" || mode == "duplicate") && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && mode != "duplicate" && err == nil {
				t.Fatal("failed run accepted")
			}
			if mode == "capture" || mode == "report" {
				for _, key := range objects.keys {
					if strings.HasSuffix(key, "report.md") {
						t.Fatal("published incomplete report")
					}
				}
			}
			if mode != "missing" && mode != "duplicate" {
				if len(objects.metrics) < 2 {
					t.Fatal("missing initial/final metrics attempts", objects.keys)
				}
				last := objects.metrics[len(objects.metrics)-1]
				want := "failed"
				if mode == "success" || mode == "finalmetrics" {
					want = "succeeded"
				}
				if last.Status != want || last.RunID != "run" || last.UserID != "owner" {
					t.Fatal(last)
				}
				if mode == "success" && (last.Counters["pages_captured"] != 1 || last.Timings["report_phase"].Count != 1 || last.Timings["object_put"].Count != 3) {
					t.Fatal(last)
				}
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := (&CrawlWorker{}).Handle(context.Background(), asynq.NewTask(TypeCrawlRun, []byte("{"))); err == nil {
		t.Fatal("malformed message")
	}
}

// crawlTestInspector 控制队列状态，不把归档消息模拟为待执行。
type crawlTestInspector struct {
	state asynq.TaskState
	err   error
}

func (i crawlTestInspector) GetTaskInfo(string, string) (*asynq.TaskInfo, error) {
	return &asynq.TaskInfo{State: i.state}, i.err
}

func TestCrawlDispatchRecovery(t *testing.T) {
	for _, mode := range []string{"missing", "missing-queue", "queue-error", "pending", "archived-queued", "archived-running", "completed", "missing-running", "inspect-error", "finish-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			q := &crawlTestQueue{}
			inspector := crawlTestInspector{err: asynq.ErrTaskNotFound}
			status := "queued"
			wantError := false
			switch mode {
			case "missing-queue":
				inspector.err = asynq.ErrQueueNotFound
			case "queue-error":
				q.err = errors.New("redis down")
				wantError = true
			case "pending":
				inspector = crawlTestInspector{state: asynq.TaskStatePending}
			case "archived-queued", "archived-running", "finish-error":
				inspector = crawlTestInspector{state: asynq.TaskStateArchived}
			case "completed":
				inspector = crawlTestInspector{state: asynq.TaskStateCompleted}
			case "inspect-error":
				inspector.err = errors.New("inspect failed")
				wantError = true
			}
			if mode == "archived-running" || mode == "missing-running" {
				status = "running"
			}
			mock.ExpectQuery("SELECT .*crawl_runs WHERE status").WillReturnRows(crawlWorkerRunRows(status))
			if mode == "archived-queued" || mode == "archived-running" || mode == "completed" || mode == "missing-running" || mode == "finish-error" {
				update := mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("failed", sqlmock.AnyArg(), "failed", "run", "owner")
				if mode == "finish-error" {
					update.WillReturnError(errors.New("database unavailable"))
					wantError = true
				} else {
					update.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			w := &CrawlWorker{DB: db, Client: q, Inspector: inspector}
			err := w.RecoverDispatch(context.Background())
			if (err != nil) != wantError {
				t.Fatal(mode, err)
			}
			wantEnqueues := 0
			if mode == "missing" || mode == "missing-queue" || mode == "queue-error" {
				wantEnqueues = 1
			}
			if len(q.ids) != wantEnqueues {
				t.Fatal(mode, q.ids)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, state := range []asynq.TaskState{asynq.TaskStatePending, asynq.TaskStateArchived, asynq.TaskStateCompleted} {
		w := &CrawlWorker{Client: &crawlTestQueue{err: asynq.ErrTaskIDConflict}, Inspector: crawlTestInspector{state: state}}
		err := w.enqueue(context.Background(), domain.CrawlRun{ID: "run", UserID: "owner"})
		if (err == nil) != (state == asynq.TaskStatePending) {
			t.Fatal(state, err)
		}
	}
	w := &CrawlWorker{Client: &crawlTestQueue{err: asynq.ErrTaskIDConflict}, Inspector: crawlTestInspector{err: errors.New("inspect failed")}}
	if err := w.enqueue(context.Background(), domain.CrawlRun{ID: "run", UserID: "owner"}); err == nil {
		t.Fatal("inspection error ignored")
	}
	if err := (&CrawlWorker{Concurrency: config.ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}}).DispatchDaily(context.Background(), time.Date(2026, 10, 30, 2, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func TestCrawlBrowserProtocol(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "skills/url-to-markdown/scripts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	script := `const input=JSON.parse(await Bun.stdin.text()); const mode=new URL(input.url).pathname; let result; if(['/good','/rss','/html-large','/format'].includes(mode)){result={page:{url:input.url,title:'Page',captured_at:new Date().toISOString(),markdown:mode==='/good'?'fact':'文'.repeat(25000),format:mode==='/rss'?'rss':mode==='/format'?'invalid':'html'}};}else if(mode==='/bad'){process.stdout.write('{');process.exit(0);}else if(mode==='/missing'){result={page:{}};}else {result={error:{code:'http_error',status:mode==='/retry'?503:401,retryable:mode==='/retry'}};} await new Promise((resolve,reject)=>process.stdout.write(JSON.stringify(result),error=>error?reject(error):resolve()));`
	if err := os.WriteFile(filepath.Join(dir, "crawl.ts"), []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	source := NewBrowserCrawlSource(root, 3)
	profile := config.CrawlProfile{Origin: "https://docs.example", Cookie: "session=test", ReadySelector: "main"}
	for _, mode := range []string{"good", "rss", "html-large", "format", "auth", "bad", "missing", "retry"} {
		ctx, recorder := telemetry.New(context.Background(), "browser-test", "owner", "task", time.Now(), config.ConcurrencyConfig{Capture: 3})
		page, err := source.Capture(ctx, profile, profile.Origin+"/"+mode)
		if mode == "retry" && (recorder.Snapshot("failed").Counters["browser_retries"] != 2 || recorder.Snapshot("failed").Timings["browser_attempt"].Count != 3 || recorder.Snapshot("failed").Timings["browser_backoff"].Count != 2) {
			t.Fatal(recorder.Snapshot("failed"))
		}
		if mode == "good" || mode == "rss" {
			if err != nil || (mode == "good" && page.Markdown != "fact") || (mode == "rss" && len([]rune(page.Markdown)) != 25000) {
				t.Fatal(page, err)
			}
		} else if err == nil {
			t.Fatal("invalid browser response accepted", mode)
		}
	}
	source.AgentDir = filepath.Join(root, "missing")
	if _, err := source.Capture(context.Background(), profile, profile.Origin+"/good"); err == nil {
		t.Fatal("missing script accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 3 {
		source.slots <- struct{}{}
	}
	if _, err := source.Capture(ctx, profile, "url"); err == nil {
		t.Fatal("ignored cancellation")
	}
	for range 3 {
		<-source.slots
	}
}
