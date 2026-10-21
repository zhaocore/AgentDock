// Package mysql 数据存储单元测试。
package mysql

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

func TestStore_Close(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	mock.ExpectClose()

	store := &Store{DB: db}
	if err := store.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestRunMigrations_NoDirectory(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	store := &Store{DB: db}

	err = store.RunMigrations("/nonexistent/directory/migrations")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// 目录为空时不触碰数据库（无文件可执行）。
func TestRunMigrations_NoSQLFiles(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	store := &Store{DB: db}

	dir := t.TempDir()
	if err := store.RunMigrations(dir); err != nil {
		t.Errorf("empty dir should be a no-op, got %v", err)
	}
}

// 迁移文件内容校验：目录里必须是 .sql 扩展名才被采纳。
func TestRunMigrations_IgnoresNonSQLFiles(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	store := &Store{DB: db}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not sql"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.RunMigrations(dir); err != nil {
		t.Errorf("non-sql files should be ignored, got %v", err)
	}
}

// 仓库状态回写：空串必须写 NULL 保持列语义。
func TestNullable(t *testing.T) {
	if v := nullable(""); v != nil {
		t.Errorf("empty string should map to NULL, got %v", v)
	}
	if v := nullable("/repo/web"); v != "/repo/web" {
		t.Errorf("non-empty should pass through, got %v", v)
	}
}

// repoPayload 之外的冒烟：CreateRepository 参数占位符为 MySQL 风格。
func TestCreateRepositoryUsesPlaceholders(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO repositories").
		WithArgs("id-1", "web", "https://example.com/web.git", "frontend", "main", domain.RepoStatusPending).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := domain.Repository{ID: "id-1", Name: "web", GitURL: "https://example.com/web.git",
		RepoType: domain.RepoTypeFrontend, Branch: "main"}
	if err := CreateRepository(context.Background(), db, repo); err != nil {
		t.Fatalf("CreateRepository: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
