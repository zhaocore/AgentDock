// dutybotGo — 多用户 Go Agent 管理平台。
//
// 基于 langchaingo 的 Agent 服务：仓库管理（git clone + codegraph 初始化）、
// Agent 定义（系统提示词 / skills / MCP / subagent）、实例与会话。
//
// 存储只依赖 MySQL + Redis（Asynq）。
//
//	@title          dutybotGo API
//	@version        0.1.0
//	@description    Agent 管理与仓库托管服务
//	@contact.name   dutybotGo Team
//	@host           localhost:8080
//	@basePath       /v1
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/zhaocore/dutybotgo/internal/agent"
	"github.com/zhaocore/dutybotgo/internal/api"
	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
	"github.com/zhaocore/dutybotgo/internal/store/rustfs"
	"github.com/zhaocore/dutybotgo/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 启动时校验关键配置
	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// MySQL
	db, err := mysql.New(ctx, cfg.Database.URL,
		cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.ConnMaxLifetime)
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	defer db.Close()

	if err := db.RunMigrations("migrations"); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	// 同步入口串行读取完整配置，校验通过后才事务更新公共预设。
	var presetMu sync.Mutex
	syncAgents := func(ctx context.Context) error {
		presetMu.Lock()
		defer presetMu.Unlock()
		presets, err := agent.LoadPresets(cfg.Agent.RootDir, cfg.LLM.Model)
		if err != nil {
			return err
		}
		return mysql.SyncPresets(ctx, db.DB, presets)
	}
	if err := syncAgents(ctx); err != nil {
		log.Fatalf("sync public agents: %v", err)
	}

	// 服务重启后遗留 running 实例置为 stopped（会话历史已持久化）
	if err := mysql.MarkRunningInstancesStopped(ctx, db.DB); err != nil {
		log.Fatalf("reset instance states: %v", err)
	}

	if err := mysql.ResetConfluenceTasks(ctx, db.DB); err != nil {
		log.Fatalf("reset Confluence tasks: %v", err)
	}
	if err := mysql.ResetCrawlRuns(ctx, db.DB); err != nil {
		log.Fatalf("reset crawl runs: %v", err)
	}

	// Asynq 入队客户端
	asynqClient := worker.NewClient(cfg.Redis.Addr, cfg.Redis.Password)
	defer asynqClient.Close()
	crawlInspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password})
	defer crawlInspector.Close()

	// Agent 运行时与实例管理器
	runtime := agent.NewLangChainRuntime(cfg.LLM.APIKey, cfg.LLM.BaseURL)
	instances := agent.NewManager(db.DB, runtime)

	// API Handler
	h := &api.Handler{
		Config:      cfg,
		DB:          db.DB,
		AsynqClient: asynqClient,
		Instances:   instances,
		Runtime:     runtime,
		SyncAgents:  syncAgents,
		// Redis 健康探测：Asynq 客户端未暴露 ping，这里直接建临时连接校验
		RedisHealth: func(ctx context.Context) error {
			rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password})
			defer rdb.Close()
			return rdb.Ping(ctx).Err()
		},
	}

	slog.Info("worker concurrency configured", "limits", cfg.Concurrency)

	// Asynq Worker（仓库克隆 / 同步 / 删除）
	workerSrv := worker.NewServer(cfg.Redis.Addr, cfg.Redis.Password, db.DB, cfg.Repo.RootDir, cfg.Concurrency.Worker)
	var crawlScheduler *asynq.Scheduler
	var crawlDispatcher *asynq.Server

	if cfg.RustFS.Endpoint != "" && cfg.RustFS.Region != "" && cfg.RustFS.Bucket != "" && cfg.RustFS.AccessKey != "" && cfg.RustFS.SecretKey != "" {
		objects := rustfs.New(cfg.RustFS.Endpoint, cfg.RustFS.Region, cfg.RustFS.Bucket, cfg.RustFS.AccessKey, cfg.RustFS.SecretKey)
		crawl := &worker.CrawlWorker{Concurrency: cfg.Concurrency, DB: db.DB, Objects: objects, Source: worker.NewBrowserCrawlSource(cfg.Agent.RootDir, cfg.Concurrency.Capture), Reporter: agent.NewReporter(agent.NewReportRuntime(cfg.LLM.APIKey, cfg.LLM.BaseURL), cfg.Concurrency.Report), Profiles: cfg.CrawlProfiles, Client: asynqClient, Inspector: crawlInspector}
		h.Crawl = crawl
		workerSrv.Crawl = crawl
		var err error
		crawlDispatcher, err = worker.StartCrawlDispatcher(cfg.Redis.Addr, cfg.Redis.Password, crawl, time.Now)
		if err != nil {
			log.Fatal(err)
		}
		zone, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			log.Fatal(err)
		}
		crawlScheduler = asynq.NewScheduler(asynq.RedisClientOpt{Addr: cfg.Redis.Addr, Password: cfg.Redis.Password}, &asynq.SchedulerOpts{Location: zone, PostEnqueueFunc: func(_ *asynq.TaskInfo, err error) {
			if err != nil {
				slog.Error("crawl schedule enqueue failed", "error", err)
			}
		}})
		if _, err = crawlScheduler.Register("0 9 * * *", asynq.NewTask(worker.TypeCrawlDispatch, nil), asynq.Queue(worker.CrawlControlQueue), asynq.MaxRetry(0), asynq.Timeout(time.Minute)); err != nil {
			log.Fatal(err)
		}
		if err = crawlScheduler.Start(); err != nil {
			log.Fatal(err)
		}
		go func() {
			timer := time.NewTicker(30 * time.Second)
			defer timer.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := crawl.RecoverDispatch(ctx); err != nil {
						slog.Error("crawl dispatch recovery failed", "error", err)
					}
				}
			}
		}()
		h.ConfluenceObjects = objects
		h.ConfluenceLibrary = &worker.ConfluenceLibrary{DB: db.DB, Objects: objects}
		if cfg.Confluence.BaseURL != "" && cfg.Confluence.Cookie != "" {
			if err := worker.ValidateConfluenceURL(cfg.Confluence.BaseURL, cfg.Confluence.BaseURL+"/pages/viewpage.action?pageId=1"); err != nil {
				log.Fatal(err)
			}
			workerSrv.Confluence = &worker.ConfluenceWorker{DB: db.DB, Objects: objects, Source: worker.NewConfluenceSource(cfg.Confluence, cfg.Agent.RootDir)}
		} else {
			log.Print("Confluence live export unavailable: configure CONFLUENCE_BASE_URL/COOKIE")
		}
	} else {
		log.Print("Confluence library and export unavailable: configure RUSTFS_ENDPOINT/REGION/BUCKET/ACCESS_KEY/SECRET_KEY")
	}

	errCh := make(chan error, 2)
	go func() {
		log.Println("asynq worker starting...")
		if err := workerSrv.Start(); err != nil {
			errCh <- fmt.Errorf("asynq worker stopped: %w", err)
		}
	}()

	// HTTP 服务
	router := api.NewRouter(h)

	go func() {
		addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
		log.Printf("dutybotGo HTTP server starting on %s", addr)
		if err := router.Run(addr); err != nil {
			errCh <- err
		}
	}()

	// 优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	var runErr error
	select {
	case err := <-errCh:
		runErr = err
	case sig := <-quit:
		log.Printf("received signal %v, shutting down", sig)
	}
	cancel()
	if crawlScheduler != nil {
		crawlScheduler.Shutdown()
	}
	if crawlDispatcher != nil {
		crawlDispatcher.Shutdown()
	}
	instances.ShutdownAll()
	workerSrv.Shutdown()
	if runErr != nil {
		log.Fatalf("server error: %v", runErr)
	}
}
