package api

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/account"
	"golang.org/x/crypto/bcrypt"
)

func adminContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	c, w := agentCtx("POST", "/admin", body)
	c.Set("actor_id", "a")
	c.Set("user_id", "u")
	return c, w
}
func adminAccountRows(status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("u", "u@test.example", status, 0, false, false, time.Now())
}
func TestAdminReadAndSettings(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	m.ExpectQuery("SELECT u.id").WillReturnRows(adminAccountRows("active"))
	c, w := adminContext("")
	h.AdminUsers(c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "u@test.example") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, fn := range []func(*gin.Context){h.AdminSettings, h.RegistrationStatus} {
		m.ExpectQuery("SELECT COALESCE").WillReturnRows(sqlmock.NewRows([]string{"admin", "enabled"}).AddRow("a", true))
		c, w = adminContext("")
		fn(c)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
	m.ExpectBegin()
	m.ExpectExec("UPDATE administration").WithArgs(false).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	c, w = adminContext(`{"registration_enabled":false}`)
	h.AdminSetRegistration(c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminCreateAndReset(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	m.ExpectBegin()
	m.ExpectExec("INSERT INTO users").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	c, w := adminContext(`{"email":"new@test.example","password":"temporary"}`)
	h.AdminCreateUser(c)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
	m.ExpectBegin()
	m.ExpectExec("UPDATE users SET password_hash").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	c, w = adminContext(`{"password":"temporary2"}`)
	h.AdminResetPassword(c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	c, w = adminContext(`{"password":"temporary2"}`)
	c.Set("user_id", "a")
	h.AdminResetPassword(c)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminStatusDisablesAndQuiesces(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	h.Activity = account.NewActivity()
	h.Instances = &notRunningManager{}
	m.ExpectBegin()
	m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
	m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("active"))
	m.ExpectExec("UPDATE users SET status").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE crawl_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT id FROM agent_instances").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("i"))
	for _, q := range []string{"UPDATE api_tokens", "UPDATE crawl_tasks", "UPDATE crawl_runs", "UPDATE confluence_tasks", "UPDATE agent_instances"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	c, w := adminContext(`{"status":"disabled"}`)
	h.AdminStatus(c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminRejectsInvalidBodies(t *testing.T) {
	h, _ := setupAgentHandler()
	defer h.DB.Close()
	for _, fn := range []func(*gin.Context){h.AdminCreateUser, h.AdminSetRegistration, h.AdminStatus, h.AdminResetPassword, h.AdminDeleteUser} {
		c, w := adminContext(`{}`)
		fn(c)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestAdminErrorsAndDeletionStatus(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "completed", "", "d"))
	c, w := adminContext("")
	h.AdminDeletion(c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	for _, tc := range []struct {
		err  error
		want int
	}{{sql.ErrNoRows, 404}, {errors.New("Duplicate entry"), 409}, {errors.New("unavailable"), 500}} {
		c, w := adminContext("")
		adminError(c, tc.err)
		if w.Code != tc.want {
			t.Fatal(w.Code)
		}
	}
	m.ExpectQuery("SELECT COALESCE").WillReturnError(errors.New("database unavailable"))
	c, w = adminContext("")
	h.RegistrationStatus(c)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}
func TestLoginAndForcedPasswordChange(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	hash, _ := bcrypt.GenerateFromPassword([]byte("temporary"), bcrypt.MinCost)
	m.ExpectQuery("SELECT id,password_hash").WillReturnRows(sqlmock.NewRows([]string{"id", "hash"}).AddRow("u", hash))
	m.ExpectQuery("SELECT u.id").WillReturnRows(adminAccountRows("active"))
	c, w := adminContext(`{"email":"u@test.example","password":"temporary"}`)
	h.Login(c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "token") {
		t.Fatal(w.Code, w.Body.String())
	}
	m.ExpectQuery("SELECT password_hash").WillReturnRows(sqlmock.NewRows([]string{"hash"}).AddRow(hash))
	m.ExpectExec("UPDATE users SET password_hash").WillReturnResult(sqlmock.NewResult(0, 1))
	c, w = adminContext(`{"old_password":"temporary","new_password":"permanent"}`)
	h.ChangePassword(c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAdminTargetPreservesActorAndRejectsInvalidID(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	id := "11111111-1111-4111-8111-111111111111"
	m.ExpectExec("INSERT INTO admin_audit").WithArgs("actor", id, "GET /users/:user_id", "started").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("INSERT INTO admin_audit").WithArgs("actor", id, "GET /users/:user_id", "succeeded").WillReturnResult(sqlmock.NewResult(0, 1))
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("actor_id", "actor"); c.Set("user_id", "actor") })
	r.GET("/users/:user_id", h.adminAudit(), h.adminTarget(), func(c *gin.Context) {
		if c.GetString("actor_id") != "actor" || c.GetString("user_id") != id {
			t.Error("identity scope leaked")
		}
		c.Status(200)
	})
	for _, tc := range []struct {
		id   string
		want int
	}{{id, 200}, {"not-uuid", 400}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/users/"+tc.id, nil))
		if w.Code != tc.want {
			t.Fatal(w.Code)
		}
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminDeletionPreviewAndCompletedRetry(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	h.Activity = account.NewActivity()
	m.ExpectQuery("SELECT u.id").WillReturnRows(adminAccountRows("disabled"))
	m.ExpectQuery("SELECT .*COUNT").WillReturnRows(sqlmock.NewRows([]string{"a", "i", "s", "m", "ct", "im", "cr", "r", "t"}).AddRow(0, 0, 1, 3, 0, 0, 0, 0, 0))
	c, w := adminContext("")
	h.AdminDeletionPreview(c)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"messages":3`) {
		t.Fatal(w.Code, w.Body.String())
	}
	m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	m.ExpectQuery("SELECT user_id,admin_user_id,stage,error,preview_digest").WillReturnRows(sqlmock.NewRows([]string{"u", "a", "s", "e", "d"}).AddRow("u", "a", "completed", "", "digest"))
	m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	c, w = adminContext(`{"digest":"` + strings.Repeat("a", 64) + `"}`)
	h.AdminDeleteUser(c)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestAdminManagedTaskStopAndDelete(t *testing.T) {
	h, m := setupAgentHandler()
	defer h.DB.Close()
	h.Activity = account.NewActivity()
	m.ExpectBegin()
	m.ExpectQuery("SELECT id FROM crawl_tasks").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(crawlAPIID))
	m.ExpectExec("UPDATE crawl_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT id FROM crawl_runs").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("run"))
	m.ExpectExec("UPDATE crawl_runs").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	c, w := adminContext("")
	c.Params = gin.Params{{Key: "task_id", Value: crawlAPIID}}
	h.AdminStopCrawl(c)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT id FROM crawl_tasks").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(crawlAPIID))
	m.ExpectExec("UPDATE crawl_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectQuery("SELECT id FROM crawl_runs").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	m.ExpectExec("UPDATE crawl_runs").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectBegin()
	m.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
	for _, q := range []string{"DELETE p FROM crawl_pages", "DELETE FROM crawl_runs", "DELETE FROM crawl_tasks"} {
		m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	m.ExpectCommit()
	c, w = adminContext("")
	c.Params = gin.Params{{Key: "task_id", Value: crawlAPIID}}
	h.AdminDeleteCrawl(c)
	c.Writer.WriteHeaderNow()
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	m.ExpectQuery("SELECT .*FROM confluence_tasks").WillReturnRows(exportRows("stopped", "tree", 0))
	m.ExpectExec("DELETE FROM confluence_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	c, w = adminContext("")
	c.Params = gin.Params{{Key: "task_id", Value: exportTaskID}}
	h.AdminDeleteConfluence(c)
	c.Writer.WriteHeaderNow()
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	m.ExpectQuery("SELECT GET_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	m.ExpectBegin()
	m.ExpectExec("UPDATE confluence_tasks").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectExec("UPDATE confluence_library_imports").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	m.ExpectQuery("SELECT RELEASE_LOCK").WillReturnRows(sqlmock.NewRows([]string{"lock"}).AddRow(1))
	c, w = adminContext("")
	h.AdminDeleteLibrary(c)
	c.Writer.WriteHeaderNow()
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminStorageFailuresRemainExplicit(t *testing.T) {
	for _, fn := range []func(*Handler, *gin.Context){func(h *Handler, c *gin.Context) { h.AdminUsers(c) }, func(h *Handler, c *gin.Context) { h.AdminSettings(c) }, func(h *Handler, c *gin.Context) { h.AdminDeletion(c) }, func(h *Handler, c *gin.Context) { h.AdminDeletionPreview(c) }} {
		h, m := setupAgentHandler()
		m.ExpectQuery("SELECT").WillReturnError(errors.New("database unavailable"))
		c, w := adminContext("")
		fn(h, c)
		if w.Code != 500 {
			t.Fatal(w.Code, w.Body.String())
		}
		h.DB.Close()
	}
	h, m := setupAgentHandler()
	defer h.DB.Close()
	m.ExpectBegin()
	m.ExpectExec("UPDATE administration").WillReturnError(errors.New("write unavailable"))
	m.ExpectRollback()
	c, w := adminContext(`{"registration_enabled":false}`)
	h.AdminSetRegistration(c)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	m.ExpectBegin()
	m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("u"))
	m.ExpectRollback()
	c, w = adminContext(`{"status":"disabled"}`)
	h.AdminStatus(c)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	m.ExpectBegin()
	m.ExpectExec("UPDATE users SET password_hash").WillReturnError(errors.New("reset unavailable"))
	m.ExpectRollback()
	c, w = adminContext(`{"password":"temporary"}`)
	h.AdminResetPassword(c)
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
}

func TestEnableRequiresQuiescenceAndPreservesActiveAccount(t *testing.T) {
	for _, state := range []string{"disabled", "active", "administrator"} {
		t.Run(state, func(t *testing.T) {
			h, m := setupAgentHandler()
			defer h.DB.Close()
			h.Activity = account.NewActivity()
			h.Instances = &notRunningManager{}
			admin := state == "administrator"
			m.ExpectQuery("SELECT u.id").WillReturnRows(sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("u", "u@test.example", state, 1, false, admin, time.Now()))
			if state == "disabled" {
				m.ExpectQuery("SELECT id FROM agent_instances").WillReturnRows(sqlmock.NewRows([]string{"id"}))
				for _, q := range []string{"UPDATE api_tokens", "UPDATE crawl_tasks", "UPDATE crawl_runs", "UPDATE confluence_tasks", "UPDATE agent_instances"} {
					m.ExpectExec(q).WillReturnResult(sqlmock.NewResult(0, 1))
				}
				m.ExpectBegin()
				m.ExpectQuery("SELECT admin_user_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("a"))
				m.ExpectQuery("SELECT status").WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("disabled"))
				m.ExpectExec("UPDATE users SET status").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE api_tokens").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("INSERT INTO admin_audit").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			}
			c, w := adminContext(`{"status":"active"}`)
			h.AdminStatus(c)
			want := 200
			if admin {
				want = 403
			}
			if w.Code != want {
				t.Fatal(w.Code, w.Body.String())
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
