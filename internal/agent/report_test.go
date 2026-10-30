package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

func TestReportPipeline(t *testing.T) {
	var active, maximum, calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		calls.Add(1)
		time.Sleep(time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"摘要[1]"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	reporter := NewReporter(NewReportRuntime("test-key", server.URL))
	a := domain.Agent{ID: "a", Name: "Reporter", Model: "test", SystemPrompt: "整理文档"}
	pages := []domain.CrawlPage{{URL: "https://docs.example/a", Title: "A", CapturedAt: time.Now(), Markdown: strings.Repeat("文", 12001)}, {URL: "https://docs.example/b", Title: "B", CapturedAt: time.Now(), Markdown: "事实"}}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			report, err := reporter.Generate(context.Background(), a, pages)
			if err != nil || !strings.Contains(report, "https://docs.example/b") {
				t.Error(report, err)
			}
		})
	}
	wg.Wait()
	if maximum.Load() != 1 || calls.Load() != 16 {
		t.Fatal("concurrency or input coverage", maximum.Load(), calls.Load())
	}
	if _, err := reporter.Generate(context.Background(), a, nil); err == nil {
		t.Fatal("empty report accepted")
	}
	if _, err := reporter.Generate(context.Background(), a, []domain.CrawlPage{{}}); err == nil {
		t.Fatal("empty source accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reporter.slots <- struct{}{}
	if _, err := reporter.Generate(ctx, a, pages); err == nil {
		t.Fatal("canceled slot acquired")
	}
	<-reporter.slots
}

func TestReportProviderFailures(t *testing.T) {
	for _, mode := range []string{"auth", "retry", "empty", "length", "oversized", "bad-json"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if mode == "auth" {
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"private-document"}}`)
					return
				}
				if mode == "retry" && calls < 3 {
					w.WriteHeader(503)
					return
				}
				content, finish := "summary", "stop"
				if mode == "empty" {
					content = ""
				}
				if mode == "length" {
					finish = "length"
				}
				if mode == "oversized" {
					content = strings.Repeat("x", 6001)
				}
				if mode == "bad-json" {
					fmt.Fprint(w, "{")
					return
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":%q}]}`, content, finish)
			}))
			defer server.Close()
			r := NewReporter(NewReportRuntime("test", server.URL))
			a := domain.Agent{Model: "test"}
			_, err := r.call(context.Background(), a, "source")
			if mode == "retry" {
				if err != nil || calls != 3 {
					t.Fatal(calls, err)
				}
			} else if err == nil || strings.Contains(err.Error(), "private-document") {
				t.Fatal("invalid or leaking error", err)
			}
			if mode == "auth" && calls != 1 {
				t.Fatal("auth retried")
			}
			if _, err = r.call(context.Background(), a, strings.Repeat("x", 24001)); err == nil {
				t.Fatal("input silently truncated")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reportDelay(ctx, time.Hour); err == nil {
		t.Fatal("retry delay ignored cancellation")
	}
}
