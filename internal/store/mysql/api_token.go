package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

// CreateAPIToken 在单事务内写入令牌与其范围。
func CreateAPIToken(ctx context.Context, db *sql.DB, token domain.APIToken, tokenHash string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin api token tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO api_tokens (id, tenant_id, label, token_prefix, token_hash, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		token.ID, token.TenantID, token.Label, token.TokenPrefix, tokenHash, token.ExpiresAt); err != nil {
		return fmt.Errorf("insert api token: %w", err)
	}
	for _, scope := range token.Scopes {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO api_token_scopes (token_id, kb_id, can_write) VALUES (?, ?, ?)`,
			token.ID, scope.KBID, scope.CanWrite); err != nil {
			return fmt.Errorf("insert api token scope: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit api token tx: %w", err)
	}
	return nil
}

// normalizeScopeKB 把全零哨兵 UUID 归一化为 "all"，API 契约统一用 "all" 表示全部范围。
func normalizeScopeKB(kbID string) string {
	if kbID == domain.APITokenAllKBs {
		return "all"
	}
	return kbID
}

// GetAPITokenByHash 按哈希取未撤销令牌及其范围，供鉴权中间件使用。
func GetAPITokenByHash(ctx context.Context, db *sql.DB, tokenHash string) (domain.APIToken, error) {
	token := domain.APIToken{Scopes: []domain.APITokenScope{}}
	err := db.QueryRowContext(ctx,
		`SELECT id, tenant_id, label, token_prefix, expires_at, created_at
		FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL`, tokenHash,
	).Scan(&token.ID, &token.TenantID, &token.Label, &token.TokenPrefix, &token.ExpiresAt, &token.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.APIToken{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("get api token by hash: %w", err)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT kb_id, can_write FROM api_token_scopes WHERE token_id = ?`, token.ID)
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("list api token scopes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var scope domain.APITokenScope
		if err := rows.Scan(&scope.KBID, &scope.CanWrite); err != nil {
			return domain.APIToken{}, fmt.Errorf("scan api token scope: %w", err)
		}
		scope.KBID = normalizeScopeKB(scope.KBID)
		token.Scopes = append(token.Scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return domain.APIToken{}, fmt.Errorf("list api token scopes: %w", err)
	}
	return token, nil
}

// ListAPITokens 列出租户全部令牌（含已撤销），新创建在前。
func ListAPITokens(ctx context.Context, db *sql.DB, tenantID string) ([]domain.APIToken, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, tenant_id, label, token_prefix, expires_at, revoked_at, last_used_at, created_at
		FROM api_tokens WHERE tenant_id = ? ORDER BY created_at DESC, id DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer rows.Close()
	tokens := []domain.APIToken{}
	for rows.Next() {
		var token domain.APIToken
		if err := rows.Scan(&token.ID, &token.TenantID, &token.Label, &token.TokenPrefix,
			&token.ExpiresAt, &token.RevokedAt, &token.LastUsedAt, &token.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan api token: %w", err)
		}
		token.Scopes = []domain.APITokenScope{}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	// 二次查询填充范围，令牌数量小，逐个查询即可
	for i := range tokens {
		scopeRows, err := db.QueryContext(ctx,
			`SELECT kb_id, can_write FROM api_token_scopes WHERE token_id = ?`, tokens[i].ID)
		if err != nil {
			return nil, fmt.Errorf("list api token scopes: %w", err)
		}
		for scopeRows.Next() {
			var scope domain.APITokenScope
			if err := scopeRows.Scan(&scope.KBID, &scope.CanWrite); err != nil {
				scopeRows.Close()
				return nil, fmt.Errorf("scan api token scope: %w", err)
			}
			scope.KBID = normalizeScopeKB(scope.KBID)
			tokens[i].Scopes = append(tokens[i].Scopes, scope)
		}
		if err := scopeRows.Err(); err != nil {
			scopeRows.Close()
			return nil, fmt.Errorf("list api token scopes: %w", err)
		}
		scopeRows.Close()
	}
	return tokens, nil
}

// RevokeAPIToken 撤销令牌（打时间戳），目标不存在或已撤销返回 sql.ErrNoRows。
func RevokeAPIToken(ctx context.Context, db *sql.DB, tenantID, tokenID string) error {
	result, err := db.ExecContext(ctx,
		`UPDATE api_tokens SET revoked_at=NOW()
		WHERE id=? AND tenant_id=? AND revoked_at IS NULL`, tokenID, tenantID)
	if err != nil {
		return fmt.Errorf("revoke api token: %w", err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("revoke api token rows: %w", err)
	} else if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// TouchAPIToken 更新令牌最近使用时间，失败由调用方决定是否记录。
func TouchAPIToken(ctx context.Context, db *sql.DB, tokenID string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE api_tokens SET last_used_at=NOW() WHERE id=? AND revoked_at IS NULL`, tokenID)
	if err != nil {
		return fmt.Errorf("touch api token: %w", err)
	}
	return nil
}
