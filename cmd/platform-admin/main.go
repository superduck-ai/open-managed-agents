package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platformadmin"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stderr, slog.LevelInfo))
	slog.SetDefault(logger)
	if err := run(os.Args[1:], os.Stdout, logger); err != nil {
		logger.Error("平台管理员命令失败", "error", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer, logger *slog.Logger) error {
	if len(args) == 0 {
		return errors.New("用法：platform-admin grant|revoke|list --account-uuid UUID --operator 运维身份")
	}
	command := args[0]
	if command != "grant" && command != "revoke" && command != "list" {
		return errors.New("命令必须为 grant、revoke 或 list")
	}
	flags := flag.NewFlagSet("platform-admin", flag.ContinueOnError)
	account := flags.String("account-uuid", "", "稳定登录账号 UUID")
	operator := flags.String("operator", "", "运维操作者标识")
	reason := flags.String("reason", "", "兼容旧用法的原因字段，可省略")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不支持额外位置参数")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	database, err := db.Open(ctx, cfg, logger.With("component", "database"))
	if err != nil {
		return err
	}
	defer database.Close()
	service := platformadmin.New(database, logger)
	if command == "list" {
		accounts, err := service.Administrators(ctx)
		if err != nil {
			return err
		}
		return json.MarshalWrite(out, accounts)
	}
	if err := service.SetAdministrator(ctx, *account, *operator, *reason, command == "grant"); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "平台管理员授权已更新")
	return err
}
