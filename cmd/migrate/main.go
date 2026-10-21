// migrate 独立执行数据库迁移。
//
//	Usage: go run ./cmd/migrate [up|down]
package main

import (
	"context"
	"log"
	"os"

	"github.com/zhaocore/dutybotgo/internal/config"
	"github.com/zhaocore/dutybotgo/internal/store/mysql"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx := context.Background()
	db, err := mysql.New(ctx, cfg.Database.URL,
		cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.ConnMaxLifetime)
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}
	defer db.Close()

	action := "up"
	if len(os.Args) > 1 {
		action = os.Args[1]
	}

	switch action {
	case "up":
		if err := db.RunMigrations("migrations"); err != nil {
			log.Fatalf("migrate up: %v", err)
		}
	default:
		log.Fatalf("unknown migrate action: %s (only 'up' supported)", action)
	}

	log.Println("migration complete")
}
