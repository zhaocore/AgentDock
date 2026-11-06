package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// AccountInstances 复用 Agent 生命周期接缝，不修改历史消息。
type AccountInstances interface {
	IsRunning(string) bool
	Stop(context.Context, string) error
}

// AccountService 编排冻结、停止确认与原子归属转移。
type AccountService struct {
	DB        *sql.DB
	Activity  *account.Activity
	Instances AccountInstances
	Library   *ConfluenceLibrary
}

// AccountPreview 包含资源数量、冲突和确认摘要。
type AccountPreview struct {
	Counts  domain.ResourceCounts         `json:"counts"`
	Library domain.ConfluenceImportReport `json:"library"`
	Digest  string                        `json:"digest"`
}

// Preview 删除预览复用现有文件库合并规则。
func (s *AccountService) Preview(ctx context.Context, admin, user string) (AccountPreview, []domain.ConfluenceLibrarySpace, error) {
	if admin == user {
		return AccountPreview{}, nil, mysql.ErrProtectedAdministrator
	}
	counts, err := mysql.AccountResources(ctx, s.DB, user)
	if err != nil {
		return AccountPreview{}, nil, err
	}
	report := domain.ConfluenceImportReport{Items: []domain.ConfluenceImportItem{}}
	spaces := []domain.ConfluenceLibrarySpace{}
	digest := ""
	if counts.ConfluenceTasks > 0 || counts.Imports > 0 {
		if s.Library == nil {
			return AccountPreview{}, nil, fmt.Errorf("RustFS must be configured to preview and transfer account library")
		}
		report, spaces, digest, err = s.Library.AccountMergePreview(ctx, admin, user)
		if err != nil {
			return AccountPreview{}, nil, err
		}
	}
	data, err := json.Marshal(struct {
		Counts  domain.ResourceCounts
		Library string
	}{counts, digest})
	if err != nil {
		return AccountPreview{}, nil, err
	}
	return AccountPreview{counts, report, domain.ConfluenceFileHash(data)}, spaces, nil
}

// StopUser 数据库冻结后取消全部在途操作，超时绝不转移归属。
func (s *AccountService) StopUser(ctx context.Context, user string) error {
	if err := s.Activity.Stop(ctx, user); err != nil {
		return fmt.Errorf("wait for account work to stop: %w", err)
	}
	ids, err := mysql.AccountInstanceIDs(ctx, s.DB, user)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if s.Instances.IsRunning(id) {
			if err = s.Instances.Stop(ctx, id); err != nil {
				return err
			}
		}
	}
	return mysql.StopAccountRecords(ctx, s.DB, user)
}

// Delete 冻结后允许失败重试；完成后相同请求返回既有成功状态。
func (s *AccountService) Delete(ctx context.Context, admin, user, digest string) (resultErr error) {
	lock, err := mysql.LockConfluenceLibrary(ctx, s.DB, "deletion:"+user)
	if err != nil {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, mysql.UnlockConfluenceLibrary(release, lock, "deletion:"+user))
	}()

	job, _, err := mysql.GetAccountDeletion(ctx, s.DB, user)
	if err == nil && job.Stage == "completed" {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	preview, _, err := s.Preview(ctx, admin, user)
	if err != nil {
		return err
	}
	if preview.Digest != digest {
		return fmt.Errorf("deletion preview expired; review resources again")
	}
	if err = mysql.BeginAccountDeletion(ctx, s.DB, user, admin, digest); err != nil {
		return err
	}
	defer func() {
		if resultErr != nil {
			record, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			resultErr = errors.Join(resultErr, mysql.DeletionError(record, s.DB, user, resultErr.Error()))
		}
	}()
	return s.resume(ctx, admin, user, digest)
}

// Resume 重新确认停止与预览基线，崩溃发生在事务前后均可安全重试。
func (s *AccountService) Resume(ctx context.Context, admin, user, digest string) (resultErr error) {
	lock, err := mysql.LockConfluenceLibrary(ctx, s.DB, "deletion:"+user)
	if err != nil {
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, mysql.UnlockConfluenceLibrary(release, lock, "deletion:"+user))
	}()
	job, _, err := mysql.GetAccountDeletion(ctx, s.DB, user)
	if err != nil {
		return err
	}
	if job.Stage == "completed" {
		return nil
	}
	if job.Stage != "frozen" && job.Stage != "stopping" {
		return fmt.Errorf("deletion requires a new administrator preview")
	}
	return s.resume(ctx, admin, user, digest)
}

func (s *AccountService) resume(ctx context.Context, admin, user, digest string) (resultErr error) {
	if err := s.StopUser(ctx, user); err != nil {
		return err
	}
	// 锁定管理员目录，避免并发导入或其他账户转移改变合并基线。
	lock, err := mysql.LockConfluenceLibrary(ctx, s.DB, admin)
	if err != nil {
		return err
	}
	defer func() {
		unlock, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		resultErr = errors.Join(resultErr, mysql.UnlockConfluenceLibrary(unlock, lock, admin))
	}()
	preview, spaces, err := s.Preview(ctx, admin, user)
	if err != nil {
		return err
	}
	if preview.Digest != digest {
		return fmt.Errorf("account resources changed; account remains frozen, review a new deletion preview")
	}
	return mysql.TransferAccountResources(ctx, s.DB, user, admin, spaces, preview.Library)
}

// Recover 只恢复已确认且尚未失败的删除，失败等待管理员重新检查。
func (s *AccountService) Recover(ctx context.Context) error {
	jobs, err := mysql.PendingAccountDeletions(ctx, s.DB)
	if err != nil {
		return err
	}
	var failures []error
	for _, j := range jobs {
		_, digest, err := mysql.GetAccountDeletion(ctx, s.DB, j.UserID)
		if err == nil {
			err = s.Resume(ctx, j.AdminUserID, j.UserID, digest)
		}
		if err != nil {
			failures = append(failures, errors.Join(err, mysql.DeletionError(ctx, s.DB, j.UserID, err.Error())))
		}
	}
	return errors.Join(failures...)
}

// StopCrawlTask 先持久化停止再确认执行者退出，防止任务删除与最后一次写入竞态。
func (s *AccountService) StopCrawlTask(ctx context.Context, user, id string) error {
	ids, err := mysql.StopManagedCrawl(ctx, s.DB, user, id)
	if err != nil {
		return err
	}
	for _, run := range ids {
		if err = s.Activity.Stop(ctx, account.WorkKey(user, run)); err != nil {
			return err
		}
	}
	return nil
}

// DeleteCrawlTask 停止确认失败时不得删除元数据。
func (s *AccountService) DeleteCrawlTask(ctx context.Context, user, id string) error {
	if err := s.StopCrawlTask(ctx, user, id); err != nil {
		return err
	}
	return mysql.DeleteManagedCrawl(ctx, s.DB, user, id)
}

// ChangeStatus 启用前先确认此前冻结工作全部退出，不复活旧排队任务或凭据。
func (s *AccountService) ChangeStatus(ctx context.Context, actor, user, status string) error {
	if status == "active" {
		u, err := mysql.GetAccount(ctx, s.DB, user)
		if err != nil {
			return err
		}
		if u.IsAdmin {
			return mysql.ErrProtectedAdministrator
		}
		if u.Status == "active" {
			return nil
		}
		if err = s.StopUser(ctx, u.ID); err != nil {
			return err
		}
	}
	if err := mysql.SetAccountStatus(ctx, s.DB, actor, user, status); err != nil {
		return err
	}
	if status == "disabled" {
		return s.StopUser(ctx, user)
	}
	return nil
}

// DeleteConfluenceTask 只删除已停止且执行者已退出的导出元数据。
func (s *AccountService) DeleteConfluenceTask(ctx context.Context, user, id string) error {
	task, err := mysql.GetConfluenceTask(ctx, s.DB, id, user)
	if err != nil {
		return err
	}
	if task.Status == "running" || task.Status == "queued" || task.Status == "stopping" || task.Status == "selecting" {
		return fmt.Errorf("task still active; stop it first")
	}
	if err = s.Activity.Stop(ctx, account.WorkKey(task.UserID, task.ID)); err != nil {
		return err
	}
	return mysql.DeleteManagedConfluence(ctx, s.DB, task.UserID, task.ID)
}
