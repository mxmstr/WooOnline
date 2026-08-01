package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"stranglehold-go-server/internal/config"
	"stranglehold-go-server/internal/server"
)

func main() {
	if err := config.LoadDotEnv(".env"); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(2)
	}

	var cfg config.Config
	flag.StringVar(&cfg.BindHost, "bind", config.Env("STRANGLEHOLD_BIND_HOST", "0.0.0.0"), "address to listen on")
	flag.StringVar(&cfg.PublicHost, "public-host", config.Env("STRANGLEHOLD_PUBLIC_HOST", "127.0.0.1"), "address advertised to clients")
	flag.IntVar(&cfg.AuthPort, "auth-port", config.EnvInt("STRANGLEHOLD_AUTH_PORT", 30670), "authentication UDP port")
	flag.IntVar(&cfg.SecurePort, "secure-port", config.EnvInt("STRANGLEHOLD_SECURE_PORT", 30671), "Rendez-Vous UDP port")
	flag.IntVar(&cfg.NATPort, "nat-port", config.EnvInt("STRANGLEHOLD_NAT_PORT", 30672), "NAT traversal UDP port")
	flag.StringVar(&cfg.DatabasePath, "db", config.Env("STRANGLEHOLD_DB_PATH", "server.db"), "SQLite database path")
	flag.StringVar(&cfg.AccountsFile, "accounts-file", config.Env("STRANGLEHOLD_ACCOUNTS_FILE", "accounts.json"), "JSON file of accounts to preload")
	flag.StringVar(&cfg.AccessKey, "access-key", config.Env("STRANGLEHOLD_ACCESS_KEY", ""), "PRUDP checksum access key")
	flag.StringVar(&cfg.RC4Key, "rc4-key", config.Env("STRANGLEHOLD_RC4_KEY", ""), "PRUDP payload RC4 key")
	flag.BoolVar(&cfg.KeepOldMatches, "keep-old-matches", config.EnvBool("KEEP_OLD_MATCHES", false), "retain stale hosted matches for diagnostics")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app, err := server.New(cfg, logger)
	if err != nil {
		logger.Error("initialize server", "error", err)
		os.Exit(1)
	}
	defer app.Close()

	if err := app.Run(ctx); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
