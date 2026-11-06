package worker

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/account"
	"github.com/zhaocore/agentdock/internal/store/mysql"
)

// accountTask 将后台执行登记到与 HTTP 共用的冻结边界，队列载荷不变。
func (s *Server) accountTask(handle asynq.HandlerFunc) asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		var p struct {
			ID     string `json:"id"`
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return err
		}
		if p.ID == "" || p.UserID == "" {
			return fmt.Errorf("task lacks id/user_id")
		}
		ctx, done := s.Activity.Begin(ctx, p.UserID)
		defer done()
		u, err := mysql.GetAccount(ctx, s.db, p.UserID)
		if err != nil {
			return err
		}
		if u.Status != "active" {
			return mysql.ErrAccountInactive
		}
		ctx, finish := s.Activity.Begin(ctx, account.WorkKey(p.UserID, p.ID))
		defer finish()
		return handle(ctx, t)
	}
}
