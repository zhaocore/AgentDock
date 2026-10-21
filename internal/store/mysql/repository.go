package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// repoColumns 是 repositories 的查询列，local_path 可能为 NULL（未 clone）。
const repoColumns = `id, name, git_url, repo_type, branch, local_path, status, last_error, created_at, updated_at`

// scanRepo 把一行仓库数据扫描到领域对象。
func scanRepo(scn interface{ Scan(...any) error }) (domain.Repository, error) {
	var repo domain.Repository
	var localPath, lastError sql.NullString
	if err := scn.Scan(&repo.ID, &repo.Name, &repo.GitURL, &repo.RepoType, &repo.Branch,
		&localPath, &repo.Status, &lastError, &repo.CreatedAt, &repo.UpdatedAt); err != nil {
		return domain.Repository{}, fmt.Errorf("scan repository: %w", err)
	}
	repo.LocalPath = localPath.String
	repo.LastError = lastError.String
	return repo, nil
}

// CreateRepository 插入仓库记录，同名冲突返回 sql.ErrNoRows。
func CreateRepository(ctx context.Context, db *sql.DB, repo domain.Repository) error {
	result, err := db.ExecContext(ctx,
		`INSERT INTO repositories (id, name, git_url, repo_type, branch, status)
		VALUES (?, ?, ?, ?, ?, ?)`,
		repo.ID, repo.Name, repo.GitURL, repo.RepoType, repo.Branch, domain.RepoStatusPending)
	if err != nil {
		return fmt.Errorf("insert repository: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("insert repository rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetRepository 按 ID 取仓库，不存在返回 sql.ErrNoRows。
func GetRepository(ctx context.Context, db *sql.DB, id string) (domain.Repository, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repositories WHERE id = ?`, id)
	repo, err := scanRepo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, sql.ErrNoRows
	}
	return repo, err
}

// GetRepositoryByName 按名称取仓库，用于同名冲突判断。
func GetRepositoryByName(ctx context.Context, db *sql.DB, name string) (domain.Repository, error) {
	row := db.QueryRowContext(ctx,
		`SELECT `+repoColumns+` FROM repositories WHERE name = ?`, name)
	repo, err := scanRepo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Repository{}, sql.ErrNoRows
	}
	return repo, err
}

// ListRepositories 分页列出全部仓库（全局共享），按创建时间倒序。
func ListRepositories(ctx context.Context, db *sql.DB, limit, offset int) ([]domain.Repository, int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT `+repoColumns+` FROM repositories ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()
	list := []domain.Repository{}
	for rows.Next() {
		repo, err := scanRepo(rows)
		if err != nil {
			return nil, 0, err
		}
		list = append(list, repo)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list repositories: %w", err)
	}
	var total int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count repositories: %w", err)
	}
	return list, total, nil
}

// UpdateRepositoryStatus 回写状态与本地路径 / 最近错误。
func UpdateRepositoryStatus(ctx context.Context, db *sql.DB, id string, status domain.RepoStatus, localPath, lastError string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE repositories SET status=?, local_path=?, last_error=?, updated_at=NOW() WHERE id=?`,
		status, nullable(localPath), nullable(lastError), id)
	if err != nil {
		return fmt.Errorf("update repository status: %w", err)
	}
	return nil
}

// DeleteRepository 删除仓库记录，不存在返回 sql.ErrNoRows。
func DeleteRepository(ctx context.Context, db *sql.DB, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM repositories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete repository: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("delete repository rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// nullable 空串写 NULL，保持列语义。
func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
