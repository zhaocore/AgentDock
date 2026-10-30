package worker

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
)

// CrawlControlQueue 为短派发任务预留执行资源，避免被长采集任务占满。
const CrawlControlQueue = "crawl-control"

// StartCrawlDispatcher 启动独立控制队列；时钟显式注入以验证定时窗口。
func StartCrawlDispatcher(addr, password string, worker *CrawlWorker, now func() time.Time) (*asynq.Server, error) {
	server := asynq.NewServer(asynq.RedisClientOpt{Addr: addr, Password: password}, asynq.Config{Concurrency: 1, Queues: map[string]int{CrawlControlQueue: 1}})
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeCrawlDispatch, func(ctx context.Context, _ *asynq.Task) error {
		return worker.DispatchDaily(ctx, now())
	})
	if err := server.Start(mux); err != nil {
		return nil, err
	}
	return server, nil
}
