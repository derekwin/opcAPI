package main

import (
	"context"
	"os"

	"github.com/derekwin/opcAPI/internal/app"
	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/observability"
)

func main() {
	cfg := config.Load()
	logger := observability.NewLogger(cfg.LogLevel)

	api := app.New(cfg, logger)
	if err := api.Run(context.Background()); err != nil {
		logger.Error("api server exited with error", "err", err)
		os.Exit(1)
	}
}
