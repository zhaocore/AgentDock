package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/domain"
)

type accountInstances struct {
	running bool
	stopErr error
}

func (i *accountInstances) IsRunning(string) bool              { return i.running }
func (i *accountInstances) Stop(context.Context, string) error { i.running = false; return i.stopErr }
func expectCounts(m sqlmock.Sqlmock, exports, imports int) {
	m.ExpectQuery("SELECT .*COUNT").WillReturnRows(sqlmock.NewRows([]string{"a", "i", "s", "m", "ct", "im", "cr", "r", "t"}).AddRow(0, 0, 0, 0, exports, imports, 0, 0, 0))
}
func TestAccountWorkGate(t *testing.T) {
	for _, mode := range []string{"active", "disabled", "db-failed", "bad-payload", "missing-user"} {
		t.Run(mode, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			s := &Server{db: db, Activity: account.NewActivity()}
			payload := []byte(`{"id":"run","user_id":"owner"}`)
			switch mode {
			case "bad-payload":
				payload = []byte("{")
			case "missing-user":
				payload = []byte(`{"id":"run"}`)
			default:
				q := m.ExpectQuery("SELECT u.id")
				if mode == "db-failed" {
					q.WillReturnError(errors.New("database down"))
				} else {
					q.WillReturnRows(sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("owner", "u@test.example", mode, 0, false, false, time.Now()))
				}
			}
			called := false
			err := s.accountTask(func(ctx context.Context, task *asynq.Task) error { called = true; return nil })(context.Background(), asynq.NewTask(TypeCrawlRun, payload))
			if called != (mode == "active") || (err == nil) != (mode == "active") {
				t.Fatal(called, err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAccountStopAndPreview(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	instances := &accountInstances{running: true}
	s := &AccountService{DB: db, Activity: account.NewActivity(), Instances: instances}
	expectCounts(m, 0, 0)
	p, spaces, err := s.Preview(context.Background(), "a", "u")
	if err != nil || len(p.Digest) != 64 || len(spaces) != 0 {
		t.Fatal(p, err)
	}
	if _, _, err = s.Preview(context.Background(), "a", "a"); err == nil {
		t.Fatal("self deletion accepted")
	}
	expectCounts(m, 1, 0)
	if _, _, err = s.Preview(context.Background(), "a", "u"); err == nil {
		t.Fatal("missing storage accepted")
	}
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("i"))
	for _, q := range []string{"UPDATE api_tokens", "UPDATE crawl_tasks", "UPDATE crawl_runs", "UPDATE confluence_tasks", "UPDATE agent_instances"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if err = s.StopUser(context.Background(), "u"); err != nil || instances.running {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAccountMergeUsesExistingVersionAndConflictRules(t *testing.T) {
	for _, mode := range []string{"added", "updated", "older", "equal", "conflict", "directory"} {
		t.Run(mode, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			objects := libraryObjects()
			s := &ConfluenceLibrary{DB: db, Objects: objects}
			at := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
			spaceID := domain.ConfluenceSpaceID("https://example.com", "TEAM")
			old := domain.ConfluenceLibraryPage{ID: "1", Title: "One", URL: "https://example.com/1", Version: 1, Path: "Root/One", ModifiedAt: at, ObjectPrefix: "admin-page/"}
			incoming := old
			incoming.ObjectPrefix = "member-page/"
			switch mode {
			case "updated":
				incoming.ModifiedAt = at.Add(time.Hour)
			case "older":
				incoming.ModifiedAt = at.Add(-time.Hour)
			case "conflict":
				incoming.Version = 2
			case "directory":
				incoming.ID = "2"
			}
			objects.data["admin-page/page.md"] = []byte("original")
			objects.data["member-page/page.md"] = []byte("original")
			current := domain.ConfluenceImport{ID: "old", UserID: "admin", Status: "applied", Spaces: []domain.ConfluenceLibrarySpace{{ID: spaceID, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceLibraryPage{old}}}, CreatedAt: at}
			fresh := domain.ConfluenceImport{ID: "new", UserID: "member", Status: "applied", Spaces: []domain.ConfluenceLibrarySpace{{ID: spaceID, BaseURL: "https://example.com", Key: "TEAM", Pages: []domain.ConfluenceLibraryPage{incoming}}}, CreatedAt: at}
			rows := importRows(current)
			if mode == "added" {
				rows = sqlmock.NewRows(libraryImportColumns)
			}
			expectLibraryCatalog(m, "admin", sqlmock.NewRows(taskColumns), rows)
			expectLibraryCatalog(m, "member", sqlmock.NewRows(taskColumns), importRows(fresh))
			report, selected, digest, err := s.AccountMergePreview(context.Background(), "admin", "member")
			if err != nil || len(digest) != 64 {
				t.Fatal(report, err)
			}
			switch mode {
			case "added":
				if report.Added != 1 || len(selected) != 1 {
					t.Fatal(report)
				}
			case "updated":
				if report.Updated != 1 {
					t.Fatal(report)
				}
			case "older", "equal":
				if report.Kept != 1 || len(selected) != 0 {
					t.Fatal(report)
				}
			default:
				if report.Conflicts != 1 || len(selected) != 0 {
					t.Fatal(report)
				}
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDeletionCompletedRetryAndRecoveryFailure(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := &AccountService{DB: db, Activity: account.NewActivity(), Instances: &accountInstances{}}
	m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"l"}).AddRow(1))
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "completed", "", "d"))
	m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"l"}).AddRow(1))
	if err := s.Delete(context.Background(), "a", "u", "d"); err != nil {
		t.Fatal(err)
	}
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error FROM account_deletions").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e"}).AddRow("u", "a", "frozen", ""))
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "frozen", "", "d"))
	m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"l"}).AddRow(1))
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "frozen", "", "d"))
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnError(errors.New("storage offline"))
	m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"l"}).AddRow(1))
	m.ExpectExec("UPDATE account_deletions").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := s.Recover(context.Background()); err == nil {
		t.Fatal("recovery error hidden")
	}
}
func TestPreservedObjectPrefixes(t *testing.T) {
	c := domain.ConfluenceTask{ID: "task", UserID: "admin", StorageUserID: "previous"}
	if ConfluencePrefix(c) != "confluence/previous/task/" {
		t.Fatal(ConfluencePrefix(c))
	}
	r := domain.CrawlRun{ID: "run", UserID: "admin", StorageUserID: "previous"}
	if CrawlPrefix(r) != "crawl/previous/run/" {
		t.Fatal(CrawlPrefix(r))
	}
}

func expectAccountFreeze(m sqlmock.Sqlmock) {
	m.ExpectBegin()
	m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow("a"))
	m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	for _, q := range []string{"UPDATE users SET status", "UPDATE api_tokens", "UPDATE crawl_tasks", "INSERT INTO admin_audit", "INSERT INTO account_deletions"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	m.ExpectCommit()
}
func expectAccountStop(m sqlmock.Sqlmock) {
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	for _, q := range []string{"UPDATE api_tokens", "UPDATE crawl_tasks", "UPDATE crawl_runs", "UPDATE confluence_tasks", "UPDATE agent_instances"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
}
func expectAccountTransfer(m sqlmock.Sqlmock) {
	m.ExpectBegin()
	m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow("a"))
	m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("deleting"))
	for _, q := range []string{"UPDATE agents", "UPDATE agent_instances", "UPDATE sessions", "UPDATE confluence_tasks", "UPDATE confluence_library_imports", "UPDATE crawl_tasks", "UPDATE crawl_runs", "DELETE FROM confluence_library_imports", "DELETE FROM users", "UPDATE account_deletions", "INSERT INTO admin_audit"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	m.ExpectCommit()
}
func TestAccountDeletionCommitsOnlyAfterQuiescenceAndMatchingPreview(t *testing.T) {
	for _, mode := range []string{"complete", "changed-after-stop", "stop-fails"} {
		t.Run(mode, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			s := &AccountService{DB: db, Activity: account.NewActivity(), Instances: &accountInstances{}}
			expectCounts(m, 0, 0)
			preview, _, err := s.Preview(context.Background(), "a", "u")
			if err != nil {
				t.Fatal(err)
			}
			m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
			m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}))
			expectCounts(m, 0, 0)
			expectAccountFreeze(m)
			if mode == "stop-fails" {
				m.ExpectQuery("SELECT id FROM agent_instances").WillReturnError(errors.New("cannot confirm stop"))
			} else {
				expectAccountStop(m)
				m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
				if mode == "changed-after-stop" {
					m.ExpectQuery("SELECT .*COUNT").WillReturnRows(sqlmock.NewRows([]string{"a", "i", "s", "m", "ct", "im", "cr", "r", "t"}).AddRow(1, 0, 0, 0, 0, 0, 0, 0, 0))
				} else {
					expectCounts(m, 0, 0)
					expectAccountTransfer(m)
				}
				m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
			}
			if mode != "complete" {
				m.ExpectExec("UPDATE account_deletions").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
			err = s.Delete(context.Background(), "a", "u", preview.Digest)
			if (err == nil) != (mode == "complete") {
				t.Fatal(mode, err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAccountStatusAndExportDeletionRequireStoppedWriters(t *testing.T) {
	for _, state := range []string{"active", "disabled", "administrator"} {
		t.Run(state, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			s := &AccountService{DB: db, Activity: account.NewActivity(), Instances: &accountInstances{}}
			m.ExpectQuery("SELECT u.id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("u", "u@test.example", state, 1, false, state == "administrator", time.Now()))
			if state == "disabled" {
				expectAccountStop(m)
				m.ExpectBegin()
				m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
				m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("disabled"))
				m.ExpectExec("UPDATE users SET status").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			}
			err := s.ChangeStatus(context.Background(), "a", "u", "active")
			if (err != nil) != (state == "administrator") {
				t.Fatal(err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, status := range []string{"running", "stopped"} {
		db, m, _ := sqlmock.New()
		s := &AccountService{DB: db, Activity: account.NewActivity()}
		m.ExpectQuery("SELECT .*FROM confluence_tasks").WillReturnRows(taskRows(status, "tree", 0))
		if status == "stopped" {
			m.ExpectExec("DELETE FROM confluence_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
		}
		err := s.DeleteConfluenceTask(context.Background(), "user", "task")
		if (err == nil) != (status == "stopped") {
			t.Fatal(status, err)
		}
		db.Close()
	}
}
