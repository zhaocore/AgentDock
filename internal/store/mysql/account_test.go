package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/zhaocore/agentdock/internal/domain"
)

func accountRow(status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("u", "u@test.example", status, 2, false, false, time.Now())
}
func TestAccountReads(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	m.ExpectQuery("SELECT u.id").WithArgs("u").WillReturnRows(accountRow("active"))
	u, err := GetAccount(ctx, db, "u")
	if err != nil || u.AuthVersion != 2 {
		t.Fatal(u, err)
	}
	m.ExpectQuery("SELECT id,password_hash").WillReturnRows(sqlmock.NewRows([]string{"id", "hash"}).AddRow("u", "hash"))
	m.ExpectQuery("SELECT u.id").WillReturnRows(accountRow("active"))
	u, hash, err := FindAccount(ctx, db, "u@test.example")
	if err != nil || hash != "hash" {
		t.Fatal(u, hash, err)
	}
	m.ExpectQuery("SELECT u.id").WillReturnRows(accountRow("active"))
	list, err := ListAccounts(ctx, db)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	m.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"admin", "enabled"}).AddRow("a", true))
	settings, err := GetAdministration(ctx, db)
	if err != nil || !settings.RegistrationEnabled {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestRegisterSerializesBootstrap(t *testing.T) {
	for _, mode := range []string{"first", "later", "closed", "insert-fails", "lock-fails"} {
		t.Run(mode, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			m.ExpectBegin()
			q := m.ExpectQuery("SELECT admin_user_id,registration_enabled.*FOR UPDATE")
			admin := sql.NullString{String: "a", Valid: mode != "first"}
			if mode == "lock-fails" {
				q.WillReturnError(errors.New("lock failed"))
				m.ExpectRollback()
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"admin", "enabled"}).AddRow(admin, mode != "closed"))
				if mode == "closed" {
					m.ExpectRollback()
				} else {
					q := m.ExpectExec("INSERT INTO users")
					if mode == "insert-fails" {
						q.WillReturnError(errors.New("duplicate"))
						m.ExpectRollback()
					} else {
						q.WillReturnResult(sqlmock.NewResult(0, 1))
						if mode == "first" {
							m.ExpectExec("UPDATE administration").WillReturnResult(sqlmock.NewResult(0, 1))
						}
						m.ExpectCommit()
					}
				}
			}
			u, err := RegisterAccount(context.Background(), db, "u@test.example", "hash")
			if mode == "first" || mode == "later" {
				if err != nil || u.IsAdmin != (mode == "first") {
					t.Fatal(u, err)
				}
			} else if err == nil {
				t.Fatal("expected error")
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAdministrationWrites(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	id, err := CreateAccount(ctx, db, "a", "u@test.example", "hash")
	if err != nil || id == "" {
		t.Fatal(err)
	}
	m.ExpectBegin()
	m.ExpectExec("UPDATE administration").WithArgs(false).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err = SetRegistration(ctx, db, "a", false); err != nil {
		t.Fatal(err)
	}
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = Audit(ctx, db, "a", "u", "read", "succeeded"); err != nil {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestStatusAndPasswordLifecycle(t *testing.T) {
	for _, status := range []string{"active", "disabled", "deleting"} {
		t.Run(status, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			m.ExpectBegin()
			m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admin"))
			m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
			m.ExpectExec("UPDATE users SET status").WithArgs(status, "u").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 2))
			if status != "active" {
				m.ExpectExec("UPDATE crawl_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
			}
			m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit()
			if err := SetAccountStatus(context.Background(), db, "admin", "u", status); err != nil {
				t.Fatal(err)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	if err := SetAccountStatus(ctx, db, "a", "u", "bogus"); err == nil {
		t.Fatal("invalid status accepted")
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
	m.ExpectRollback()
	if err := SetAccountStatus(ctx, db, "a", "a", "disabled"); !errors.Is(err, ErrProtectedAdministrator) {
		t.Fatal(err)
	}
	m.ExpectBegin()
	m.ExpectExec("UPDATE users SET password_hash").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 2))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	if err := ResetAccountPassword(ctx, db, "a", "u", "new"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int64{0, 1} {
		m.ExpectExec("UPDATE users SET password_hash").WithArgs("new", "u", "old").WillReturnResult(sqlmock.NewResult(0, n))
		err := ChangeAccountPassword(ctx, db, "u", "old", "new")
		if (err == nil) != (n == 1) {
			t.Fatal(n, err)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAccountResourcesAndStop(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	m.ExpectQuery("SELECT .*COUNT").WillReturnRows(sqlmock.NewRows([]string{"a", "i", "s", "m", "ct", "im", "cr", "r", "t"}).AddRow(1, 2, 3, 4, 5, 6, 7, 8, 9))
	c, err := AccountResources(ctx, db, "u")
	if err != nil || c.Messages != 4 || c.Tokens != 9 {
		t.Fatal(c, err)
	}
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("i"))
	ids, err := AccountInstanceIDs(ctx, db, "u")
	if err != nil || len(ids) != 1 {
		t.Fatal(ids, err)
	}
	for _, q := range []string{"UPDATE api_tokens", "UPDATE crawl_tasks", "UPDATE crawl_runs", "UPDATE confluence_tasks", "UPDATE agent_instances"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	if err = StopAccountRecords(ctx, db, "u"); err != nil {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAccountDeletionTransaction(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			ctx := context.Background()
			m.ExpectBegin()
			m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow("a"))
			m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
			for _, q := range []string{"UPDATE users SET status", "UPDATE api_tokens", "UPDATE crawl_tasks", "INSERT INTO admin_audit", "INSERT INTO account_deletions"} {
				m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			m.ExpectCommit()
			if err := BeginAccountDeletion(ctx, db, "u", "a", "digest"); err != nil {
				t.Fatal(err)
			}
			m.ExpectBegin()
			m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"admin"}).AddRow("a"))
			m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("deleting"))
			for _, q := range []string{"UPDATE agents", "UPDATE agent_instances", "UPDATE sessions", "UPDATE confluence_tasks", "UPDATE confluence_library_imports", "UPDATE crawl_tasks", "UPDATE crawl_runs", "INSERT INTO confluence_library_imports", "DELETE FROM confluence_library_imports"} {
				m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if fail {
				m.ExpectExec("DELETE FROM users").WillReturnError(errors.New("foreign key failure"))
				m.ExpectRollback()
			} else {
				m.ExpectExec("DELETE FROM users").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE account_deletions").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			}
			err := TransferAccountResources(ctx, db, "u", "a", []domain.ConfluenceLibrarySpace{{ID: "s", Pages: []domain.ConfluenceLibraryPage{}}}, domain.ConfluenceImportReport{})
			if (err != nil) != fail {
				t.Fatal(err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestDeletionProgressAndResourceRemoval(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	m.ExpectExec("UPDATE account_deletions").WillReturnResult(sqlmock.NewResult(0, 1))
	if err := DeletionError(ctx, db, "u", "stop timeout"); err != nil {
		t.Fatal(err)
	}
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error FROM account_deletions").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e"}).AddRow("u", "a", "frozen", ""))
	jobs, err := PendingAccountDeletions(ctx, db)
	if err != nil || len(jobs) != 1 {
		t.Fatal(jobs, err)
	}
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "completed", "", "digest"))
	job, digest, err := GetAccountDeletion(ctx, db, "u")
	if err != nil || job.Stage != "completed" || digest != "digest" {
		t.Fatal(job, err)
	}
	m.ExpectExec("DELETE FROM confluence_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	if err = DeleteManagedConfluence(ctx, db, "u", "t"); err != nil {
		t.Fatal(err)
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(0))
	for _, q := range []string{"DELETE p FROM crawl_pages", "DELETE FROM crawl_runs", "DELETE FROM crawl_tasks"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	m.ExpectCommit()
	if err = DeleteManagedCrawl(ctx, db, "u", "t"); err != nil {
		t.Fatal(err)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAccountRuntimeSingleton(t *testing.T) {
	for _, locked := range []int{0, 1} {
		db, m, _ := sqlmock.New()
		m.ExpectQuery(regexp.QuoteMeta("SELECT GET_LOCK(CONCAT('agentdock-runtime:',DATABASE()),0)")).WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(locked))
		conn, err := LockAccountRuntime(context.Background(), db)
		if (err == nil) != (locked == 1) {
			t.Fatal(err)
		}
		if conn != nil {
			conn.Close()
		}
		db.Close()
	}
}

func TestAccountTransactionsRejectBeginFailures(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	failure := errors.New("transaction unavailable")
	operations := []func() error{
		func() error { _, err := RegisterAccount(ctx, db, "u@example.com", "hash"); return err },
		func() error { _, err := CreateAccount(ctx, db, "a", "u@example.com", "hash"); return err },
		func() error { return SetRegistration(ctx, db, "a", false) },
		func() error { return SetAccountStatus(ctx, db, "a", "u", "disabled") },
		func() error { return ResetAccountPassword(ctx, db, "a", "u", "hash") },
		func() error { return BeginAccountDeletion(ctx, db, "u", "a", "digest") },
		func() error { return TransferAccountResources(ctx, db, "u", "a", nil, domain.ConfluenceImportReport{}) },
		func() error { return DeleteManagedCrawl(ctx, db, "u", "task") },
		func() error { _, err := StopManagedCrawl(ctx, db, "u", "task"); return err },
	}
	for i, operation := range operations {
		m.ExpectBegin().WillReturnError(failure)
		if err := operation(); !errors.Is(err, failure) {
			t.Fatal(i, err)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestFreezeFailuresRollbackWithoutChangingPrivileges(t *testing.T) {
	for _, failureAt := range []string{"settings", "user", "update", "revoke", "tasks", "audit", "deleting"} {
		t.Run(failureAt, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			m.ExpectBegin()
			q := m.ExpectQuery("SELECT admin_user_id")
			failed := false
			fail := errors.New("write failed")
			if failureAt == "settings" {
				q.WillReturnError(fail)
				failed = true
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
			}
			if !failed {
				q = m.ExpectQuery("SELECT status")
				if failureAt == "user" {
					q.WillReturnError(sql.ErrNoRows)
					failed = true
				} else {
					status := "active"
					if failureAt == "deleting" {
						status = "deleting"
						failed = true
					}
					q.WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(status))
				}
			}
			if !failed {
				for _, step := range []struct{ name, query string }{{"update", "UPDATE users SET status"}, {"revoke", "UPDATE api_tokens"}, {"tasks", "UPDATE crawl_tasks"}, {"audit", "INSERT INTO admin_audit"}} {
					e := m.ExpectExec(step.query)
					if step.name == failureAt {
						e.WillReturnError(fail)
						break
					}
					e.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			m.ExpectRollback()
			if err := SetAccountStatus(context.Background(), db, "a", "u", "disabled"); err == nil {
				t.Fatal("freeze failure hidden")
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestReadsAndDeletionFailClosed(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	ctx := context.Background()
	failure := errors.New("read interrupted")
	m.ExpectQuery("SELECT u.id").WillReturnError(failure)
	if _, err := ListAccounts(ctx, db); err == nil {
		t.Fatal("list error hidden")
	}
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnError(failure)
	if _, err := AccountInstanceIDs(ctx, db, "u"); err == nil {
		t.Fatal("stop uncertainty hidden")
	}
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error FROM account_deletions").WillReturnError(failure)
	if _, err := PendingAccountDeletions(ctx, db); err == nil {
		t.Fatal("recovery error hidden")
	}
	m.ExpectExec("DELETE FROM confluence_tasks").WillReturnResult(sqlmock.NewResult(0, 0))
	if err := DeleteManagedConfluence(ctx, db, "u", "t"); err == nil {
		t.Fatal("missing or busy task treated as deleted")
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
	m.ExpectRollback()
	if err := DeleteManagedCrawl(ctx, db, "u", "t"); err == nil {
		t.Fatal("active crawl deleted")
	}
}

func TestTemporaryAccountCreationAndResetRollback(t *testing.T) {
	for _, step := range []string{"create", "audit", "reset-missing", "reset-token", "reset-audit"} {
		t.Run(step, func(t *testing.T) {
			db, m, _ := sqlmock.New()
			defer db.Close()
			ctx := context.Background()
			m.ExpectBegin()
			failure := errors.New("account mutation failed")
			if step == "create" || step == "audit" {
				e := m.ExpectExec("INSERT INTO users")
				if step == "create" {
					e.WillReturnError(failure)
				} else {
					e.WillReturnResult(sqlmock.NewResult(0, 1))
					m.ExpectExec("INSERT INTO admin_audit").WillReturnError(failure)
				}
				m.ExpectRollback()
				if _, err := CreateAccount(ctx, db, "a", "u@test.example", "hash"); err == nil {
					t.Fatal("unaudited account creation succeeded")
				}
			} else {
				e := m.ExpectExec("UPDATE users SET password_hash")
				if step == "reset-missing" {
					e.WillReturnResult(sqlmock.NewResult(0, 0))
				} else {
					e.WillReturnResult(sqlmock.NewResult(0, 1))
					e = m.ExpectExec("UPDATE api_tokens")
					if step == "reset-token" {
						e.WillReturnError(failure)
					} else {
						e.WillReturnResult(sqlmock.NewResult(0, 1))
						m.ExpectExec("INSERT INTO admin_audit").WillReturnError(failure)
					}
				}
				m.ExpectRollback()
				if err := ResetAccountPassword(ctx, db, "a", "u", "hash"); err == nil {
					t.Fatal("incomplete reset succeeded")
				}
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
