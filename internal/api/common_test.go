// Package api 公共函数单元测试。
package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- parsePagination ---

func TestParsePagination_Defaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases", nil)
	limit, offset, err := parsePagination(req)
	if err != nil {
		t.Fatalf("defaults should not error: %v", err)
	}
	if limit != 20 || offset != 0 {
		t.Errorf("expected 20/0, got %d/%d", limit, offset)
	}
}

func TestParsePagination_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?limit=5&offset=10", nil)
	limit, offset, err := parsePagination(req)
	if err != nil {
		t.Fatalf("valid params should not error: %v", err)
	}
	if limit != 5 || offset != 10 {
		t.Errorf("expected 5/10, got %d/%d", limit, offset)
	}
}

func TestParsePagination_LimitBoundary(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?limit=100", nil)
	limit, _, err := parsePagination(req)
	if err != nil || limit != 100 {
		t.Errorf("limit=100 should be valid, got %d, err=%v", limit, err)
	}
}

func TestParsePagination_LimitZero(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?limit=0", nil)
	_, _, err := parsePagination(req)
	if err == nil {
		t.Error("limit=0 should be rejected")
	}
}

func TestParsePagination_LimitOver100(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?limit=101", nil)
	_, _, err := parsePagination(req)
	if err == nil {
		t.Error("limit=101 should be rejected")
	}
}

func TestParsePagination_LimitNotNumber(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?limit=abc", nil)
	_, _, err := parsePagination(req)
	if err == nil {
		t.Error("non-numeric limit should be rejected")
	}
}

func TestParsePagination_OffsetNegative(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?offset=-1", nil)
	_, _, err := parsePagination(req)
	if err == nil {
		t.Error("negative offset should be rejected with 400")
	}
}

func TestParsePagination_OffsetValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/knowledge-bases?offset=40", nil)
	_, offset, err := parsePagination(req)
	if err != nil || offset != 40 {
		t.Errorf("offset=40 should be valid, got %d, err=%v", offset, err)
	}
}

func TestNewPaginatedResponse_Basic(t *testing.T) {
	resp := NewPaginatedResponse([]string{"a", "b"}, 10, 5, 0)
	if resp.Total != 10 {
		t.Errorf("expected Total=10, got %d", resp.Total)
	}
	if resp.Limit != 5 {
		t.Errorf("expected Limit=5, got %d", resp.Limit)
	}
	if !resp.HasMore {
		t.Error("offset 0 + limit 5 < total 10, should have more")
	}
}

func TestNewPaginatedResponse_LastPage(t *testing.T) {
	resp := NewPaginatedResponse([]string{"a"}, 10, 5, 5)
	if resp.HasMore {
		t.Error("offset 5 + limit 5 = 10 is not < 10, should be hasMore=false")
	}
}

func TestNewPaginatedResponse_NoMore(t *testing.T) {
	resp := NewPaginatedResponse([]string{}, 5, 5, 5)
	if resp.HasMore {
		t.Error("offset 5 + limit 5 = 10 > total 5, should be hasMore=false")
	}
}

func TestNewPaginatedResponse_Empty(t *testing.T) {
	resp := NewPaginatedResponse(nil, 0, 20, 0)
	if resp.HasMore {
		t.Error("empty result should not have more")
	}
	if resp.Total != 0 {
		t.Errorf("expected Total=0, got %d", resp.Total)
	}
}

func TestNewPaginatedResponse_Offset(t *testing.T) {
	resp := NewPaginatedResponse([]int{1, 2, 3}, 100, 20, 20)
	if resp.Offset != 20 {
		t.Errorf("expected Offset=20, got %d", resp.Offset)
	}
}

func TestIsUniqueViolation_True(t *testing.T) {
	err := errors.New("duplicate key value violates unique constraint")
	if !isUniqueViolation(err) {
		t.Error("should detect unique violation")
	}
}

func TestIsUniqueViolation_DuplicateKey(t *testing.T) {
	err := errors.New("ERROR: duplicate key value violates")
	if !isUniqueViolation(err) {
		t.Error("duplicate key should be detected as unique violation")
	}
}

func TestIsUniqueViolation_False(t *testing.T) {
	err := errors.New("connection refused")
	if isUniqueViolation(err) {
		t.Error("connection refused is not a unique violation")
	}
}

func TestIsUniqueViolation_Nil(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Error("nil error should not be unique violation")
	}
}