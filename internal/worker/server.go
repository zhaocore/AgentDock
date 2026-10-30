// Package worker 提供 Asynq 任务：仓库克隆 / 同步 / 删除。
package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hibiken/asynq"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

// 任务类型。
const (
	TypeRepoClone  = "repo:clone"
	TypeRepoSync   = "repo:sync"
	TypeRepoDelete = "repo:delete"
)

// Enqueuer 供 API 层入队的最小接口。
type Enqueuer interface {
	EnqueueContext(ctx context.Context, task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
	Close() error
}

// NewClient 创建 Asynq 入队客户端。
func NewClient(addr, password string) *asynq.Client {
	return asynq.NewClient(asynq.RedisClientOpt{Addr: addr, Password: password})
}

// repoPayload 仓库任务参数。
type repoPayload struct {
	RepoID    string `json:"repo_id"`
	LocalPath string `json:"local_path,omitempty"`
}

// EnqueueRepoClone 入队克隆任务。
func EnqueueRepoClone(ctx context.Context, client Enqueuer, repoID string) error {
	return enqueueRepoTask(ctx, client, TypeRepoClone, repoID, "")
}

// EnqueueRepoSync 入队同步任务。
func EnqueueRepoSync(ctx context.Context, client Enqueuer, repoID string) error {
	return enqueueRepoTask(ctx, client, TypeRepoSync, repoID, "")
}

// EnqueueRepoDelete 入队删除任务（携带待清理的本地目录）。
func EnqueueRepoDelete(ctx context.Context, client Enqueuer, repoID, localPath string) error {
	return enqueueRepoTask(ctx, client, TypeRepoDelete, repoID, localPath)
}

// enqueueRepoTask 统一入队。
func enqueueRepoTask(ctx context.Context, client Enqueuer, taskType, repoID, localPath string) error {
	payload, err := json.Marshal(repoPayload{RepoID: repoID, LocalPath: localPath})
	if err != nil {
		return fmt.Errorf("encode %s payload: %w", taskType, err)
	}
	if _, err := client.EnqueueContext(ctx, asynq.NewTask(taskType, payload), asynq.MaxRetry(2)); err != nil {
		return fmt.Errorf("enqueue %s: %w", taskType, err)
	}
	return nil
}

// Server Asynq worker 服务。
type Server struct {
	Crawl      *CrawlWorker
	srv        *asynq.Server
	db         *sql.DB
	rootDir    string
	Confluence *ConfluenceWorker
}

// NewServer 创建 worker 服务。
func NewServer(addr, password string, db *sql.DB, rootDir string, concurrency int) *Server {
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: addr, Password: password},
		asynq.Config{Concurrency: concurrency, Queues: map[string]int{"default": 10}},
	)
	return &Server{srv: srv, db: db, rootDir: rootDir}
}

// Start 注册处理器并启动 worker（阻塞）。
func (s *Server) Start() error {
	mux := asynq.NewServeMux()
	if s.Crawl != nil {
		mux.HandleFunc(TypeCrawlRun, s.Crawl.Handle)
	}
	if s.Confluence != nil {
		mux.HandleFunc(TypeConfluenceExport, s.Confluence.Handle)
	}
	mux.HandleFunc(TypeRepoClone, s.handleRepoClone)
	mux.HandleFunc(TypeRepoSync, s.handleRepoSync)
	mux.HandleFunc(TypeRepoDelete, s.handleRepoDelete)
	return s.srv.Run(mux)
}

// Shutdown 停止 worker。
func (s *Server) Shutdown() {
	s.srv.Shutdown()
}

// handleRepoClone 克隆仓库并触发 codegraph 初始化。
func (s *Server) handleRepoClone(ctx context.Context, t *asynq.Task) error {
	var p repoPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode repo clone payload: %w", err)
	}
	repo, err := mysql.GetRepository(ctx, s.db, p.RepoID)
	if err != nil {
		return fmt.Errorf("get repository %s: %w", p.RepoID, err)
	}
	dest := filepath.Join(s.rootDir, repo.Name)

	if err := mysql.UpdateRepositoryStatus(ctx, s.db, repo.ID, domain.RepoStatusCloning, "", ""); err != nil {
		return fmt.Errorf("mark cloning: %w", err)
	}

	// 目标目录已存在时先清理，保证 clone 语义
	if err := os.RemoveAll(dest); err != nil {
		return s.failRepository(ctx, repo.ID, fmt.Sprintf("clean dest dir: %v", err))
	}
	if err := os.MkdirAll(s.rootDir, 0o755); err != nil {
		return s.failRepository(ctx, repo.ID, fmt.Sprintf("create repo root: %v", err))
	}
	if out, err := runGit(ctx, "clone", "--branch", repo.Branch, repo.GitURL, dest); err != nil {
		return s.failRepository(ctx, repo.ID, fmt.Sprintf("git clone: %v: %s", err, out))
	}

	// codegraph 索引不存在才触发初始化
	if _, err := os.Stat(filepath.Join(dest, ".codegraph")); os.IsNotExist(err) {
		if out, err := runCmd(ctx, dest, "codegraph", "init", "-i"); err != nil {
			// 初始化失败不阻断仓库就绪，只记录告警
			slog.Warn("codegraph init failed", "repo_id", repo.ID, "error", err, "output", out)
		}
	}

	if err := mysql.UpdateRepositoryStatus(ctx, s.db, repo.ID, domain.RepoStatusReady, dest, ""); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	slog.Info("repository cloned", "repo_id", repo.ID, "path", dest)
	return nil
}

// handleRepoSync 拉取远端并硬重置到分支。
func (s *Server) handleRepoSync(ctx context.Context, t *asynq.Task) error {
	var p repoPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode repo sync payload: %w", err)
	}
	repo, err := mysql.GetRepository(ctx, s.db, p.RepoID)
	if err != nil {
		return fmt.Errorf("get repository %s: %w", p.RepoID, err)
	}
	if repo.LocalPath == "" {
		return s.failRepository(ctx, repo.ID, "repo has no local path; clone first")
	}
	if err := mysql.UpdateRepositoryStatus(ctx, s.db, repo.ID, domain.RepoStatusCloning, "", ""); err != nil {
		return fmt.Errorf("mark cloning: %w", err)
	}
	if out, err := runGit(ctx, "-C", repo.LocalPath, "fetch", "origin", repo.Branch); err != nil {
		return s.failRepository(ctx, repo.ID, fmt.Sprintf("git fetch: %v: %s", err, out))
	}
	if out, err := runGit(ctx, "-C", repo.LocalPath, "reset", "--hard", "origin/"+repo.Branch); err != nil {
		return s.failRepository(ctx, repo.ID, fmt.Sprintf("git reset: %v: %s", err, out))
	}
	if _, err := os.Stat(filepath.Join(repo.LocalPath, ".codegraph")); os.IsNotExist(err) {
		if out, err := runCmd(ctx, repo.LocalPath, "codegraph", "init", "-i"); err != nil {
			slog.Warn("codegraph init failed", "repo_id", repo.ID, "error", err, "output", out)
		}
	}
	if err := mysql.UpdateRepositoryStatus(ctx, s.db, repo.ID, domain.RepoStatusReady, repo.LocalPath, ""); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	slog.Info("repository synced", "repo_id", repo.ID)
	return nil
}

// handleRepoDelete 删除本地目录（DB 记录由 API 层先行删除）。
func (s *Server) handleRepoDelete(ctx context.Context, t *asynq.Task) error {
	var p repoPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode repo delete payload: %w", err)
	}
	if p.LocalPath == "" {
		return nil
	}
	// 只允许删除 repo 根目录下的路径，防误删
	if !isSubPath(s.rootDir, p.LocalPath) {
		return fmt.Errorf("refuse to delete path outside repo root: %s", p.LocalPath)
	}
	if err := os.RemoveAll(p.LocalPath); err != nil {
		return fmt.Errorf("remove repo dir %s: %w", p.LocalPath, err)
	}
	slog.Info("repository dir removed", "repo_id", p.RepoID, "path", p.LocalPath)
	return nil
}

// failRepository 把仓库置为 failed 并记录错误，返回 nil 终止任务（不重试）。
func (s *Server) failRepository(ctx context.Context, repoID, reason string) error {
	if err := mysql.UpdateRepositoryStatus(ctx, s.db, repoID, domain.RepoStatusFailed, "", reason); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	slog.Warn("repository task failed", "repo_id", repoID, "reason", reason)
	return nil
}

// runGit 执行 git 命令，返回合并的输出。
func runGit(ctx context.Context, args ...string) (string, error) {
	return runCmd(ctx, "", "git", args...)
}

// runCmd 在指定目录执行命令，返回合并的输出。
func runCmd(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w", name, args, err)
	}
	return string(out), nil
}

// isSubPath 判断 target 是否严格位于 root 之下（target == root 也拒绝删除）。
func isSubPath(root, target string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absTarget)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
