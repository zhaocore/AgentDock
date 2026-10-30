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

	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/telemetry"
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
	reporter := NewReporter(NewReportRuntime("test-key", server.URL), 1)
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
			r := NewReporter(NewReportRuntime("test", server.URL), 1)
			a := domain.Agent{Model: "test"}
			ctx, recorder := telemetry.New(context.Background(), "retry-test", "user", "task", time.Now(), config.ConcurrencyConfig{Report: 1})
			_, err := r.call(ctx, a, "source")
			if mode == "retry" {
				if recorder.Snapshot("succeeded").Counters["model_retries"] != 2 || recorder.Snapshot("succeeded").Counters["model_http_status_503"] != 2 || recorder.Snapshot("succeeded").Timings["model_backoff"].Count != 2 {
					t.Fatal(recorder.Snapshot("failed"))
				}
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

// TestReportCapacityMetrics 使用受控 HTTP 延迟比较整份报告限额，不调用真实模型。
func TestReportCapacityMetrics(t *testing.T) {
	for _, limit := range []int{1, 2} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			var active, maximum atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := active.Add(1)
				defer active.Add(-1)
				for {
					old := maximum.Load()
					if n <= old || maximum.CompareAndSwap(old, n) {
						break
					}
				}
				time.Sleep(25 * time.Millisecond)
				fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"摘要"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			reporter := NewReporter(NewReportRuntime("test", server.URL), limit)
			var group sync.WaitGroup
			records := make([]*telemetry.Recorder, 4)
			started := time.Now()
			for i := range 4 {
				group.Go(func() {
					ctx, recorder := telemetry.New(context.Background(), fmt.Sprint(i), "test", "task", time.Now(), config.ConcurrencyConfig{Report: limit})
					records[i] = recorder
					_, err := reporter.Generate(ctx, domain.Agent{Model: "test"}, []domain.CrawlPage{{URL: "https://example.com", Title: "Page", Markdown: "fact", CapturedAt: time.Now()}})
					if err != nil {
						t.Error(err)
					}
				})
			}
			group.Wait()
			var wait, model float64
			for _, record := range records {
				snapshot := record.Snapshot("succeeded")
				if snapshot.Timings["model_call"].Count != 2 || snapshot.Counters["model_http_status_200"] != 2 {
					t.Fatal(snapshot)
				}
				wait += snapshot.Timings["report_wait"].TotalMS
				model += snapshot.Timings["model_call"].TotalMS
			}
			if int(maximum.Load()) != limit || wait < 40 {
				t.Fatal("limit or waiting not measured", maximum.Load(), wait)
			}
			t.Logf("controlled reports=4 limit=%d wall_ms=%.1f summed_report_wait_ms=%.1f summed_model_call_ms=%.1f max_http=%d", limit, float64(time.Since(started))/float64(time.Millisecond), wait, model, maximum.Load())
		})
	}
}

func TestReportSlotCancellationMetrics(t *testing.T) {
	reporter := NewReporter(nil, 1)
	reporter.slots <- struct{}{}
	ctx, recorder := telemetry.New(context.Background(), "run", "user", "task", time.Now(), config.ConcurrencyConfig{Report: 1})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err := reporter.Generate(ctx, domain.Agent{}, nil); err == nil {
		t.Fatal("canceled wait succeeded")
	}
	snapshot := recorder.Snapshot("canceled")
	if snapshot.Timings["report_wait"].Failures != 1 || snapshot.Timings["report_wait"].TotalMS < 15 || snapshot.Timings["model_call"].Count != 0 {
		t.Fatal(snapshot)
	}
}
