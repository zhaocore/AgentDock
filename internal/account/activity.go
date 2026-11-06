// Package account 协调单进程内账户冻结与在途操作退出。
package account

import (
	"context"
	"sync"
)

// Activity 跟踪请求与 Worker，取消后等待所有写入者退出。
type Activity struct {
	mu      sync.Mutex
	next    uint64
	users   map[string]map[uint64]context.CancelFunc
	changed chan struct{}
}

// NewActivity 创建进程共享的活动登记器。
func NewActivity() *Activity {
	return &Activity{users: make(map[string]map[uint64]context.CancelFunc), changed: make(chan struct{})}
}

// Begin 必须先登记再读取账户状态，避免冻结与开始之间遗漏。
func (a *Activity) Begin(ctx context.Context, user string) (context.Context, func()) {
	a.mu.Lock()
	a.next++
	id := a.next
	child, cancel := context.WithCancel(ctx)
	if a.users[user] == nil {
		a.users[user] = make(map[uint64]context.CancelFunc)
	}
	a.users[user][id] = cancel
	a.mu.Unlock()
	return child, func() {
		cancel()
		a.mu.Lock()
		delete(a.users[user], id)
		if len(a.users[user]) == 0 {
			delete(a.users, user)
		}
		close(a.changed)
		a.changed = make(chan struct{})
		a.mu.Unlock()
	}
}

// Stop 在数据库冻结后调用；等待请求及后台工作完成清理。
func (a *Activity) Stop(ctx context.Context, user string) error {
	for {
		a.mu.Lock()
		entries := a.users[user]
		for _, cancel := range entries {
			cancel()
		}
		done := len(entries) == 0
		changed := a.changed
		a.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// WorkKey 用于单任务停止确认，不扩大账户权限。
func WorkKey(user, id string) string { return user + ":" + id }
