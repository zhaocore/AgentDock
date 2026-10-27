package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/zhaocore/dutybotgo/internal/domain"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

var (
	ErrConfluenceSelectionUnavailable = errors.New("Confluence tree is not waiting for a selection; refresh task")
	ErrInvalidConfluenceSelection     = errors.New("invalid Confluence subtree selection")
)

// SelectExport 原子领取选择，冻结范围后入队；失败保留原树并记录可恢复状态。
func (w *ConfluenceWorker) SelectExport(ctx context.Context, task domain.ConfluenceTask, nodeID string, queue Enqueuer) (updated domain.ConfluenceTask, err error) {
	completed := task.Stage == "markdown" && task.Status == "completed"
	if !completed && (task.Stage != "selection" || (task.Status != "waiting" && task.Status != "stopped" && task.Status != "failed")) {
		return task, ErrConfluenceSelectionUnavailable
	}
	tree, err := ReadConfluenceTree(ctx, w.Objects, task)
	if err != nil {
		return task, err
	}
	pages, err := domain.SelectConfluencePages(tree, nodeID)
	if err != nil {
		return task, fmt.Errorf("%w: %v", ErrInvalidConfluenceSelection, err)
	}
	if completed {
		data, readErr := w.Objects.Get(ctx, ConfluencePrefix(task)+"tree.json")
		if readErr != nil {
			return task, fmt.Errorf("read completed Confluence tree: %w", readErr)
		}
		now := time.Now().UTC()
		next := domain.ConfluenceTask{ID: uuid.NewString(), UserID: task.UserID, URL: task.URL, Stage: "selection", Status: "waiting", Total: len(tree.Pages), CreatedAt: now, UpdatedAt: now}
		if err := w.Objects.Put(ctx, ConfluencePrefix(next)+"tree.json", data, "application/json"); err != nil {
			return task, fmt.Errorf("copy completed Confluence tree: %w", err)
		}
		if err := mysql.CreateConfluenceSelection(ctx, w.DB, next); err != nil {
			return task, fmt.Errorf("create next Confluence export: %w", err)
		}
		task = next
	}
	changed, err := mysql.TransitionConfluenceTask(ctx, w.DB, task.ID, task.UserID, task.Status, "selecting")
	if err != nil {
		return task, err
	}
	if !changed {
		return task, ErrConfluenceSelectionUnavailable
	}
	defer func() {
		if err != nil {
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			err = errors.Join(err, mysql.FailConfluenceTask(finishCtx, w.DB, task.ID, task.UserID, err.Error()))
		}
	}()
	selection, err := json.Marshal(domain.ConfluenceSelection{NodeID: &nodeID})
	if err != nil {
		return task, err
	}
	if err = w.Objects.Put(ctx, ConfluencePrefix(task)+"selection.json", selection, "application/json"); err != nil {
		return task, fmt.Errorf("save Confluence selection: %w", err)
	}
	changed, err = mysql.QueueConfluenceSelection(ctx, w.DB, task.ID, task.UserID, len(pages))
	if err != nil {
		return task, err
	}
	if !changed {
		return task, ErrConfluenceSelectionUnavailable
	}
	if err = EnqueueConfluence(ctx, queue, task.ID, task.UserID); err != nil {
		return task, fmt.Errorf("enqueue selected Confluence pages: %w", err)
	}
	task.Stage, task.Status, task.Completed, task.Total, task.Error = "markdown", "queued", 0, len(pages), ""
	return task, nil
}
