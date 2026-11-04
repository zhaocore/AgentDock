// Package config 配置校验单元测试。
package config

import (
	"testing"
)

// 构造带必需字段的基础有效配置。
func validConfig() *Config {
	return &Config{
		Server:   ServerConfig{Mode: "debug"},
		Database: DatabaseConfig{URL: "agentdock:strong@tcp(localhost:3306)/agentdock"},
		Redis:    RedisConfig{Addr: "localhost:6379"},
		Repo:     RepoConfig{RootDir: "repo"},
		LLM: LLMConfig{
			Provider: "openai",
			BaseURL:  "https://api.openai.com/v1",
			APIKey:   "sk-test-key",
			Model:    "gpt-4o-mini",
		},
		Auth: AuthConfig{JWTSecret: "test-secret-not-default"},
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid config should pass: %v", err)
	}
}

func TestValidate_EmptyJWTSecret(t *testing.T) {
	cfg := validConfig()
	cfg.Auth.JWTSecret = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected empty JWT secret to fail")
	}
}

func TestValidate_DefaultJWTSecretInRelease(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Mode = "release"
	cfg.Auth.JWTSecret = "change-me-in-production"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected default JWT secret to fail in release mode")
	}
}

func TestValidate_EmptyLLMProvider(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.Provider = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing LLM provider to fail")
	}
}

func TestValidate_EmptyLLMBaseURL(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.BaseURL = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing LLM base URL to fail")
	}
}

func TestValidate_EmptyLLMAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.APIKey = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing LLM API key to fail")
	}
}

func TestValidate_EmptyLLMModel(t *testing.T) {
	cfg := validConfig()
	cfg.LLM.Model = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing LLM model to fail")
	}
}

func TestValidate_EmptyRepoRootDir(t *testing.T) {
	cfg := validConfig()
	cfg.Repo.RootDir = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing repo root dir to fail")
	}
}
