package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/domain"
)

const selectionTree = `{"nodes":[{"id":"3","title":"Outside","children":[]},{"id":"10","title":"Root","children":[{"id":"1","title":"One","children":[]},{"id":"20","title":"Branch","children":[{"id":"2","title":"Two","children":[]}]}]}],"pages":[{"id":"3","title":"Outside","url":"https://example.com/3","version":1,"path":"Outside"},{"id":"1","title":"One","url":"https://example.com/1","version":1,"path":"Root/One"},{"id":"2","title":"Two","url":"https://example.com/2","version":1,"path":"Root/Branch/Two"}]}`

type selectionQueue struct{ fail bool }

func (q selectionQueue) EnqueueContext(context.Context, *asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	if q.fail {
		return nil, errors.New("queue down")
	}
	return &asynq.TaskInfo{}, nil
}
func (selectionQueue) Close() error { return nil }

func TestSelectConfluenceExport(t *testing.T) {
	for _, mode := range []string{"subtree", "all", "invalid-node", "not-ready", "claim-conflict", "claim-error", "storage-error", "queue-update-error", "queue-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			objects := &memoryObjects{data: map[string][]byte{"confluence/user/task/tree.json": []byte(selectionTree)}, fail: mode == "storage-error"}
			w := ConfluenceWorker{DB: db, Objects: objects}
			task := domain.ConfluenceTask{ID: "task", UserID: "user", Stage: "selection", Status: "waiting"}
			nodeID, total := "10", 2
			if mode == "all" {
				nodeID, total = "", 3
			}
			if mode == "invalid-node" {
				nodeID = "999"
			}
			if mode == "not-ready" {
				task.Stage = "tree"
			}
			if mode != "not-ready" && mode != "invalid-node" {
				expect := mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs("selecting", "task", "user", "waiting")
				if mode == "claim-error" {
					expect.WillReturnError(errors.New("db down"))
				} else {
					n := int64(1)
					if mode == "claim-conflict" {
						n = 0
					}
					expect.WillReturnResult(sqlmock.NewResult(0, n))
				}
				if mode != "claim-conflict" && mode != "claim-error" {
					if mode != "storage-error" {
						expect := mock.ExpectExec("UPDATE confluence_tasks SET status='queued', stage='markdown'").WithArgs(total, "task", "user")
						if mode == "queue-update-error" {
							expect.WillReturnError(errors.New("db down"))
						} else {
							expect.WillReturnResult(sqlmock.NewResult(0, 1))
						}
					}
					if mode == "storage-error" || mode == "queue-error" || mode == "queue-update-error" {
						mock.ExpectExec("UPDATE confluence_tasks SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
					}
				}
			}
			updated, err := w.SelectExport(context.Background(), task, nodeID, selectionQueue{fail: mode == "queue-error"})
			success := mode == "subtree" || mode == "all"
			if (err == nil) != success {
				t.Fatal(err)
			}
			if success {
				if updated.Status != "queued" || updated.Stage != "markdown" || updated.Total != total {
					t.Fatal(updated)
				}
				plan, err := ReadConfluencePlan(context.Background(), objects, updated)
				if err != nil || len(plan.Pages) != total || plan.SelectedNodeID == nil || *plan.SelectedNodeID != nodeID {
					t.Fatal(plan, err)
				}
			}
			if string(objects.data["confluence/user/task/tree.json"]) != selectionTree {
				t.Fatal("original tree changed")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReadConfluencePlanRejectsMissingScope(t *testing.T) {
	for _, data := range []string{"", "bad JSON", `{}`, `{"node_id":"999"}`} {
		objects := &memoryObjects{data: map[string][]byte{"confluence/user/task/tree.json": []byte(selectionTree)}}
		if data != "" {
			objects.data["confluence/user/task/selection.json"] = []byte(data)
		}
		task := domain.ConfluenceTask{ID: "task", UserID: "user", Stage: "markdown"}
		if _, err := ReadConfluencePlan(context.Background(), objects, task); err == nil {
			t.Fatal(data)
		}
	}
}

func TestSelectedMarkdownCheckpoint(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	objects := &memoryObjects{data: map[string][]byte{"confluence/user/task/tree.json": []byte(selectionTree), "confluence/user/task/selection.json": []byte(`{"node_id":"20"}`)}}
	source := &exportSource{}
	w := ConfluenceWorker{DB: db, Objects: objects, Source: source}
	task := domain.ConfluenceTask{ID: "task", UserID: "user", Stage: "markdown", Total: 1}
	mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("markdown", 1, 1, "task", "user").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := w.run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if len(objects.data["confluence/user/task/2/page.md"]) == 0 || len(objects.data["confluence/user/task/1/page.md"]) != 0 || source.pages != 1 {
		t.Fatal("export escaped selected subtree")
	}
	task.Completed = 1
	if err := w.run(context.Background(), task); err != nil || source.pages != 1 {
		t.Fatal("resume did not preserve scope", err)
	}
	task.Stage = "selection"
	if err := w.run(context.Background(), task); err == nil {
		t.Fatal("selection bypassed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConfluenceHandleWaitsAfterTree(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows("queued", "tree", 0))
	mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs("running", "task", "user", "queued").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("selection", 0, 1, "task", "user").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT .*confluence_tasks WHERE id").WillReturnRows(taskRows("running", "selection", 0))
	mock.ExpectExec("UPDATE confluence_tasks SET status").WithArgs("waiting", "task", "user", "running").WillReturnResult(sqlmock.NewResult(0, 1))
	source := &exportSource{tree: []byte(treeFixture)}
	w := ConfluenceWorker{DB: db, Objects: &memoryObjects{data: map[string][]byte{}}, Source: source}
	payload, _ := json.Marshal(confluencePayload{"task", "user"})
	if err := w.Handle(context.Background(), asynq.NewTask(TypeConfluenceExport, payload)); err != nil || source.pages != 0 {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 完成后切换节点创建独立批次，原范围与文件仍可读取。
func TestCompletedConfluenceSelectionStartsNextBatch(t *testing.T) {
	for _, mode := range []string{"selected", "all", "copy-error", "insert-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			ctx := context.Background()
			task := domain.ConfluenceTask{ID: "task", UserID: "user", URL: "https://example.com/1", Stage: "markdown", Status: "completed", Completed: 1, Total: 1}
			prefix := ConfluencePrefix(task)
			objects := &memoryObjects{data: map[string][]byte{prefix + "tree.json": []byte(selectionTree), prefix + "selection.json": []byte(`{"node_id":"1"}`), prefix + "1/page.md": []byte("# Previous")}, fail: mode == "copy-error"}
			source := &exportSource{}
			w := ConfluenceWorker{DB: db, Objects: objects, Source: source}
			nodeID, total := "20", 1
			if mode == "all" {
				nodeID, total = "", 3
			}
			if mode != "copy-error" {
				expect := mock.ExpectExec("INSERT INTO confluence_tasks").WithArgs(sqlmock.AnyArg(), "user", task.URL, 3)
				if mode == "insert-error" {
					expect.WillReturnError(errors.New("db down"))
				} else {
					expect.WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs("selecting", sqlmock.AnyArg(), "user", "waiting").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE confluence_tasks SET status='queued', stage='markdown'").WithArgs(total, sqlmock.AnyArg(), "user").WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			updated, err := w.SelectExport(ctx, task, nodeID, selectionQueue{fail: false})
			if mode == "copy-error" || mode == "insert-error" {
				if err == nil || updated.ID != task.ID {
					t.Fatal(updated, err)
				}
			} else {
				if err != nil || updated.ID == task.ID || updated.Status != "queued" || updated.Completed != 0 || updated.Total != total || updated.UserID != task.UserID {
					t.Fatal(updated, err)
				}
				if string(objects.data[ConfluencePrefix(updated)+"tree.json"]) != selectionTree {
					t.Fatal("frozen tree changed")
				}
				mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("markdown", sqlmock.AnyArg(), total, updated.ID, "user").WillReturnResult(sqlmock.NewResult(0, 1))
				if mode == "all" {
					mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("markdown", 2, total, updated.ID, "user").WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec("UPDATE confluence_tasks SET stage").WithArgs("markdown", 3, total, updated.ID, "user").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				if err := w.run(ctx, updated); err != nil || source.pages != total {
					t.Fatal(err, source.pages)
				}
			}
			plan, err := ReadConfluencePlan(ctx, objects, task)
			if err != nil || len(plan.Pages) != 1 || plan.Pages[0].ID != "1" || string(objects.data[prefix+"1/page.md"]) != "# Previous" {
				t.Fatal("previous batch changed", plan, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
