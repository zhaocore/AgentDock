//go:build integration

package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// TestAdministrationBrowser 以独立数据库和真实 HTTP/Cookie 链路运行完整浏览器套件。
func TestAdministrationBrowser(t *testing.T) {
	dsn, redisAddr := os.Getenv("MYSQL_ADMIN_TEST_DSN"), os.Getenv("ADMIN_TEST_REDIS_ADDR")
	if dsn == "" || redisAddr == "" || os.Getenv("ADMIN_BROWSER_TEST") != "1" {
		t.Skip("requires independent MYSQL_ADMIN_TEST_DSN, ADMIN_TEST_REDIS_ADDR and ADMIN_BROWSER_TEST=1")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err = db.QueryRow(`SELECT DATABASE()`).Scan(&name); err != nil || !strings.HasPrefix(name, "agentdock_admin_test_") {
		t.Fatal("refuse non-test database", name, err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty test database", err)
	}
	if err = (&mysql.Store{DB: db}).RunMigrations("../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = mysql.SyncPresets(ctx, db, []domain.Agent{{ID: uuid.NewString(), Name: "Browser preset", SystemPrompt: "test", Model: "test", Skills: []string{}, MCPServers: []domain.MCPServerConfig{}}}); err != nil {
		t.Fatal(err)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr, Password: os.Getenv("ADMIN_TEST_REDIS_PASSWORD")})
	defer redisClient.Close()
	h := &Handler{DB: db, Config: &config.Config{Server: config.ServerConfig{BasePath: "/v1", Mode: "test"}, Auth: config.AuthConfig{JWTSecret: "isolated-browser-authentication-key", RateLimit: 10000, TokenExpiry: time.Hour}}, Activity: account.NewActivity(), Instances: &notRunningManager{}, RedisHealth: func(ctx context.Context) error { return redisClient.Ping(ctx).Err() }}
	server := httptest.NewServer(NewRouter(h))
	defer server.Close()
	dashboard, err := filepath.Abs("../../dashboard")
	if err != nil {
		t.Fatal(err)
	}
	// 使用已安装的项目二进制，避免包管理器自动下载不同版本。
	next := exec.Command("node", "node_modules/next/dist/bin/next", "start", "--hostname", "127.0.0.1", "--port", "3108")
	next.Dir = dashboard
	next.Env = append(os.Environ(), "API_INTERNAL_URL="+server.URL+"/v1", "DASHBOARD_URL=http://localhost:3108")
	logFile, err := os.CreateTemp(t.TempDir(), "next-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	next.Stdout = logFile
	next.Stderr = logFile
	if err = next.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := next.Process.Kill(); err != nil {
			t.Log("Next process already ended", err)
		}
		if err := next.Wait(); err != nil {
			t.Log("Next cleanup", err)
		}
	}()
	ready := exec.Command("node", "--input-type=module", "-e", `for(let i=0;i<100;i++){try{const r=await fetch("http://127.0.0.1:3108/login");if(r.ok)process.exit(0)}catch{}await new Promise(r=>setTimeout(r,300))}process.exit(1)`)
	if err = ready.Run(); err != nil {
		data, _ := os.ReadFile(logFile.Name())
		t.Fatal("Next startup failed", err, string(data))
	}
	browser := exec.Command("node", "node_modules/@playwright/test/cli.js", "test", "--workers=1", "--timeout=60000")
	browser.Dir = dashboard
	browser.Env = append(os.Environ(), "AGENTDOCK_LIVE_E2E=1", "PLAYWRIGHT_BASE_URL=http://localhost:3108")
	output, err := browser.CombinedOutput()
	fmt.Print(string(output))
	if err != nil {
		t.Fatal("browser acceptance failed", err)
	}
}
