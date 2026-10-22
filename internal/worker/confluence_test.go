package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/domain"
)

type memoryObjects struct {
	data map[string][]byte
	fail bool
}

func (m *memoryObjects) Put(ctx context.Context, key string, data []byte, kind string) error {
	if m.fail {
		return errors.New("storage down")
	}
	m.data[key] = bytes.Clone(data)
	return nil
}
func (m *memoryObjects) Get(ctx context.Context, key string) ([]byte, error) {
	data, ok := m.data[key]
	if !ok {
		return nil, errors.New("missing object")
	}
	return data, nil
}

type exportSource struct {
	tree  []byte
	fail  bool
	pages int
}

func (s *exportSource) Tree(context.Context, string) ([]byte, error) {
	if s.fail {
		return nil, errors.New("tree failed")
	}
	return s.tree, nil
}
func (s *exportSource) Page(context.Context, domain.ConfluencePage) ([]ExportFile, error) {
	s.pages++
	if s.fail {
		return nil, errors.New("page failed")
	}
	return []ExportFile{{Name: "page.md", Content: []byte("# Hello")}}, nil
}

const treeFixture = `{"nodes":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"children":[]}],"pages":[{"id":"1","title":"One","url":"https://example.com/pages/viewpage.action?pageId=1","version":1,"path":"One"}]}`

var taskColumns = []string{"id", "user_id", "url", "status", "stage", "completed", "total", "error", "created_at", "updated_at"}

func taskRows(status, stage string, done int) *sqlmock.Rows {
	return sqlmock.NewRows(taskColumns).AddRow("task", "user", "https://example.com/page", status, stage, done, 1, "", time.Now(), time.Now())
}

func TestConfluenceRunCheckpointAndFailure(t *testing.T) {
	for _, mode := range []string{"fresh", "resume", "source-error", "storage-error", "invalid-checkpoint", "bad-tree"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			objects := &memoryObjects{data: map[string][]byte{"confluence/user/task/tree.json": []byte(treeFixture)}, fail: mode == "storage-error"}
			source := &exportSource{tree: []byte(treeFixture), fail: mode == "source-error"}
			task := domain.ConfluenceTask{ID: "task", UserID: "user", Stage: "tree"}
			if mode == "resume" {
				task.Stage = "export"
				task.Completed = 1
			}
			if mode == "invalid-checkpoint" {
				task.Stage = "export"
				task.Completed = 2
			}
			if mode == "bad-tree" {
				source.tree = []byte(`{}`)
			}
			if mode == "fresh" {
				mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("export", 0, 1, "task", "user").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("export", 1, 1, "task", "user").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			worker := ConfluenceWorker{DB: db, Objects: objects, Source: source}
			err = worker.run(context.Background(), task)
			success := mode == "fresh" || mode == "resume"
			if (err == nil) != success {
				t.Fatalf("mode %s: %v", mode, err)
			}
			if mode == "fresh" && string(objects.data["confluence/user/task/1/page.md"]) != "# Hello" {
				t.Fatal("missing published page")
			}
			if mode == "resume" && source.pages != 0 {
				t.Fatal("resume downloaded completed page")
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfluenceHandleTransitions(t *testing.T) {
	for _, mode := range []string{"completed", "stopped", "duplicate", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			status := "queued"
			if mode == "duplicate" {
				status = "running"
			}
			mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WithArgs("task", "user").WillReturnRows(taskRows(status, "export", 1))
			if mode != "duplicate" {
				mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs("running", "task", "user", "queued").WillReturnResult(sqlmock.NewResult(0, 1))
				next := "running"
				if mode == "stopped" {
					next = "stopping"
				}
				mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows(next, "export", 1))
				if mode == "failure" || mode == "cancel" {
					mock.ExpectExec("UPDATE confluence_tasks SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
				} else {
					to, from := "completed", "running"
					if mode == "stopped" {
						to, from = "stopped", "stopping"
					}
					mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs(to, "task", "user", from).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			objects := &memoryObjects{data: map[string][]byte{"confluence/user/task/tree.json": []byte(treeFixture)}}
			if mode == "failure" || mode == "cancel" {
				objects.data = map[string][]byte{}
			}
			w := ConfluenceWorker{DB: db, Objects: objects, Source: &exportSource{}}
			payload, _ := json.Marshal(confluencePayload{"task", "user"})
			err := w.Handle(context.Background(), asynq.NewTask(TypeConfluenceExport, payload))
			if (err != nil) != (mode == "failure" || mode == "cancel") {
				t.Fatal(err)
			}
			if err = mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConfluenceSourcePageRealConverter(t *testing.T) {
	image := append([]byte{137, 80, 78, 71}, bytes.Repeat([]byte("attachment"), 16*1024)...)
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Cookie") != "session=test" {
			t.Error("missing Cookie")
		}
		base := "https://" + r.Host
		if strings.Contains(r.URL.Path, "/rest/api/") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": "1", "title": "One", "version": map[string]int{"number": 1}, "_links": map[string]string{"base": base}, "body": map[string]any{"view": map[string]string{"value": `<h1>Hello</h1><p>世界</p><img src="` + base + `/download/attachments/1/test.png"><img src="https://jira.example.com/images/icons/issuetypes/epic.svg">`}}})
		} else {
			w.Write(image)
		}
	}))
	defer server.Close()
	root, err := filepath.Abs("../../agents")
	if err != nil {
		t.Fatal(err)
	}
	source := NewConfluenceSource(config.ConfluenceConfig{BaseURL: server.URL, Cookie: "session=test"}, root)
	source.HTTP = server.Client()
	page := domain.ConfluencePage{ID: "1", Title: "One", Version: 1, URL: server.URL + "/pages/viewpage.action?pageId=1"}
	files, err := source.Page(context.Background(), page)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || !strings.Contains(string(files[0].Content), "confluence_version: 1") || !strings.Contains(string(files[0].Content), "./assets/asset-") {
		t.Fatalf("bad bundle: %v", files)
	}
	if len(files) == 2 && !bytes.Equal(files[1].Content, image) {
		t.Fatal("attachment truncated during converter pipe transfer")
	}
	if !strings.Contains(string(files[0].Content), "https://jira.example.com/images/icons/issuetypes/epic.svg") {
		t.Fatal("external resource URL missing from Markdown")
	}
	if requests != 2 {
		t.Fatal(requests)
	}
	page.Version = 2
	if _, err = source.Page(context.Background(), page); err == nil || !strings.Contains(err.Error(), "version changed") {
		t.Fatal(err)
	}
}

func TestConfluenceBoundaryAndErrors(t *testing.T) {
	for _, raw := range []string{"http://example.com/x", "https://other.com/x", "https://user:pass@example.com/x", "https://example.com/../x", "https://example.com/x#fragment"} {
		if ValidateConfluenceURL("https://example.com", raw) == nil {
			t.Fatal(raw)
		}
	}
	if ValidateConfluenceURL("https://example.com/wiki", "https://example.com/wiki/pages/1") != nil {
		t.Fatal("context path rejected")
	}
	for _, raw := range []string{"../x", "assets/../../secret", "assets/asset-123.png", "file.html"} {
		if ValidConfluenceFile(raw) {
			t.Fatal(raw)
		}
	}
	if !ValidConfluenceFile("page.md") || !ValidConfluenceFile("assets/asset-0123456789abcdef01234567.png") {
		t.Fatal("valid files rejected")
	}
	if _, err := decodeTree([]byte(`{"nodes":[],"pages":[{"id":"../1","version":1}]}`)); err == nil {
		t.Fatal("bad plan")
	}
	if _, err := script(context.Background(), "missing-dutybot-executable", "none", nil); err == nil {
		t.Fatal("missing process")
	}
	source := NewConfluenceSource(config.ConfluenceConfig{BaseURL: "invalid"}, "none")
	if _, err := source.Tree(context.Background(), "https://example.com/page"); err == nil {
		t.Fatal("invalid base")
	}
	if _, err := source.Page(context.Background(), domain.ConfluencePage{URL: "bad"}); err == nil {
		t.Fatal("invalid page")
	}
}

type blockingSource struct{}

func (blockingSource) Tree(ctx context.Context, _ string) ([]byte, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (blockingSource) Page(ctx context.Context, _ domain.ConfluencePage) ([]ExportFile, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// 停止请求必须取消正在执行的外部调用，确认停止后才允许继续。
func TestConfluenceStopCancelsActiveSource(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows("queued", "tree", 0))
	mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs("running", "task", "user", "queued").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows("stopping", "tree", 0))
	mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows("stopping", "tree", 0))
	mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs("stopped", "task", "user", "stopping").WillReturnResult(sqlmock.NewResult(0, 1))
	w := ConfluenceWorker{DB: db, Source: blockingSource{}, Objects: &memoryObjects{data: map[string][]byte{}}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.Handle(ctx, asynq.NewTask(TypeConfluenceExport, []byte(`{"id":"task","user_id":"user"}`))); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
