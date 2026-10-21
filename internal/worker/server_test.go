package worker

import (
	"path/filepath"
	"testing"
)

// 删除任务只允许清理 repo 根目录内的路径，防止 payload 篡改误删系统目录。
func TestIsSubPath(t *testing.T) {
	cases := []struct {
		root, target string
		want         bool
	}{
		{"/srv/repo", "/srv/repo/web", true},
		{"/srv/repo", "/srv/repo", false}, // 根目录本身拒绝删除
		{"/srv/repo", "/srv/other", false},
		{"/srv/repo", "/etc", false},
		{"/srv/repo", "/srv/reposibling", false},
		{"/srv/repo", "../../etc", false},
	}
	for _, tc := range cases {
		if got := isSubPath(tc.root, tc.target); got != tc.want {
			t.Errorf("isSubPath(%q, %q) = %v, want %v", tc.root, tc.target, got, tc.want)
		}
	}
}

// 克隆目标目录必须以仓库名隔离在根目录下。
func TestCloneDestLayout(t *testing.T) {
	dest := filepath.Join("/srv/repo", "web")
	if dest != "/srv/repo/web" {
		t.Fatalf("unexpected dest layout: %s", dest)
	}
}
