package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"github.com/hibiken/asynq"
	"github.com/zhaocore/agentdock/internal/domain"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"github.com/zhaocore/agentdock/internal/store/rustfs"
)

// TypeConfluenceExport 两阶段导出任务，不在队列中存凭据。
const TypeConfluenceExport = "confluence:export"

type confluencePayload struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`
}

// EnqueueConfluence 入队运行请求，由数据库状态原子领取检查点。
func EnqueueConfluence(ctx context.Context, client Enqueuer, id, userID string) error {
	payload, err := json.Marshal(confluencePayload{id, userID})
	if err != nil {
		return err
	}
	_, err = client.EnqueueContext(ctx, asynq.NewTask(TypeConfluenceExport, payload), asynq.MaxRetry(0), asynq.Timeout(24*time.Hour))
	return err
}

// ConfluenceWorker 编排数据库检查点、远程导出与对象发布。
type ConfluenceWorker struct {
	DB      *sql.DB
	Objects rustfs.Objects
	Source  ExportSource
}

// ConfluencePrefix 所有对象以用户和任务隔离。
func ConfluencePrefix(task domain.ConfluenceTask) string {
	user := task.StorageUserID
	if user == "" {
		user = task.UserID
	}
	return "confluence/" + user + "/" + task.ID + "/"
}

// ReadConfluenceTree 读取冻结快照并校验计划。
func ReadConfluenceTree(ctx context.Context, objects rustfs.Objects, task domain.ConfluenceTask) (domain.ConfluenceTree, error) {
	data, err := objects.Get(ctx, ConfluencePrefix(task)+"tree.json")
	if err != nil {
		return domain.ConfluenceTree{}, err
	}
	return decodeTree(data)
}

// ReadConfluencePlan 旧任务沿用全量计划，新任务读取冻结范围，不修改原始树。
func ReadConfluencePlan(ctx context.Context, objects rustfs.Objects, task domain.ConfluenceTask) (domain.ConfluenceTree, error) {
	tree, err := ReadConfluenceTree(ctx, objects, task)
	if err != nil || task.Stage != "markdown" {
		return tree, err
	}
	data, err := objects.Get(ctx, ConfluencePrefix(task)+"selection.json")
	if err != nil {
		return tree, err
	}
	var selection domain.ConfluenceSelection
	if err := json.Unmarshal(data, &selection); err != nil {
		return tree, fmt.Errorf("decode Confluence selection: %w", err)
	}
	if selection.NodeID == nil {
		return tree, fmt.Errorf("Confluence selection lacks node_id")
	}
	pages, err := domain.SelectConfluencePages(tree, *selection.NodeID)
	if err != nil {
		return tree, err
	}
	tree.Pages, tree.SelectedNodeID = pages, selection.NodeID
	return tree, nil
}

func decodeTree(data []byte) (domain.ConfluenceTree, error) {
	var tree domain.ConfluenceTree
	if err := json.Unmarshal(data, &tree); err != nil {
		return tree, err
	}
	if tree.Nodes == nil || tree.Pages == nil {
		return tree, fmt.Errorf("Confluence tree lacks nodes/pages")
	}
	seen := map[string]bool{}
	for _, page := range tree.Pages {
		if !regexp.MustCompile(`^\d+$`).MatchString(page.ID) || seen[page.ID] || page.Version < 1 || page.Title == "" || page.Path == "" {
			return tree, fmt.Errorf("invalid Confluence page plan")
		}
		seen[page.ID] = true
	}
	return tree, nil
}

// Handle 在停止/服务关闭时终止外部请求，保留上次已发布检查点。
func (w *ConfluenceWorker) Handle(ctx context.Context, t *asynq.Task) error {
	var payload confluencePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return err
	}
	task, err := mysql.GetConfluenceTask(ctx, w.DB, payload.ID, payload.UserID)
	if err != nil {
		return err
	}
	if task.Status != "queued" {
		return nil
	}

	ok, err := mysql.TransitionConfluenceTask(ctx, w.DB, task.ID, task.UserID, "queued", "running")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				current, err := mysql.GetConfluenceTask(runCtx, w.DB, task.ID, task.UserID)
				if err != nil {
					slog.Error("Confluence stop monitor failed", "task_id", task.ID, "error", err)
					cancel()
					return
				}
				if current.Status != "running" {
					cancel()
					return
				}
			}
		}
	}()
	err = w.run(runCtx, task)
	cancel()
	<-monitorDone
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	current, readErr := mysql.GetConfluenceTask(finishCtx, w.DB, task.ID, task.UserID)
	if readErr != nil {
		return readErr
	}
	if current.Status == "stopping" {
		_, err = mysql.TransitionConfluenceTask(finishCtx, w.DB, task.ID, task.UserID, "stopping", "stopped")
		return err
	}
	if err != nil {
		if recordErr := mysql.FailConfluenceTask(finishCtx, w.DB, task.ID, task.UserID, err.Error()); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return err
	}
	next := "completed"
	if current.Stage == "selection" {
		next = "waiting"
	}
	_, err = mysql.TransitionConfluenceTask(finishCtx, w.DB, task.ID, task.UserID, "running", next)
	return err
}

func (w *ConfluenceWorker) run(ctx context.Context, task domain.ConfluenceTask) error {
	if task.Stage == "tree" {
		data, err := w.Source.Tree(ctx, task.URL)
		if err != nil {
			return err
		}
		tree, err := decodeTree(data)
		if err != nil {
			return err
		}
		if err = w.Objects.Put(ctx, ConfluencePrefix(task)+"tree.json", data, "application/json"); err != nil {
			return err
		}
		return mysql.ProgressConfluenceTask(ctx, w.DB, task.ID, task.UserID, "selection", 0, len(tree.Pages))
	}
	if task.Stage != "export" && task.Stage != "markdown" {
		return fmt.Errorf("select a Confluence subtree before starting Markdown export")
	}
	tree, err := ReadConfluencePlan(ctx, w.Objects, task)
	if err != nil {
		return err
	}
	if task.Completed < 0 || task.Completed > len(tree.Pages) {
		return fmt.Errorf("invalid Confluence checkpoint completed=%d total=%d", task.Completed, len(tree.Pages))
	}
	for index := task.Completed; index < len(tree.Pages); index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		page := tree.Pages[index]
		files, err := w.Source.Page(ctx, page)
		if err != nil {
			return err
		}
		foundMarkdown := false
		for _, file := range files {
			if !ValidConfluenceFile(file.Name) {
				return fmt.Errorf("invalid export filename %q", file.Name)
			}
			if file.Name == "page.md" {
				foundMarkdown = true
			}
			if err = w.Objects.Put(ctx, ConfluencePrefix(task)+page.ID+"/"+file.Name, file.Content, "application/octet-stream"); err != nil {
				return err
			}
		}
		if !foundMarkdown {
			return fmt.Errorf("export lacks Markdown page_id=%s", page.ID)
		}
		if err = mysql.ProgressConfluenceTask(ctx, w.DB, task.ID, task.UserID, task.Stage, index+1, len(tree.Pages)); err != nil {
			return err
		}
	}
	return nil
}

// ValidConfluenceFile 只允许转换器生成的 Markdown 与哈希附件名。
func ValidConfluenceFile(name string) bool {
	return name == "page.md" || regexp.MustCompile(`^assets/asset-[a-f0-9]{24}\.[a-z0-9]{1,8}$`).MatchString(name)
}
