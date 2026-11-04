package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
)

func TestConfluenceStore(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	task := domain.ConfluenceTask{ID: "task", UserID: "owner", URL: "https://example.com/page"}
	mock.ExpectExec("INSERT INTO confluence_tasks").WithArgs(task.ID, task.UserID, task.URL).WillReturnResult(sqlmock.NewResult(1, 1))
	if err := CreateConfluenceTask(ctx, db, task); err != nil {
		t.Fatal(err)
	}
	cols := []string{"id", "user_id", "url", "status", "stage", "completed", "total", "error", "created_at", "updated_at"}
	row := func() *sqlmock.Rows {
		return sqlmock.NewRows(cols).AddRow("task", "owner", task.URL, "stopped", "export", 1, 2, "", time.Now(), time.Now())
	}
	mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs("task", "owner").WillReturnRows(row())
	actual, err := GetConfluenceTask(ctx, db, "task", "owner")
	if err != nil || actual.Completed != 1 {
		t.Fatal(actual, err)
	}
	mock.ExpectQuery("SELECT .* WHERE id=\\? AND user_id=\\?").WithArgs("task", "other").WillReturnRows(sqlmock.NewRows(cols))
	if _, err = GetConfluenceTask(ctx, db, "task", "other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT .* WHERE user_id=\\?").WithArgs("owner").WillReturnRows(row())
	tasks, err := ListConfluenceTasks(ctx, db, "owner")
	if err != nil || len(tasks) != 1 {
		t.Fatal(tasks, err)
	}
	mock.ExpectQuery("SELECT .* WHERE user_id=\\?").WithArgs("owner").WillReturnError(errors.New("db down"))
	if _, err = ListConfluenceTasks(ctx, db, "owner"); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WithArgs("queued", "task", "owner", "stopped").WillReturnResult(sqlmock.NewResult(0, 1))
	if changed, err := TransitionConfluenceTask(ctx, db, "task", "owner", "stopped", "queued"); err != nil || !changed {
		t.Fatal(changed, err)
	}
	mock.ExpectExec("UPDATE confluence_tasks SET status=\\?").WillReturnError(errors.New("db down"))
	if _, err = TransitionConfluenceTask(ctx, db, "task", "owner", "stopped", "queued"); err == nil {
		t.Fatal("missing error")
	}
	mock.ExpectExec("UPDATE confluence_tasks SET stage=\\?").WithArgs("export", 2, 2, "task", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = ProgressConfluenceTask(ctx, db, "task", "owner", "export", 2, 2); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE confluence_tasks SET status='failed'").WithArgs("failed request", "task", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = FailConfluenceTask(ctx, db, "task", "owner", "failed request"); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("UPDATE confluence_tasks SET status='stopped'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE confluence_tasks SET status='waiting'").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = ResetConfluenceTasks(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestQueueConfluenceSelection(t *testing.T) {
	for _, mode := range []string{"queued", "changed", "error"} {
		db, mock, _ := sqlmock.New()
		expect := mock.ExpectExec("UPDATE confluence_tasks SET status='queued', stage='markdown'").WithArgs(2, "task", "owner")
		if mode == "error" {
			expect.WillReturnError(errors.New("db down"))
		} else {
			rows := int64(1)
			if mode == "changed" {
				rows = 0
			}
			expect.WillReturnResult(sqlmock.NewResult(0, rows))
		}
		changed, err := QueueConfluenceSelection(context.Background(), db, "task", "owner", 2)
		if changed != (mode == "queued") || (err != nil) != (mode == "error") {
			t.Fatal(mode, changed, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
