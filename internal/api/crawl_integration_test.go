//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/dutybotgo/internal/agent"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/store/rustfs"
	"github.com/zhaocore/dutybotgo/internal/telemetry"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

// TestCrawlEndToEnd 连接独立 MySQL、Redis、RustFS 与真实浏览器，模型使用可控 HTTP 服务。
func TestCrawlEndToEnd(t *testing.T) {
	dsn, redisAddr, endpoint := os.Getenv("MYSQL_CRAWL_TEST_DSN"), os.Getenv("CRAWL_TEST_REDIS_ADDR"), os.Getenv("CRAWL_TEST_RUSTFS_ENDPOINT")
	if dsn == "" || redisAddr == "" || endpoint == "" {
		t.Skip("requires independent MYSQL_CRAWL_TEST_DSN, CRAWL_TEST_REDIS_ADDR and CRAWL_TEST_RUSTFS_ENDPOINT")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") || !strings.HasPrefix(redisAddr, "127.0.0.1:") {
		t.Fatal("integration requires explicitly isolated loopback services")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var name string
	if err = db.QueryRow(`SELECT DATABASE()`).Scan(&name); err != nil || !strings.HasPrefix(name, "dutybot_crawl_test_") {
		t.Fatal("refuse non-crawl test database", name, err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&count); err != nil || count != 0 {
		t.Fatal("test requires empty database", count, err)
	}
	if err = (&mysql.Store{DB: db}).RunMigrations("../../migrations"); err != nil {
		t.Fatal(err)
	}
	u1, u2, aID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, u := range []string{u1, u2} {
		if _, err = db.Exec(`INSERT INTO users (id,email,password_hash) VALUES (?,?,?)`, u, u+"@example.com", "unused"); err != nil {
			t.Fatal(err)
		}
	}
	if err = mysql.CreateAgent(ctx, db, domain.Agent{ID: aID, OwnerUserID: u1, Name: "Reporter", SystemPrompt: "整理报告", Model: "test", Skills: []string{}, MCPServers: []domain.MCPServerConfig{}, Subagents: []domain.SubagentDef{}}); err != nil {
		t.Fatal(err)
	}
	var expired atomic.Bool
	var active, peak atomic.Int32
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "other=second-origin" {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Other origin</title></head><body><main data-ready="yes">Other fact<a href="/grand">Grand</a></main></body></html>`)
	}))
	defer other.Close()
	public := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("Cookie leaked to anonymous origin")
			w.WriteHeader(403)
			return
		}
		if r.URL.Path == "/auth" {
			w.WriteHeader(403)
			return
		}
		if r.URL.Path == "/binary" {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "binary")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>Public</title></head><body><main>Public fact</main></body></html>`)
	}))
	defer public.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expired.Load() || r.Header.Get("Cookie") != "session=crawl-test" {
			w.WriteHeader(401)
			return
		}
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		if r.URL.Path == "/rss" {
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			fmt.Fprintf(w, `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/"><channel><title>RSS 完整订阅</title><item><title>全部正文</title><link>https://news.example/article</link><content:encoded><![CDATA[<p>%s</p><p>全文末尾标记</p>]]></content:encoded></item></channel></rss>`, strings.Repeat("长文", 14000))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		links := ""
		if r.URL.Path == "/recursive" {
			links = fmt.Sprintf(`<a href="%s/child">Child</a><a href="%s/child#again">Duplicate</a><a href="%s/public">Public</a><a href="%s/auth">Needs Cookie</a><a href="%s/binary">PDF</a>`, other.URL, other.URL, public.URL, public.URL, public.URL)
		}
		fmt.Fprintf(w, `<html><head><title>Source %s</title></head><body><main data-ready="yes"><h1>Controlled facts</h1><p>fact %s</p>%s</main></body></html>`, r.URL.Path, r.URL.Path, links)
	}))
	defer source.Close()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		for _, m := range body.Messages {
			if strings.Contains(m.Content, "crawl-test") {
				t.Error("Cookie sent to Agent")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"受控来源摘要[1]：已验证事实。"},"finish_reason":"stop"}]}`)
	}))
	defer model.Close()
	root := t.TempDir()
	scriptDir := filepath.Join(root, "skills/url-to-markdown/scripts")
	if err = os.MkdirAll(scriptDir, 0700); err != nil {
		t.Fatal(err)
	}
	skillRoot, err := filepath.Abs("../../agents/skills/url-to-markdown")
	if err != nil {
		t.Fatal(err)
	}
	// 自签名证书只在该测试上下文中被信任，生产入口仍严格校验 TLS。
	wrapper := fmt.Sprintf(`import {chromium} from %q; import {captureInContext,parseInput,CaptureError} from %q; import {request} from "node:https";
const browser=await chromium.launch();let result;try{const context=await browser.newContext({ignoreHTTPSErrors:true,serviceWorkers:'block',acceptDownloads:false});result={page:await captureInContext(parseInput(await Bun.stdin.text()),context,(url,options,handler)=>request(url,{...options,rejectUnauthorized:false},handler))};}catch(error){result={error:error instanceof CaptureError?error.failure:{code:'test_browser_error',status:0,retryable:false}};}finally{await browser.close();}await new Promise((resolve,reject)=>process.stdout.write(JSON.stringify(result),error=>error?reject(error):resolve()));`, filepath.Join(skillRoot, "node_modules/playwright/index.mjs"), filepath.Join(skillRoot, "scripts/crawl.ts"))
	if err = os.WriteFile(filepath.Join(scriptDir, "crawl.ts"), []byte(wrapper), 0600); err != nil {
		t.Fatal(err)
	}
	queue := worker.NewClient(redisAddr, "")
	defer queue.Close()
	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddr})
	defer inspector.Close()
	objects := rustfs.New(endpoint, "us-east-1", "dutybot-crawl-test-"+uuid.NewString(), "crawltestkey", "crawl-test-secret-only")
	crawl := &worker.CrawlWorker{Concurrency: config.ConcurrencyConfig{Worker: 4, Capture: 3, Task: 3, Report: 1}, DB: db, Objects: objects, Source: worker.NewBrowserCrawlSource(root, 3), Reporter: agent.NewReporter(agent.NewReportRuntime("test-key", model.URL), 1), Profiles: []config.CrawlProfile{{ID: "source", Origin: source.URL, Cookie: "session=crawl-test", UserIDs: []string{u1}, ReadySelector: "[data-ready=yes]"}, {ID: "other", Origin: other.URL, Cookie: "other=second-origin", UserIDs: []string{u1}, ReadySelector: "[data-ready=yes]"}}, Client: queue, Inspector: inspector}
	h := &Handler{DB: db, Crawl: crawl, Config: &config.Config{Server: config.ServerConfig{BasePath: "/v1"}, Auth: config.AuthConfig{JWTSecret: "crawl-test-jwt", RateLimit: 10000, TokenExpiry: time.Hour}}}
	router := NewRouter(h)
	call := func(user, method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		token, err := h.generateJWT(user, user+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s want %d got %d: %s", method, path, want, out.Code, out.Body.String())
		}
		return out
	}
	urls := []string{source.URL + "/page1", source.URL + "/page2", source.URL + "/page3", source.URL + "/page4"}
	body, _ := json.Marshal(CrawlTaskRequest{Name: "Daily", AgentID: aID, ProfileID: "source", URLs: urls, Enabled: boolPtr(true)})
	var task domain.CrawlTask
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks", string(body), 201).Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	call(u2, "GET", "/crawl-tasks/"+task.ID, "", 404)
	if strings.Contains(call(u1, "GET", "/crawl-profiles", "", 200).Body.String(), "session=") {
		t.Fatal("Cookie exposed")
	}
	if strings.Contains(call(u2, "GET", "/crawl-profiles", "", 200).Body.String(), "source") {
		t.Fatal("unauthorized profile exposed")
	}
	call(u2, "POST", "/crawl-tasks", string(body), 400)
	var run domain.CrawlRun
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks/"+task.ID+"/runs", "", 202).Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	call(u1, "POST", "/crawl-tasks/"+task.ID+"/runs", "", 409)
	call(u2, "GET", "/crawl-runs/"+run.ID+"/report", "", 404)
	call(u1, "GET", "/crawl-runs/"+run.ID+"/report", "", 409)
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{Concurrency: 4})
	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeCrawlRun, crawl.Handle)
	scheduled := make(chan error, 1)
	mux.HandleFunc(worker.TypeCrawlDispatch, func(ctx context.Context, _ *asynq.Task) error {
		err := crawl.DispatchDaily(ctx, time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC))
		select {
		case scheduled <- err:
		default:
		}
		return err
	})
	if err = srv.Start(mux); err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()
	waitRun := func(id, want string) {
		t.Helper()
		deadline := time.Now().Add(50 * time.Second)
		for time.Now().Before(deadline) {
			current, err := mysql.GetCrawlRun(ctx, db, id, u1)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status == want {
				return
			}
			if current.Status == "failed" && want != "failed" {
				t.Fatal(current.Error)
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("run timeout", id, want)
	}
	waitRun(run.ID, "succeeded")
	recursiveBody, _ := json.Marshal(CrawlTaskRequest{Name: "Recursive", AgentID: aID, ProfileIDs: []string{"source", "other"}, Hops: 2, URLs: []string{source.URL + "/recursive", source.URL + "/recursive#again"}, Enabled: boolPtr(false)})
	var recursiveTask domain.CrawlTask
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks", string(recursiveBody), 201).Body.Bytes(), &recursiveTask); err != nil {
		t.Fatal(err)
	}
	var recursiveRun domain.CrawlRun
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks/"+recursiveTask.ID+"/runs", "", 202).Body.Bytes(), &recursiveRun); err != nil {
		t.Fatal(err)
	}
	waitRun(recursiveRun.ID, "partial")
	recursiveResult, err := mysql.GetCrawlRun(ctx, db, recursiveRun.ID, u1)
	if err != nil || recursiveResult.Total != 6 || recursiveResult.Completed != 6 || recursiveResult.Task.Hops != 2 || len(recursiveResult.Task.ProfileIDs) != 2 {
		t.Fatal(recursiveResult, err)
	}
	recursiveReport := call(u1, "GET", "/crawl-runs/"+recursiveRun.ID+"/report", "", 200).Body.String()
	if !strings.Contains(recursiveReport, public.URL+"/auth") || !strings.Contains(recursiveReport, "403") {
		t.Fatal(recursiveReport)
	}
	call(u2, "GET", "/crawl-runs/"+recursiveRun.ID+"/report", "", 404)
	rssBody, _ := json.Marshal(CrawlTaskRequest{Name: "RSS", AgentID: aID, ProfileID: "source", URLs: []string{source.URL + "/rss"}, Enabled: boolPtr(false)})
	var rssTask domain.CrawlTask
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks", string(rssBody), 201).Body.Bytes(), &rssTask); err != nil {
		t.Fatal(err)
	}
	var rssRun domain.CrawlRun
	if err = json.Unmarshal(call(u1, "POST", "/crawl-tasks/"+rssTask.ID+"/runs", "", 202).Body.Bytes(), &rssRun); err != nil {
		t.Fatal(err)
	}
	waitRun(rssRun.ID, "succeeded")
	// 终态数据库发布略早于指标落盘，等待真实 Worker 完成终结快照。
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, readErr := objects.Get(ctx, worker.CrawlPrefix(domain.CrawlRun{ID: rssRun.ID, UserID: u1})+"metrics.json")
		var snapshot telemetry.Snapshot
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err := json.Unmarshal(data, &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Status == "succeeded" {
			if snapshot.UserID != u1 || snapshot.RunID != rssRun.ID || snapshot.Counters["documents_rss"] != 1 || snapshot.Timings["browser_attempt"].Count != 1 || snapshot.Timings["model_call"].Count < 2 || snapshot.Concurrency.Task != 3 {
				t.Fatal(snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("metrics not finalized", snapshot)
		}
		time.Sleep(20 * time.Millisecond)
	}

	full := call(u1, "GET", "/crawl-runs/"+rssRun.ID+"/markdown", "", 200)
	if len([]rune(full.Body.String())) < 28000 || !strings.Contains(full.Body.String(), "全文末尾标记") || !strings.Contains(full.Header().Get("Content-Disposition"), "sources-") {
		t.Fatal("RSS source incomplete", full.Body.Len())
	}
	call(u2, "GET", "/crawl-runs/"+rssRun.ID+"/markdown", "", 404)

	report := call(u1, "GET", "/crawl-runs/"+run.ID+"/report?download=1", "", 200)
	for _, url := range urls {
		if !strings.Contains(report.Body.String(), url) {
			t.Fatal("source lost", url)
		}
	}
	if peak.Load() > 3 {
		t.Fatal("capture limit exceeded", peak.Load())
	}
	// 多次并行领取必须只有一个成功，其他请求明确冲突。
	var winners atomic.Int32
	var wonID string
	var lock sync.Mutex
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			claimed, err := mysql.ClaimCrawlRun(ctx, db, task.ID, u1, "manual", "")
			if err == nil {
				winners.Add(1)
				lock.Lock()
				wonID = claimed.ID
				lock.Unlock()
			} else if err != mysql.ErrCrawlBusy {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if winners.Load() != 1 {
		t.Fatal("concurrent claims", winners.Load())
	}
	if err = crawl.RecoverDispatch(ctx); err != nil {
		t.Fatal(err)
	}
	waitRun(wonID, "succeeded")
	expired.Store(true)
	response := call(u1, "POST", "/crawl-tasks/"+task.ID+"/runs", "", 202)
	var failed domain.CrawlRun
	if err = json.Unmarshal(response.Body.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	waitRun(failed.ID, "partial")
	call(u1, "GET", "/crawl-runs/"+failed.ID+"/report", "", 200)
	call(u1, "GET", "/crawl-runs/"+run.ID+"/report", "", 200)
	expired.Store(false)
	scheduler := asynq.NewScheduler(asynq.RedisClientOpt{Addr: redisAddr}, &asynq.SchedulerOpts{})
	entry, err := scheduler.Register("@every 1s", asynq.NewTask(worker.TypeCrawlDispatch, nil), asynq.MaxRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	if err = scheduler.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-scheduled:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("real scheduler did not dispatch")
	}
	if err = scheduler.Unregister(entry); err != nil {
		t.Fatal(err)
	}
	scheduler.Shutdown()
	scheduledRuns, err := mysql.ListCrawlRuns(ctx, db, task.ID, u1)
	if err != nil {
		t.Fatal(err)
	}
	found := ""
	for _, r := range scheduledRuns {
		if r.Trigger == "scheduled" {
			found = r.ID
		}
	}
	if found == "" {
		t.Fatal("scheduled run absent")
	}
	waitRun(found, "succeeded")
	if err = crawl.DispatchDaily(ctx, time.Date(2026, 10, 30, 1, 0, 20, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if err = crawl.DispatchDaily(ctx, time.Date(2026, 10, 30, 1, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	claimed, err := mysql.ClaimCrawlRun(ctx, db, task.ID, u1, "manual", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = mysql.ResetCrawlRuns(ctx, db); err != nil {
		t.Fatal(err)
	}
	waitRun(claimed.ID, "failed")
	payload, _ := json.Marshal(struct {
		ID   string `json:"id"`
		User string `json:"user_id"`
	}{claimed.ID, u1})
	if err = crawl.Handle(ctx, asynq.NewTask(worker.TypeCrawlRun, payload)); err != nil {
		t.Fatal(err)
	}
	task.Enabled = false
	updated, _ := json.Marshal(CrawlTaskRequest{Name: task.Name, AgentID: task.AgentID, ProfileID: task.ProfileID, URLs: task.URLs, Enabled: boolPtr(false)})
	call(u1, "PUT", "/crawl-tasks/"+task.ID, string(updated), 200)
	if err = crawl.DispatchDaily(ctx, time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	call(u1, "GET", "/crawl-tasks/"+task.ID+"/runs", "", 200)
	if err = mysql.DeleteAgent(ctx, db, aID, u1); err != nil {
		t.Fatal(err)
	}
	crawl.Profiles = nil
	call(u1, "PUT", "/crawl-tasks/"+task.ID, string(updated), 200)
	call(u2, "PUT", "/crawl-tasks/"+task.ID, string(updated), 404)
	call(u1, "GET", "/crawl-runs/"+run.ID+"/report", "", 200)
}

func boolPtr(value bool) *bool { return &value }
