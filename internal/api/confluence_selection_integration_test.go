//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/worker"
)

type selectionIntegrationSource struct {
	tree  []byte
	pages []string
}

func (s *selectionIntegrationSource) Tree(context.Context, string) ([]byte, error) {
	return s.tree, nil
}
func (s *selectionIntegrationSource) Page(_ context.Context, page domain.ConfluencePage) ([]worker.ExportFile, error) {
	s.pages = append(s.pages, page.ID)
	return []worker.ExportFile{{Name: "page.md", Content: []byte(fmt.Sprintf("---\nconfluence_page_id: %q\nconfluence_version: %d\n---\n# Page %s", page.ID, page.Version, page.ID))}}, nil
}

// runConfluenceSelectionIntegration 复用独立测试库，验证真实 SQL 状态机和 JWT 范围。
func runConfluenceSelectionIntegration(t *testing.T, db *sql.DB) {
	ctx := context.Background()
	owner, other := uuid.NewString(), uuid.NewString()
	for _, user := range []string{owner, other} {
		if _, err := db.ExecContext(ctx, `INSERT INTO users (id,email,password_hash) VALUES (?,?,?)`, user, user+"@test.example", "unused"); err != nil {
			t.Fatal(err)
		}
	}
	objects := &libraryAPIObjects{data: map[string][]byte{}}
	h := &Handler{DB: db, Config: &config.Config{Server: config.ServerConfig{BasePath: "/v1"}, Auth: config.AuthConfig{JWTSecret: "selection-test-secret", TokenExpiry: time.Hour, RateLimit: 1000}, Confluence: config.ConfluenceConfig{BaseURL: "https://example.com", Cookie: "test"}}, ConfluenceObjects: objects, ConfluenceLibrary: &worker.ConfluenceLibrary{DB: db, Objects: objects}, AsynqClient: confluenceQueue{}}
	router := NewRouter(h)
	call := func(user, method, path, body string, want int) []byte {
		t.Helper()
		token, err := h.generateJWT(user, user+"@test.example", 0)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response.Body.Bytes()
	}
	var tree domain.ConfluenceTree
	if err := json.Unmarshal([]byte(libraryAPITree), &tree); err != nil {
		t.Fatal(err)
	}
	node, page := tree.Nodes[0], tree.Pages[0]
	node.ID, node.Title = "2", "Two"
	page.ID, page.Title, page.Path = "2", "Two", "Root/Two"
	tree.Nodes = append(tree.Nodes, node)
	tree.Pages = append(tree.Pages, page)
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	source := &selectionIntegrationSource{tree: data}
	w := worker.ConfluenceWorker{DB: db, Objects: objects, Source: source}
	createTree := func() domain.ConfluenceTask {
		t.Helper()
		var task domain.ConfluenceTask
		if err := json.Unmarshal(call(owner, "POST", "/confluence-tasks", `{"url":"https://example.com/page"}`, 202), &task); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"id": task.ID, "user_id": owner})
		before := len(source.pages)
		if err := w.Handle(ctx, asynq.NewTask(worker.TypeConfluenceExport, payload)); err != nil {
			t.Fatal(err)
		}
		current, err := mysql.GetConfluenceTask(ctx, db, task.ID, owner)
		if err != nil || current.Stage != "selection" || current.Status != "waiting" || len(source.pages) != before {
			t.Fatal(current, err)
		}
		return current
	}
	task := createTree()
	base := "/confluence-tasks/" + task.ID
	call(other, "POST", base+"/export", `{"node_id":"2"}`, 404)
	call(owner, "POST", base+"/export", `{"node_id":"999"}`, 400)
	call(owner, "POST", base+"/export", `{"node_id":"2"}`, 202)
	call(owner, "POST", base+"/export", `{"node_id":"1"}`, 409)
	payload, _ := json.Marshal(map[string]string{"id": task.ID, "user_id": owner})
	if err := w.Handle(ctx, asynq.NewTask(worker.TypeConfluenceExport, payload)); err != nil {
		t.Fatal(err)
	}
	current, err := mysql.GetConfluenceTask(ctx, db, task.ID, owner)
	if err != nil || current.Stage != "markdown" || current.Completed != 1 || current.Total != 1 || current.Status != "completed" || len(source.pages) != 1 || source.pages[0] != "2" {
		t.Fatal(current, err, source.pages)
	}
	call(owner, "GET", base+"/pages/1/file?name=page.md", "", 404)
	call(owner, "GET", base+"/pages/2/file?name=page.md", "", 200)
	var library domain.ConfluenceLibrary
	if err := json.Unmarshal(call(owner, "GET", "/confluence-library", "", 200), &library); err != nil || len(library.Spaces) != 1 || len(library.Spaces[0].Pages) != 1 || library.Spaces[0].Pages[0].ID != "2" {
		t.Fatal(library, err)
	}
	if string(objects.data[worker.ConfluencePrefix(task)+"tree.json"]) != string(data) {
		t.Fatal("source tree changed")
	}
	second := createTree()
	if changed, err := mysql.TransitionConfluenceTask(ctx, db, second.ID, owner, "waiting", "selecting"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if err := mysql.ResetConfluenceTasks(ctx, db); err != nil {
		t.Fatal(err)
	}
	current, err = mysql.GetConfluenceTask(ctx, db, second.ID, owner)
	if err != nil || current.Status != "waiting" {
		t.Fatal(current, err)
	}
	call(owner, "POST", "/confluence-tasks/"+second.ID+"/export", `{"node_id":""}`, 202)
	payload, _ = json.Marshal(map[string]string{"id": second.ID, "user_id": owner})
	if err := w.Handle(ctx, asynq.NewTask(worker.TypeConfluenceExport, payload)); err != nil {
		t.Fatal(err)
	}
	current, err = mysql.GetConfluenceTask(ctx, db, second.ID, owner)
	if err != nil || current.Completed != 2 || current.Total != 2 || current.Status != "completed" {
		t.Fatal(current, err)
	}
}
