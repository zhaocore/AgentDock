package config

import (
	"os"
	"testing"
)

// Load 从环境变量读取全量配置并构造 DSN / Redis 地址。
func TestLoadFromEnv(t *testing.T) {
	envs := map[string]string{
		"MYSQL_HOST":     "db.example.com",
		"MYSQL_PORT":     "3307",
		"MYSQL_USER":     "bot",
		"MYSQL_PASSWORD": "secret",
		"MYSQL_DB":       "agentdock",
		"REDIS_HOST":     "redis.example.com",
		"REDIS_PORT":     "6380",
		"REPO_ROOT_DIR":  "/srv/repos",
		"LLM_API_KEY":    "sk-x",
		"LLM_BASE_URL":   "https://api.example.com/v1",
		"JWT_SECRET":     "s",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}
	// 隔离 .env 文件影响
	t.Setenv("DATABASE_URL", "")
	t.Setenv("MYSQL_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	wantDSN := "bot:secret@tcp(db.example.com:3307)/agentdock?parseTime=true&charset=utf8mb4&multiStatements=true"
	if cfg.Database.URL != wantDSN {
		t.Errorf("DSN=%s", cfg.Database.URL)
	}
	if cfg.Redis.Addr != "redis.example.com:6380" {
		t.Errorf("redis addr=%s", cfg.Redis.Addr)
	}
	if cfg.Repo.RootDir != "/srv/repos" {
		t.Errorf("repo root=%s", cfg.Repo.RootDir)
	}
}

// DATABASE_URL 优先于分项配置。
func TestLoadPrefersDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "mysql://u:p@tcp(h)/db")
	t.Setenv("MYSQL_HOST", "ignored")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.URL != "u:p@tcp(h)/db?parseTime=true&charset=utf8mb4&multiStatements=true" {
		t.Errorf("DATABASE_URL should win, scheme stripped, params appended, got %s", cfg.Database.URL)
	}
}

// 非法时长直接报错。
func TestLoadInvalidDuration(t *testing.T) {
	t.Setenv("DATABASE_CONN_MAX_LIFETIME", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("invalid duration must fail")
	}
	_ = os.Getenv("HOME")
}
