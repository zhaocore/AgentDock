package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hibiken/asynq"

	"github.com/zhaocore/dutybotgo/internal/domain"
)

func testTime() time.Time { return time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) }

func newTestServer(t *testing.T) (*Server, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	return &Server{db: db, rootDir: root}, mock, root
}

func repoRows(status string, localPath any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
		AddRow("repo-1", "web", "file://nonexistent-remote", "frontend", "main", localPath, status, nil, testTime(), testTime())
}

// 克隆失败：git clone 报错后仓库必须置 failed 并记录原因，任务不重试。
func TestHandleRepoClone_Failure(t *testing.T) {
	s, mock, _ := newTestServer(t)
	mock.ExpectQuery("SELECT").WillReturnRows(repoRows("pending", nil))
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusCloning, nil, nil, "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusFailed, nil, sqlmock.AnyArg(), "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	payload := `{"repo_id":"repo-1"}`
	if err := s.handleRepoClone(context.Background(), asynq.NewTask(TypeRepoClone, []byte(payload))); err != nil {
		t.Fatalf("clone failure should be swallowed (terminal state), got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 同步无本地路径的仓库：直接 failed。
func TestHandleRepoSync_NoLocalPath(t *testing.T) {
	s, mock, _ := newTestServer(t)
	mock.ExpectQuery("SELECT").WillReturnRows(repoRows("ready", nil))
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusFailed, nil, sqlmock.AnyArg(), "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.handleRepoSync(context.Background(), asynq.NewTask(TypeRepoSync, []byte(`{"repo_id":"repo-1"}`))); err != nil {
		t.Fatalf("sync without local path should fail terminal, got %v", err)
	}
}

// 删除任务：repo 根内的目录被清理。
func TestHandleRepoDelete_RemovesDir(t *testing.T) {
	s, _, root := newTestServer(t)
	target := filepath.Join(root, "web")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	payload := `{"repo_id":"repo-1","local_path":"` + target + `"}`
	if err := s.handleRepoDelete(context.Background(), asynq.NewTask(TypeRepoDelete, []byte(payload))); err != nil {
		t.Fatalf("handleRepoDelete: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("repo dir should be removed")
	}
}

// 删除任务：根外路径拒绝清理（防误删）。
func TestHandleRepoDelete_RefusesOutsideRoot(t *testing.T) {
	s, _, _ := newTestServer(t)
	payload := `{"repo_id":"repo-1","local_path":"/etc"}`
	if err := s.handleRepoDelete(context.Background(), asynq.NewTask(TypeRepoDelete, []byte(payload))); err == nil {
		t.Fatal("deleting outside repo root must be refused")
	}
}

// 克隆成功路径：用本地裸仓库做真实 git clone。
func TestHandleRepoClone_Success(t *testing.T) {
	s, mock, root := newTestServer(t)

	// 构造本地裸仓库作为远端
	ctx := context.Background()
	// 构造带一次提交的本地仓库作为远端（空裸仓库无法按分支克隆）
	work := filepath.Join(root, "src")
	remote := filepath.Join(root, "remote.git")
	if _, err := runCmd(ctx, "", "git", "init", "-b", "master", work); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", work, "add", "."},
		{"-C", work, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init"},
		{"-C", work, "clone", "--bare", ".", remote},
	} {
		if _, err := runCmd(ctx, "", "git", args...); err != nil {
			t.Fatalf("prepare remote: %v", err)
		}
	}

	dest := filepath.Join(root, "web")
	mock.ExpectQuery("SELECT").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "git_url", "repo_type", "branch", "local_path", "status", "last_error", "created_at", "updated_at"}).
			AddRow("repo-1", "web", remote, "frontend", "master", nil, "pending", nil, testTime(), testTime()))
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusCloning, nil, nil, "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE repositories SET status").
		WithArgs(domain.RepoStatusReady, dest, nil, "repo-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := s.handleRepoClone(ctx, asynq.NewTask(TypeRepoClone, []byte(`{"repo_id":"repo-1"}`))); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Errorf("cloned repo missing HEAD: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
