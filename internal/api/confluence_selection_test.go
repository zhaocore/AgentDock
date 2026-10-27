package api

import (
	"database/sql/driver"
	"encoding/json"
	"github.com/zhaocore/dutybotgo/internal/domain"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/dutybotgo/internal/config"
)

const apiSelectionTree = `{"nodes":[{"id":"10","title":"Root","children":[{"id":"1","title":"One","children":[]},{"id":"2","title":"Two","children":[]}]}],"pages":[{"id":"1","title":"One","version":1,"path":"Root/One"},{"id":"2","title":"Two","version":1,"path":"Root/Two"}]}`

func TestSelectConfluenceExportAPI(t *testing.T) {
	for _, mode := range []string{"all", "completed", "subtree", "unknown-node", "missing-node", "not-owner", "not-ready", "queue-error"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			prefix := "confluence/owner/" + exportTaskID + "/"
			objects := &libraryAPIObjects{data: map[string][]byte{prefix + "tree.json": []byte(apiSelectionTree)}}
			h := &Handler{DB: db, Config: &config.Config{Confluence: config.ConfluenceConfig{Cookie: "test"}}, ConfluenceObjects: objects, AsynqClient: confluenceQueue{fail: mode == "queue-error"}}
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Set("user_id", "owner")
			c.Params = gin.Params{{Key: "task_id", Value: exportTaskID}}
			expect := mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs(exportTaskID, "owner")
			rows := exportRows("waiting", "selection", 0)
			if mode == "completed" {
				rows = exportRows("completed", "markdown", 1)
			}
			if mode == "not-owner" {
				rows = sqlmock.NewRows(exportCols)
			}
			if mode == "not-ready" {
				rows = exportRows("running", "tree", 0)
			}
			expect.WillReturnRows(rows)
			body, total, want := `{"node_id":""}`, 2, 202
			switch mode {
			case "subtree":
				body, total = `{"node_id":"2"}`, 1
			case "unknown-node":
				body, want = `{"node_id":"999"}`, 400
			case "missing-node":
				body, want = `{}`, 400
			case "not-owner":
				want = 404
			case "not-ready":
				want = 409
			case "queue-error":
				want = 502
			}
			if want == 202 || mode == "queue-error" {
				var mockID driver.Value = exportTaskID
				if mode == "completed" {
					mock.ExpectExec("INSERT INTO confluence_tasks").WithArgs(sqlmock.AnyArg(), "owner", sqlmock.AnyArg(), 2).WillReturnResult(sqlmock.NewResult(0, 1))
					mockID = sqlmock.AnyArg()
				}
				mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs("selecting", mockID, "owner", "waiting").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE confluence_tasks SET status='queued', stage='markdown'").WithArgs(total, mockID, "owner").WillReturnResult(sqlmock.NewResult(0, 1))
				if mode == "queue-error" {
					mock.ExpectExec("UPDATE confluence_tasks SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
			h.SelectConfluenceExport(c)
			if response.Code != want {
				t.Fatal(response.Code, response.Body.String())
			}
			if want == 202 && !strings.Contains(response.Body.String(), `"stage":"markdown"`) {
				t.Fatal(response.Body.String())
			}
			if mode == "completed" {
				var next domain.ConfluenceTask
				if err := json.Unmarshal(response.Body.Bytes(), &next); err != nil || next.ID == exportTaskID || next.Completed != 0 {
					t.Fatal(next, err)
				}
				if string(objects.data["confluence/owner/"+next.ID+"/tree.json"]) != apiSelectionTree {
					t.Fatal("next batch tree missing")
				}
			}
			if string(objects.data[prefix+"tree.json"]) != apiSelectionTree {
				t.Fatal("original tree changed")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectedConfluenceFileVisibility(t *testing.T) {
	for _, pageID := range []string{"1", "2"} {
		db, mock, _ := sqlmock.New()
		prefix := "confluence/owner/" + exportTaskID + "/"
		objects := &libraryAPIObjects{data: map[string][]byte{prefix + "tree.json": []byte(apiSelectionTree), prefix + "selection.json": []byte(`{"node_id":"2"}`), prefix + "2/page.md": []byte("# Selected")}}
		h := &Handler{DB: db, Config: &config.Config{Confluence: config.ConfluenceConfig{Cookie: "test"}}, ConfluenceObjects: objects}
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Set("user_id", "owner")
		c.Params = gin.Params{{Key: "task_id", Value: exportTaskID}, {Key: "page_id", Value: pageID}}
		c.Request = httptest.NewRequest("GET", "/?name=page.md", nil)
		mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs(exportTaskID, "owner").WillReturnRows(exportRows("completed", "markdown", 1))
		h.ConfluenceFile(c)
		want := 404
		if pageID == "2" {
			want = 200
		}
		if response.Code != want {
			t.Fatal(pageID, response.Code, response.Body.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
