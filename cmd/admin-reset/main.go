// 管理员恢复命令从标准输入读取新临时密码，不经参数或日志暴露。
package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/zhaocore/agentdock/internal/config"
	"github.com/zhaocore/agentdock/internal/store/mysql"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) != 1 {
		log.Fatal("usage: provide new administrator temporary password via stdin; no arguments")
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		log.Fatal("read password: provide one newline-terminated line on stdin")
	}
	password := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if len(password) < 6 || len(password) > 72 {
		log.Fatal("password must contain 6–72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal(err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := mysql.New(ctx, cfg.Database.URL, cfg.Database.MaxOpenConns, cfg.Database.MaxIdleConns, cfg.Database.ConnMaxLifetime)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	settings, err := mysql.GetAdministration(ctx, db.DB)
	if err != nil {
		log.Fatal(err)
	}
	if settings.AdminUserID == "" {
		log.Fatal("administrator is not initialized; register the first account")
	}
	if err = mysql.ResetAccountPassword(ctx, db.DB, "server-console", settings.AdminUserID, string(hash)); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Administrator password reset; all previous JWTs and API tokens revoked. Change temporary password after login.")
}
