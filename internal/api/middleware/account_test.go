package middleware

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/zhaocore/agentdock/internal/account"
)

func TestAccountAuthBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, status, path string
		token, must        bool
		version            int64
		dbErr              error
		want               int
	}{
		{name: "active", status: "active", path: "/v1/agents", want: 200},
		{name: "revoked JWT", status: "active", path: "/v1/agents", version: 1, want: 401},
		{name: "disabled", status: "disabled", path: "/v1/agents", want: 401},
		{name: "deleted", dbErr: sql.ErrNoRows, want: 401},
		{name: "unavailable", dbErr: errors.New("mysql down"), want: 503},
		{name: "temporary", status: "active", must: true, path: "/v1/agents", want: 403},
		{name: "temporary me", status: "active", must: true, path: "/v1/auth/me", want: 200},
		{name: "temporary change", status: "active", must: true, path: "/v1/auth/change-password", want: 200},
		{name: "temporary token", status: "active", must: true, token: true, path: "/v1/agents", want: 403},
		{name: "token auth", status: "active", token: true, path: "/v1/auth/me", want: 403},
		{name: "token business", status: "active", token: true, path: "/v1/agents", want: 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			q := mock.ExpectQuery("SELECT u.id")
			if tc.dbErr != nil {
				q.WillReturnError(tc.dbErr)
			} else {
				q.WillReturnRows(sqlmock.NewRows([]string{"id", "email", "status", "version", "must", "admin", "created"}).AddRow("u", "u@test.example", tc.status, tc.version, tc.must, false, time.Now()))
			}
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("user_id", "u")
				c.Set("auth_version", int64(0))
				if tc.token {
					c.Set(APITokenIDKey, "token")
				}
			})
			r.Use(AccountAuth(db, account.NewActivity()))
			path := tc.path
			if path == "" {
				path = "/test"
			}
			r.GET(path, func(c *gin.Context) { c.Status(200) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != tc.want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestAdministratorRejectsAllAPITokens(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, token := range []bool{false, true} {
			r := gin.New()
			r.Use(func(c *gin.Context) {
				c.Set("is_admin", admin)
				if token {
					c.Set(APITokenIDKey, "id")
				}
			})
			r.Use(RequireAdministrator())
			r.GET("/admin", func(c *gin.Context) { c.Status(200) })
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/admin", nil))
			want := 403
			if admin && !token {
				want = 200
			}
			if w.Code != want {
				t.Fatal(admin, token, w.Code)
			}
		}
	}
}
