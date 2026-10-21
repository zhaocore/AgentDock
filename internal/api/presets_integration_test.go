//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhaocore/dutybotgo/internal/agent"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

// TestSharedPresetsMySQL 验证真实迁移、JWT 用户隔离、配置同步与历史保留。
func TestSharedPresetsMySQL(t *testing.T) {
	dsn := os.Getenv("MYSQL_AGENT_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_AGENT_TEST_DSN must point to an independent empty dutybot_agents_test_* database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var databaseName string
	if err := db.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&databaseName); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(databaseName, "dutybot_agents_test_") {
		t.Fatalf("refuse non-test database %q", databaseName)
	}
	var tables int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Fatal("test requires an empty database")
	}
	store := &mysql.Store{DB: db}
	oldMigrations := t.TempDir()
	oldSchema, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldMigrations, "0001_init.sql"), oldSchema, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMigrations(oldMigrations); err != nil {
		t.Fatal(err)
	}
	u1, u2, privateID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, user := range []string{u1, u2} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, password_hash) VALUES (?, ?, ?)`, user, user+"@test.example", "unused"); err != nil {
			t.Fatal(err)
		}
	}
	if err := mysql.CreateAgent(ctx, db, domain.Agent{ID: privateID, OwnerUserID: u1, Name: "legacy", SystemPrompt: "private", Model: "model", Skills: []string{}, MCPServers: []domain.MCPServerConfig{}, Subagents: []domain.SubagentDef{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMigrations("../../migrations"); err != nil {
		t.Fatal(err)
	}
	if _, err := mysql.GetAgent(ctx, db, privateID, u1); err != nil {
		t.Fatalf("migration lost legacy agent: %v", err)
	}

	root := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("presets/agent1.md", "first prompt")
	write("skills/review/SKILL.md", "shared skill body")
	write("mcp.json", `[{"name":"tools","transport":"sse","endpoint":"https://private.example/secret","env":{"KEY":"secret-value"}}]`)
	syncAgents := func(ctx context.Context) error {
		presets, err := agent.LoadPresets(root, "shared-model")
		if err != nil {
			return err
		}
		return mysql.SyncPresets(ctx, db, presets)
	}
	if err := syncAgents(ctx); err != nil {
		t.Fatal(err)
	}
	presets, err := agent.LoadPresets(root, "shared-model")
	if err != nil {
		t.Fatal(err)
	}
	sharedID := presets[0].ID
	prompts := make(chan string, 2)
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if len(body.Messages) == 0 {
			t.Error("missing system message")
			w.WriteHeader(400)
			return
		}
		prompts <- body.Messages[0].Content
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)); err != nil {
			t.Error(err)
		}
	}))
	defer llm.Close()
	manager := agent.NewManager(db, agent.NewLangChainRuntime("test-key", llm.URL))
	defer manager.ShutdownAll()
	h := &Handler{DB: db, Instances: manager, SyncAgents: syncAgents, Config: &config.Config{Server: config.ServerConfig{BasePath: "/v1"}, Auth: config.AuthConfig{JWTSecret: "integration-secret", TokenExpiry: time.Hour, RateLimit: 1000}}}
	router := NewRouter(h)
	call := func(user, method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		token, err := h.generateJWT(user, user+"@test.example")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s: want %d got %d: %s", method, path, status, response.Code, response.Body.String())
		}
		return response
	}
	for _, user := range []string{u1, u2} {
		response := call(user, "GET", "/agents", "", 200)
		if !strings.Contains(response.Body.String(), sharedID) || strings.Contains(response.Body.String(), "secret-value") || strings.Contains(response.Body.String(), "private.example") {
			t.Fatalf("invalid public list: %s", response.Body.String())
		}
		call(user, "PUT", "/agents/"+sharedID, `{}`, 403)
		call(user, "DELETE", "/agents/"+sharedID, "", 403)
	}
	call(u2, "GET", "/agents/"+privateID, "", 404)
	var inst domain.AgentInstance
	response := call(u1, "POST", "/agents/"+sharedID+"/instances", `{}`, 201)
	if err := json.Unmarshal(response.Body.Bytes(), &inst); err != nil {
		t.Fatal(err)
	}
	if inst.OwnerUserID != u1 {
		t.Fatalf("instance not user-owned: %+v", inst)
	}
	call(u2, "POST", "/instances/"+inst.ID+"/start", "", 404)
	call(u1, "POST", "/instances/"+inst.ID+"/start", "", 202)
	var session domain.Session
	response = call(u1, "POST", "/instances/"+inst.ID+"/sessions", "", 201)
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"first prompt", "updated prompt"} {
		write("presets/agent1.md", prompt)
		call(u1, "GET", "/agents", "", 200)
		call(u1, "POST", "/sessions/"+session.ID+"/messages", `{"content":"hello"}`, 200)
		if sent := <-prompts; !strings.Contains(sent, prompt) || !strings.Contains(sent, "shared skill body") {
			t.Fatalf("stale runtime prompt: %s", sent)
		}
	}
	call(u2, "GET", "/sessions/"+session.ID+"/messages", "", 404)
	call(u2, "POST", "/sessions/"+session.ID+"/messages", `{"content":"hello"}`, 404)
	write("mcp.json", "{")
	call(u1, "GET", "/agents", "", 500)
	if saved, err := mysql.GetAgent(ctx, db, sharedID, u1); err != nil || !strings.Contains(saved.SystemPrompt, "updated prompt") {
		t.Fatalf("failed sync changed previous data: %+v %v", saved, err)
	}
	write("mcp.json", "[]")
	if err := os.Remove(filepath.Join(root, "presets/agent1.md")); err != nil {
		t.Fatal(err)
	}
	call(u1, "GET", "/agents", "", 200)
	call(u1, "GET", "/agents/"+sharedID, "", 404)
	call(u1, "POST", "/sessions/"+session.ID+"/messages", `{"content":"hello"}`, 404)
	response = call(u1, "GET", "/sessions/"+session.ID+"/messages", "", 200)
	var messages struct {
		Data []domain.Message `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages.Data) != 4 {
		t.Fatalf("retired preset lost history: %+v", messages.Data)
	}
	call(u2, "POST", "/instances/"+inst.ID+"/stop", "", 404)
	call(u1, "POST", "/instances/"+inst.ID+"/stop", "", 202)
}
