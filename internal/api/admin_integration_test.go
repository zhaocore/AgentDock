//go:build integration

package api

import (
	"context"
	"database/sql"
	"encoding/json"

	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/worker"
	"golang.org/x/crypto/bcrypt"
)

// TestAdministrationMySQL 使用明确独立空库验证并发引导、即时撤销、归属转移和旧对象读取。
func TestAdministrationMySQL(t *testing.T) {
	dsn := os.Getenv("MYSQL_ADMIN_TEST_DSN")
	if dsn == "" {
		t.Skip("MYSQL_ADMIN_TEST_DSN must point to independent empty agentdock_admin_test_* database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var name string
	if err = db.QueryRow(`SELECT DATABASE()`).Scan(&name); err != nil || !strings.HasPrefix(name, "agentdock_admin_test_") {
		t.Fatal("refuse non-test database", name, err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE()`).Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty test database", err)
	}
	if err = (&mysql.Store{DB: db}).RunMigrations("../../migrations"); err != nil {
		t.Fatal(err)
	}
	t.Log("migrations complete")
	hash, _ := bcrypt.GenerateFromPassword([]byte("temporary123"), bcrypt.MinCost)
	var group sync.WaitGroup
	accounts := make(chan domain.Account, 8)
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		group.Go(func() {
			u, err := mysql.RegisterAccount(ctx, db, uuid.NewString()+"@test.example", string(hash))
			if err != nil {
				failures <- err
				return
			}
			accounts <- u
		})
	}
	group.Wait()
	close(accounts)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var admin, member domain.Account
	administrators := 0
	for u := range accounts {
		if u.IsAdmin {
			admin = u
			administrators++
		} else {
			member = u
		}
	}
	t.Log("concurrent registration complete")
	if administrators != 1 {
		t.Fatal("administrator count", administrators)
	}
	if err = mysql.SetRegistration(ctx, db, admin.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err = mysql.RegisterAccount(ctx, db, "closed@test.example", string(hash)); err != mysql.ErrRegistrationClosed {
		t.Fatal(err)
	}
	created, err := mysql.CreateAccount(ctx, db, admin.ID, "created@test.example", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := mysql.GetAccount(ctx, db, created)
	if err != nil || !fresh.MustChangePassword {
		t.Fatal(fresh, err)
	}
	activity := account.NewActivity()
	h := &Handler{Config: &config.Config{Server: config.ServerConfig{BasePath: "/v1", Mode: "test"}, Auth: config.AuthConfig{JWTSecret: "independent-integration-key", RateLimit: 10000, TokenExpiry: time.Hour}}, DB: db, Activity: activity, Instances: &notRunningManager{}}
	t.Log("create router")
	router := NewRouter(h)
	adminJWT, _ := h.generateJWT(admin.ID, admin.Email, 0)
	memberJWT, _ := h.generateJWT(member.ID, member.Email, 0)
	temporaryJWT, _ := h.generateJWT(created, fresh.Email, 0)
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/v1"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		return w
	}
	if w := request("GET", "/admin/users", memberJWT, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/repositories", temporaryJWT, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/auth/change-password", temporaryJWT, `{"old_password":"temporary123","new_password":"permanent123"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	tokenResponse := request("POST", "/api-tokens", adminJWT, `{"label":"admin API","kb_scope":"all","can_write":true}`)
	if tokenResponse.Code != 201 {
		t.Fatal(tokenResponse.Code, tokenResponse.Body.String())
	}
	var token struct {
		Token string `json:"token"`
	}
	if err = json.Unmarshal(tokenResponse.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/admin/users", token.Token, ""); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	memberToken := request("POST", "/api-tokens", memberJWT, `{"label":"member API","kb_scope":"all","can_write":true}`)
	if memberToken.Code != 201 {
		t.Fatal(memberToken.Code, memberToken.Body.String())
	}
	if err = json.Unmarshal(memberToken.Body.Bytes(), &token); err != nil {
		t.Fatal(err)
	}
	t.Log("account status operation")
	if w := request("PUT", "/admin/users/"+member.ID+"/status", adminJWT, `{"status":"disabled"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, old := range []string{memberJWT, token.Token} {
		if w := request("GET", "/repositories", old, ""); w.Code != 401 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	t.Log("account status operation")
	if w := request("PUT", "/admin/users/"+member.ID+"/status", adminJWT, `{"status":"active"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/repositories", memberJWT, ""); w.Code != 401 {
		t.Fatal("old JWT revived", w.Code)
	}
	if w := request("GET", "/repositories", token.Token, ""); w.Code != 401 {
		t.Fatal("old token revived", w.Code)
	}
	// 构造真实 SQL 关联图，正文对象走受控存储接缝，验证不改原始路径。
	agentID, instanceID, sessionID, taskID, runID, exportID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, q := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agents(id,owner_user_id,name,system_prompt,model,skills,mcp_servers,subagents)VALUES(?,?,'private','prompt','test','[]',JSON_ARRAY(JSON_OBJECT('name','private','transport','stdio','env',JSON_OBJECT('API_KEY','synthetic-test-credential'))),'[]')`, []any{agentID, member.ID}},
		{`INSERT INTO agent_instances(id,agent_id,owner_user_id,status,repo_ids)VALUES(?,?,?,'stopped','[]')`, []any{instanceID, agentID, member.ID}},
		{`INSERT INTO sessions(id,user_id,instance_id,agent_id,deleted_at)VALUES(?,?,?,?,NOW())`, []any{sessionID, member.ID, instanceID, agentID}},
		{`INSERT INTO messages(id,session_id,role,content)VALUES(?,?,'user','preserve')`, []any{uuid.NewString(), sessionID}},
		{`INSERT INTO crawl_tasks(id,user_id,name,agent_id,profile_id,profile_ids,urls,enabled)VALUES(?,?,'task',?,'restricted',JSON_ARRAY('restricted'),JSON_ARRAY('https://example.com'),TRUE)`, []any{taskID, member.ID, agentID}},
		{`INSERT INTO crawl_runs(id,user_id,task_id,snapshot,trigger_type,status,stage,total,error)VALUES(?,?,?,'{}','manual','succeeded','published',1,'')`, []any{runID, member.ID, taskID}},
		{`INSERT INTO confluence_tasks(id,user_id,url,status,stage,completed,total,error)VALUES(?,?,'https://example.com/1','completed','export',1,1,'')`, []any{exportID, member.ID}},
	} {
		if _, err = db.Exec(q.query, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	objects := &libraryAPIObjects{data: map[string][]byte{}}
	prefix := "confluence/" + member.ID + "/" + exportID + "/"
	tree := `{"snapshot":{"source":{"base_url":"https://example.com","space_key":"TEAM"}},"nodes":[{"id":"1","title":"One","url":"https://example.com/1","version":1,"last_modified_at":"2026-11-04T00:00:00Z","children":[]}],"pages":[{"id":"1","title":"One","url":"https://example.com/1","version":1,"path":"Root/One"}]}`
	objects.data[prefix+"tree.json"] = []byte(tree)
	objects.data[prefix+"1/page.md"] = []byte("retained source")
	library := &worker.ConfluenceLibrary{DB: db, Objects: objects}
	service := &worker.AccountService{DB: db, Activity: activity, Instances: h.Instances, Library: library}
	t.Log("resource graph created")
	preview, _, err := service.Preview(ctx, admin.ID, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Counts.Messages != 1 || preview.Library.Added != 1 {
		t.Fatal(preview)
	}
	t.Log("deletion operation")
	if err = service.Delete(ctx, admin.ID, member.ID, preview.Digest); err != nil {
		t.Fatal(err)
	}
	t.Log("deletion operation")
	if err = service.Delete(ctx, admin.ID, member.ID, preview.Digest); err != nil {
		t.Fatal("idempotent retry", err)
	}
	if _, err = mysql.GetAccount(ctx, db, member.ID); err != sql.ErrNoRows {
		t.Fatal("account not deleted", err)
	}
	catalog, err := library.Catalog(ctx, admin.ID)
	if err != nil || len(catalog.Spaces) != 1 {
		t.Fatal(catalog, err)
	}
	data, err := library.File(ctx, admin.ID, catalog.Spaces[0].ID, "1", "page.md")
	if err != nil || string(data) != "retained source" {
		t.Fatal(string(data), err)
	}
	transferred, err := mysql.GetConfluenceTask(ctx, db, exportID, admin.ID)
	if err != nil || worker.ConfluencePrefix(transferred) != prefix {
		t.Fatal(transferred, err)
	}
	var privateConnectors string
	if err = db.QueryRow(`SELECT mcp_servers FROM agents WHERE id=?`, agentID).Scan(&privateConnectors); err != nil || privateConnectors != "[]" {
		t.Fatal("external credentials transferred", err)
	}
	var owner string
	var deleted sql.NullTime
	if err = db.QueryRow(`SELECT user_id,deleted_at FROM sessions WHERE id=?`, sessionID).Scan(&owner, &deleted); err != nil || owner != admin.ID || !deleted.Valid {
		t.Fatal(owner, deleted, err)
	}
	var enabled bool
	var profile string
	if err = db.QueryRow(`SELECT enabled,profile_id FROM crawl_tasks WHERE id=?`, taskID).Scan(&enabled, &profile); err != nil || enabled || profile != "" {
		t.Fatal(enabled, profile, err)
	}
	if _, err = mysql.CreateAccount(ctx, db, admin.ID, member.Email, string(hash)); err != nil {
		t.Fatal("deleted email not reusable", err)
	}
	// 模拟冻结后进程退出：只靠持久记录与新活动登记器恢复。
	recovering, err := mysql.CreateAccount(ctx, db, admin.ID, "recover@test.example", string(hash))
	if err != nil {
		t.Fatal(err)
	}
	recoveryPreview, _, err := service.Preview(ctx, admin.ID, recovering)
	if err != nil {
		t.Fatal(err)
	}
	if err = mysql.BeginAccountDeletion(ctx, db, recovering, admin.ID, recoveryPreview.Digest); err != nil {
		t.Fatal(err)
	}
	restarted := &worker.AccountService{DB: db, Activity: account.NewActivity(), Instances: h.Instances, Library: library}
	if err = restarted.Recover(ctx); err != nil {
		t.Fatal("restart recovery", err)
	}
	if _, err = mysql.GetAccount(ctx, db, recovering); err != sql.ErrNoRows {
		t.Fatal("recovery left account", err)
	}
	// 已有部署迁移的管理员选择通过独立旧库测试覆盖；本测试不触碰现有业务库。
}
