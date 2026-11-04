package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
)

type crawlGraphSource struct {
	mu      sync.Mutex
	links   map[string][]string
	errors  map[string]error
	skipped map[string]bool
	visits  map[string]config.CrawlProfile
}

func (s *crawlGraphSource) Capture(_ context.Context, p config.CrawlProfile, address string) (domain.CrawlPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.visits[address]; exists {
		return domain.CrawlPage{}, fmt.Errorf("duplicate capture")
	}
	s.visits[address] = p
	return domain.CrawlPage{URL: address, Title: "Page", CapturedAt: time.Now(), Markdown: "fact", Links: s.links[address], Skipped: s.skipped[address]}, s.errors[address]
}

func TestCrawlRecursiveCapture(t *testing.T) {
	const root = "https://a.example/"
	const child = "https://b.example/child?b=2&a=1"
	const grand = "https://b.example/grand"
	for _, mode := range []string{"zero", "one", "two", "filter", "duplicate-seeds", "ordered-params", "cookie", "non-html", "fatal", "limit", "record-failure", "object-failure", "progress-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			mock.MatchExpectationsInOrder(false)
			source := &crawlGraphSource{links: map[string][]string{root: {root + "#self", child, child + "#again", "mailto:x@example.com"}, child: {grand, root}, grand: {root}}, errors: map[string]error{}, skipped: map[string]bool{}, visits: map[string]config.CrawlProfile{}}
			task := domain.CrawlTask{UserID: "owner", URLs: []string{root}, Hops: 1}
			want := []string{root, child}
			growth := 1
			wantIssues := 0
			wantPages := 2
			wantError := false
			processed := 2
			switch mode {
			case "zero":
				task.Hops = 0
				want = []string{root}
				growth = 0
				wantPages = 1
				processed = 1
			case "two":
				task.Hops = 2
				want = append(want, grand)
				growth = 2
				wantPages = 3
				processed = 3
			case "filter":
				task.IncludePattern = "/grand$"
				want = []string{root}
				growth = 0
				wantPages = 1
				processed = 1
			case "duplicate-seeds":
				task.URLs = append(task.URLs, "https://A.example:443/#part")
				growth = 0
			case "ordered-params":
				other := "https://b.example/child?a=1&b=2"
				source.links[root] = append(source.links[root], other)
				want = append(want, other)
				wantPages = 3
				processed = 3
			case "cookie":
				source.errors[child] = &CrawlCaptureError{Code: "http_error", Status: 403, FinalURL: child}
				wantIssues = 1
				wantPages = 1
			case "non-html":
				source.skipped[child] = true
				wantPages = 1
			case "fatal":
				source.errors[child] = errors.New("HTTP 404")
				wantError = true
				wantPages = 1
				processed = 1
			case "limit":
				source.links[root] = nil
				for i := 0; i < 100; i++ {
					source.links[root] = append(source.links[root], fmt.Sprintf("https://b.example/%d", i))
				}
				want = []string{root}
				wantError = true
				growth = 0
				wantPages = 1
				processed = 0
			case "record-failure", "object-failure", "progress-failure":
				want = []string{root}
				wantError = true
				growth = 0
				wantPages = 0
				processed = 0
			}
			records := len(want)
			for index := 0; index < records; index++ {
				q := mock.ExpectExec("INSERT INTO crawl_pages")
				if mode == "record-failure" {
					q.WillReturnError(errors.New("record unavailable"))
				} else {
					q.WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}
			updates := processed
			if mode == "progress-failure" {
				updates = 1
				growth = 1
				wantPages = 1
				processed = 1
			}
			for i := 0; i < updates; i++ {
				q := mock.ExpectExec("UPDATE crawl_runs SET stage")
				if mode == "progress-failure" {
					q.WillReturnError(errors.New("progress unavailable"))
				} else {
					q.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			for i := 0; i < growth; i++ {
				mock.ExpectExec("UPDATE crawl_runs SET total").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			objects := &crawlTestObjects{}
			if mode == "object-failure" {
				objects.failure = "pages/0.json"
			}
			w := &CrawlWorker{Concurrency: config.ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}, DB: db, Source: source, Objects: objects}
			profiles := []config.CrawlProfile{{ID: "a", Origin: "https://a.example", Cookie: "a=secret"}, {ID: "b", Origin: "https://b.example", Cookie: "b=secret"}}
			pages, issues, done, err := w.capture(context.Background(), domain.CrawlRun{ID: "run", UserID: "owner", Task: task, Total: len(task.URLs)}, profiles)
			if (err != nil) != wantError || len(pages) != wantPages || len(issues) != wantIssues || done != processed {
				t.Fatal(len(pages), issues, done, err)
			}
			for _, address := range want {
				p, found := source.visits[address]
				if !found {
					t.Fatal("missing", address)
				}
				if strings.HasPrefix(address, "https://b.example") && p.Cookie != "b=secret" {
					t.Fatal("wrong Cookie origin", p)
				}
			}
			if len(source.visits) != len(want) {
				t.Fatal("unexpected visits", source.visits)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCrawlPartialPublication(t *testing.T) {
	for _, allBlocked := range []bool{false, true} {
		t.Run(fmt.Sprint(allBlocked), func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			const a = "https://a.example/"
			const b = "https://b.example/"
			source := &crawlGraphSource{visits: map[string]config.CrawlProfile{}, errors: map[string]error{b: &CrawlCaptureError{Code: "http_error", Status: 401, FinalURL: b}}}
			if allBlocked {
				source.errors[a] = &CrawlCaptureError{Code: "login_redirect", Status: 302, FinalURL: a + "login"}
			}
			r := domain.CrawlRun{ID: "run", UserID: "owner", Total: 2, Task: domain.CrawlTask{UserID: "owner", AgentID: "agent", URLs: []string{a, b}}}
			mock.ExpectQuery("SELECT .*agents").WillReturnRows(sqlmock.NewRows([]string{"id", "owner", "name", "prompt", "model", "skills", "mcp", "subagents", "created", "updated"}).AddRow("agent", nil, "Report", "prompt", "model", "[]", "[]", "[]", time.Now(), time.Now()))
			for i := 0; i < 2; i++ {
				mock.ExpectExec("INSERT INTO crawl_pages").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("capture", i+1, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("report", 2, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE crawl_runs SET stage").WithArgs("publish", 2, "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE crawl_runs SET status=\\?").WithArgs("partial", sqlmock.AnyArg(), "partial", "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			objects := &crawlReportObjects{}
			w := &CrawlWorker{Concurrency: config.ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}, DB: db, Objects: objects, Source: source, Reporter: crawlTestReporter{failure: allBlocked}}
			if err := w.execute(context.Background(), r); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(objects.data["crawl/owner/run/report.md"]), "https://b.example/") || !strings.Contains(string(objects.data["crawl/owner/run/report.md"]), "部分完成") {
				t.Fatal(objects.data)
			}
			var issues []crawlCookieIssue
			if err := json.Unmarshal(objects.data["crawl/owner/run/cookie-issues.json"], &issues); err != nil {
				t.Fatal(err)
			}
			want := 1
			if allBlocked {
				want = 2
			}
			if len(issues) != want {
				t.Fatal(issues)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type crawlReportObjects struct{ data map[string][]byte }

func (o *crawlReportObjects) Put(_ context.Context, key string, data []byte, _ string) error {
	if o.data == nil {
		o.data = map[string][]byte{}
	}
	o.data[key] = append([]byte{}, data...)
	return nil
}
func (o *crawlReportObjects) Get(_ context.Context, key string) ([]byte, error) {
	return o.data[key], nil
}

func TestCrawlCaptureClassification(t *testing.T) {
	for _, test := range []struct {
		code   string
		status int
		want   bool
	}{{"http_error", 401, true}, {"http_error", 403, true}, {"login_marker_missing", 200, true}, {"network_error", 0, false}, {"http_error", 404, false}} {
		e := &CrawlCaptureError{Code: test.code, Status: test.status}
		if e.NeedsCookieReview() != test.want {
			t.Fatal(e)
		}
	}
}

// crawlBatchSource 在另一页开始后返回错误，验证取消不会掩盖真正失败原因。
type crawlBatchSource struct {
	started  chan struct{}
	canceled chan struct{}
	failure  error
}

func (s *crawlBatchSource) Capture(ctx context.Context, _ config.CrawlProfile, address string) (domain.CrawlPage, error) {
	if strings.HasSuffix(address, "/failure") {
		<-s.started
		if s.failure == nil {
			return domain.CrawlPage{URL: "https://wrong.example/", Title: "Page"}, nil
		}
		return domain.CrawlPage{}, s.failure
	}
	close(s.started)
	select {
	case <-ctx.Done():
		close(s.canceled)
		return domain.CrawlPage{}, ctx.Err()
	case <-time.After(time.Second):
		return domain.CrawlPage{URL: address, Title: "Page", Markdown: "fact", CapturedAt: time.Now()}, nil
	}
}

func TestCrawlBatchCancellation(t *testing.T) {
	for _, mode := range []string{"fatal", "url-mismatch", "login"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			failure := errors.New("source request failed")
			source := &crawlBatchSource{started: make(chan struct{}), canceled: make(chan struct{}), failure: failure}
			if mode == "url-mismatch" {
				source.failure = nil
			}
			if mode == "login" {
				source.failure = &CrawlCaptureError{Code: "http_error", Status: 403}
				for index, status := range []string{"captured", "needs_cookie"} {
					mock.ExpectExec("INSERT INTO crawl_pages").WithArgs(index, status, sqlmock.AnyArg(), sqlmock.AnyArg(), "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE crawl_runs SET stage").WillReturnResult(sqlmock.NewResult(0, 1))
				}
			} else {
				mock.ExpectExec("INSERT INTO crawl_pages").WithArgs(1, "failed", sqlmock.AnyArg(), sqlmock.AnyArg(), "run", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			w := &CrawlWorker{DB: db, Objects: &crawlReportObjects{}, Source: source, Concurrency: config.ConcurrencyConfig{Task: 2}}
			run := domain.CrawlRun{ID: "run", UserID: "owner", Total: 2, Task: domain.CrawlTask{URLs: []string{"https://example.com/slow", "https://example.com/failure"}}}
			pages, issues, completed, err := w.capture(context.Background(), run, nil)
			if mode == "login" {
				if err != nil || len(pages) != 1 || len(issues) != 1 || completed != 2 {
					t.Fatal(pages, issues, completed, err)
				}
				select {
				case <-source.canceled:
					t.Fatal("login review canceled remaining page")
				default:
				}
			} else {
				select {
				case <-source.canceled:
				default:
					t.Fatal("fatal page did not cancel remaining capture")
				}
				if err == nil || mode == "fatal" && !errors.Is(err, failure) || mode == "url-mismatch" && !strings.Contains(err.Error(), "crawl source URL mismatch") {
					t.Fatal("original failure was lost", err)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
