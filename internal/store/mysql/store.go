// Package mysql 提供 MySQL 数据库连接、迁移执行与数据访问。
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Store 是 MySQL 数据访问的门面。
type Store struct {
	DB *sql.DB
}

// New 创建 MySQL 连接并执行健康检查。
func New(ctx context.Context, dsn string, maxOpen, maxIdle int, connMaxLifetime time.Duration) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &Store{DB: db}, nil
}

// Close 关闭数据库连接。
func (s *Store) Close() error {
	return s.DB.Close()
}

// RunMigrations 按序执行尚未应用的 SQL 文件。
// 每个迁移在独立事务内执行并用 MySQL 命名锁防并发。
func (s *Store) RunMigrations(migrationsDir string) error {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir %s: %w", migrationsDir, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil
	}

	// MySQL 命名锁串行化迁移执行，锁在连接归还时自动释放
	lockConn, err := s.DB.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("acquire migrations connection: %w", err)
	}
	defer lockConn.Close()
	if _, err := lockConn.ExecContext(context.Background(), `SELECT GET_LOCK('dutybotgo_migrations', 30)`); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer lockConn.ExecContext(context.Background(), `SELECT RELEASE_LOCK('dutybotgo_migrations')`)

	if _, err := lockConn.ExecContext(context.Background(), `CREATE TABLE IF NOT EXISTS schema_migrations (
		name VARCHAR(255) PRIMARY KEY, applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}
	for _, f := range files {
		var applied bool
		if err := lockConn.QueryRowContext(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = ?)`, f).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", f, err)
		}
		if applied {
			continue
		}
		path := filepath.Join(migrationsDir, f)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}
		tx, err := lockConn.BeginTx(context.Background(), nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", f, err)
		}
		if _, err := tx.Exec(string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec migration %s: %w", f, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name) VALUES (?)`, f); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", f, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", f, err)
		}
		fmt.Printf("migration applied: %s\n", f)
	}
	return nil
}
