// Package config 提供应用配置管理，使用 viper 读取环境变量和 .env 文件。
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 是应用全部配置的聚合结构体。
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Repo     RepoConfig
	Agent    AgentConfig
	LLM      LLMConfig
	Auth     AuthConfig
}

// ServerConfig HTTP 服务配置。
type ServerConfig struct {
	Host     string
	Port     string
	Mode     string // debug | release
	BasePath string // API 基础路径
}

// DatabaseConfig MySQL 连接配置。
type DatabaseConfig struct {
	Host            string
	Port            string
	User            string
	Password        string
	DBName          string
	URL             string // 构造好的 DSN
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// RedisConfig Redis 连接配置，Asynq 复用。
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	Addr     string // 构造好的地址: host:port
}

// RepoConfig 仓库克隆目录配置。
type RepoConfig struct {
	RootDir string // 仓库克隆根目录
}

// AgentConfig 公共 Agent 部署目录配置。
type AgentConfig struct {
	RootDir string
}

// LLMConfig LLM 服务配置（OpenAI 兼容端点，供 langchaingo 使用）。
type LLMConfig struct {
	Provider string // openai
	APIKey   string
	BaseURL  string
	Model    string
}

// AuthConfig 认证与限流配置。
type AuthConfig struct {
	JWTSecret   string
	RateLimit   int // 每用户每分钟请求数
	TokenExpiry time.Duration
}

// Load 从环境变量和 .env 文件加载配置。
func Load() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.SetConfigType("dotenv")
	viper.AutomaticEnv()

	// 文件不存在时不报错，允许全部走环境变量
	_ = viper.ReadInConfig()

	viper.SetDefault("SERVER_HOST", "0.0.0.0")
	viper.SetDefault("SERVER_PORT", "8080")
	viper.SetDefault("SERVER_MODE", "debug")
	viper.SetDefault("SERVER_BASE_PATH", "/v1")

	viper.SetDefault("MYSQL_HOST", "localhost")
	viper.SetDefault("MYSQL_PORT", "3306")
	viper.SetDefault("MYSQL_USER", "dutybot")
	viper.SetDefault("MYSQL_PASSWORD", "dutybot")
	viper.SetDefault("MYSQL_DB", "dutybot")
	viper.SetDefault("DATABASE_MAX_OPEN_CONNS", 25)
	viper.SetDefault("DATABASE_MAX_IDLE_CONNS", 5)
	viper.SetDefault("DATABASE_CONN_MAX_LIFETIME", "5m")

	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")

	viper.SetDefault("REPO_ROOT_DIR", "repo")
	viper.SetDefault("AGENT_CONFIG_DIR", "agents")

	viper.SetDefault("LLM_PROVIDER", "openai")
	viper.SetDefault("LLM_MODEL", "gpt-4o-mini")

	viper.SetDefault("API_RATE_LIMIT", 100)
	viper.SetDefault("AUTH_TOKEN_EXPIRY", "24h")

	// 支持 DATABASE_URL / MYSQL_URL 缩写
	dbURL := viper.GetString("DATABASE_URL")
	if dbURL == "" {
		dbURL = viper.GetString("MYSQL_URL")
	}
	// go-sql-driver 不认 mysql:// scheme 前缀，统一剥掉
	dbURL = strings.TrimPrefix(dbURL, "mysql://")
	if dbURL == "" {
		dbHost := viper.GetString("MYSQL_HOST")
		dbPort := viper.GetString("MYSQL_PORT")
		dbUser := viper.GetString("MYSQL_USER")
		dbPass := viper.GetString("MYSQL_PASSWORD")
		dbName := viper.GetString("MYSQL_DB")
		dbURL = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4&multiStatements=true", dbUser, dbPass, dbHost, dbPort, dbName)
	} else {
		// 带参数的 DSN 补齐驱动必需参数（已有 query 用 & 拼接）
		sep := "?"
		if hasQueryParam(dbURL) {
			sep = "&"
		}
		dbURL = dbURL + sep + "parseTime=true&charset=utf8mb4&multiStatements=true"
	}

	// 支持 REDIS_URL 缩写（redis://user:pass@host:port/db）
	redisAddr := ""
	redisPassword := viper.GetString("REDIS_PASSWORD")
	if redisURL := viper.GetString("REDIS_URL"); redisURL != "" {
		if u, err := url.Parse(redisURL); err == nil {
			redisAddr = u.Host
			if u.User != nil {
				if p, ok := u.User.Password(); ok {
					redisPassword = p
				}
			}
		}
	}
	if redisAddr == "" {
		redisHost := viper.GetString("REDIS_HOST")
		redisPort := viper.GetString("REDIS_PORT")
		redisAddr = fmt.Sprintf("%s:%s", redisHost, redisPort)
	}

	connLifetime, err := time.ParseDuration(viper.GetString("DATABASE_CONN_MAX_LIFETIME"))
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_CONN_MAX_LIFETIME: %w", err)
	}

	tokenExpiry, err := time.ParseDuration(viper.GetString("AUTH_TOKEN_EXPIRY"))
	if err != nil {
		return nil, fmt.Errorf("invalid AUTH_TOKEN_EXPIRY: %w", err)
	}

	cfg := &Config{
		Server: ServerConfig{
			Host:     viper.GetString("SERVER_HOST"),
			Port:     viper.GetString("SERVER_PORT"),
			Mode:     viper.GetString("SERVER_MODE"),
			BasePath: viper.GetString("SERVER_BASE_PATH"),
		},
		Database: DatabaseConfig{
			Host:            viper.GetString("MYSQL_HOST"),
			Port:            viper.GetString("MYSQL_PORT"),
			User:            viper.GetString("MYSQL_USER"),
			Password:        viper.GetString("MYSQL_PASSWORD"),
			DBName:          viper.GetString("MYSQL_DB"),
			URL:             dbURL,
			MaxOpenConns:    viper.GetInt("DATABASE_MAX_OPEN_CONNS"),
			MaxIdleConns:    viper.GetInt("DATABASE_MAX_IDLE_CONNS"),
			ConnMaxLifetime: connLifetime,
		},
		Redis: RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     viper.GetString("REDIS_PORT"),
			Password: redisPassword,
			Addr:     redisAddr,
		},
		Repo: RepoConfig{
			RootDir: viper.GetString("REPO_ROOT_DIR"),
		},
		Agent: AgentConfig{RootDir: viper.GetString("AGENT_CONFIG_DIR")},
		LLM: LLMConfig{
			Provider: viper.GetString("LLM_PROVIDER"),
			APIKey:   viper.GetString("LLM_API_KEY"),
			BaseURL:  viper.GetString("LLM_BASE_URL"),
			Model:    viper.GetString("LLM_MODEL"),
		},
		Auth: AuthConfig{
			JWTSecret:   viper.GetString("JWT_SECRET"),
			RateLimit:   viper.GetInt("API_RATE_LIMIT"),
			TokenExpiry: tokenExpiry,
		},
	}

	return cfg, nil
}

// hasQueryParam 判断 DSN 是否已带 query 参数。
func hasQueryParam(dsn string) bool {
	// 去掉可能的 tcp(host) 段后查找 ?
	return strings.Contains(dsn, "?")
}

// Validate 启动时校验关键配置项，失败则拒绝启动。
func (c *Config) Validate() error {
	if c.Auth.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET must not be empty")
	}
	if c.Auth.JWTSecret == "change-me-in-production" && c.Server.Mode != "debug" {
		return fmt.Errorf("JWT_SECRET is still the default value, refusing to start in %s mode", c.Server.Mode)
	}

	if c.LLM.Provider == "" {
		return fmt.Errorf("LLM_PROVIDER must not be empty")
	}
	if c.LLM.BaseURL == "" {
		return fmt.Errorf("LLM_BASE_URL must not be empty")
	}
	if c.LLM.APIKey == "" {
		return fmt.Errorf("LLM_API_KEY must not be empty")
	}
	if c.LLM.Model == "" {
		return fmt.Errorf("LLM_MODEL must not be empty")
	}
	if c.Repo.RootDir == "" {
		return fmt.Errorf("REPO_ROOT_DIR must not be empty")
	}
	return nil
}
