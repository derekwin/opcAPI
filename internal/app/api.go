package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/derekwin/opcAPI/internal/audit"
	"github.com/derekwin/opcAPI/internal/auth"
	"github.com/derekwin/opcAPI/internal/billing"
	"github.com/derekwin/opcAPI/internal/config"
	"github.com/derekwin/opcAPI/internal/keymgmt"
	"github.com/derekwin/opcAPI/internal/promptapi"
	"github.com/derekwin/opcAPI/internal/provider"
	"github.com/derekwin/opcAPI/internal/quota"
	redisstore "github.com/derekwin/opcAPI/internal/store/redis"
	httptransport "github.com/derekwin/opcAPI/internal/transport/http"
	"github.com/derekwin/opcAPI/internal/user"
)

// API wires core dependencies and controls application lifecycle.
type API struct {
	cfg          config.Config
	logger       *slog.Logger
	server       *http.Server
	policySyncer *keymgmt.PolicySyncer
	initErr      error
}

func New(cfg config.Config, logger *slog.Logger) *API {
	gateway := provider.NewGateway(cfg, logger)
	authManager := auth.NewManager(cfg, logger)

	userStore, userStoreErr := user.NewFileStore(cfg.UserStorePath)
	userService := user.NewService(userStore, user.NewSessionManager(cfg.SessionSecret, cfg.SessionTTL), cfg.PhoneTestCode, logger)
	billingStore, billingStoreErr := billing.NewFileStore(cfg.BillingStorePath)
	billingService := billing.NewService(billingStore, logger)
	promptStore, promptStoreErr := promptapi.NewFileStore(cfg.PromptAPIStorePath)
	promptService := promptapi.NewService(promptStore, logger)
	auditPath := strings.TrimSpace(cfg.AuditStorePath)
	if auditPath == "" {
		auditPath = "data/audit_logs.json"
	}
	auditStore, auditStoreErr := audit.NewFileStore(auditPath)
	auditService := audit.NewService(auditStore, logger)

	var redisClient *redisstore.Client
	var limiter *quota.Limiter

	if strings.EqualFold(strings.TrimSpace(cfg.PolicyBackend), "redis") {
		redisClient = redisstore.NewClient(redisstore.Config{
			Addr:         cfg.RedisAddr,
			Password:     cfg.RedisPassword,
			DB:           cfg.RedisDB,
			DialTimeout:  cfg.RedisDialTimeout,
			ReadTimeout:  cfg.RedisReadTimeout,
			WriteTimeout: cfg.RedisWriteTimeout,
		})
		limiter = quota.NewRedisLimiter(cfg.RequestRPMLimit, cfg.DailyTokenLimit, redisClient, cfg.RedisQuotaPrefix)
	} else {
		limiter = quota.NewLimiter(cfg.RequestRPMLimit, cfg.DailyTokenLimit)
	}

	policySyncer := keymgmt.NewPolicySyncer(cfg, logger, authManager, limiter, redisClient)
	handler := httptransport.NewRouter(logger, gateway, authManager, cfg.RequireTenantBoundKey, limiter, policySyncer, userService, billingService, promptService, auditService)

	server := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return &API{
		cfg:          cfg,
		logger:       logger,
		server:       server,
		policySyncer: policySyncer,
		initErr:      firstErr(userStoreErr, billingStoreErr, promptStoreErr, auditStoreErr),
	}
}

func (a *API) Run(ctx context.Context) error {
	if a.initErr != nil {
		return a.initErr
	}
	sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if a.policySyncer != nil {
		if err := a.policySyncer.Initialize(sigCtx); err != nil {
			return err
		}
		go a.policySyncer.Start(sigCtx)
	}

	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("api server starting", "addr", a.cfg.HTTPAddr)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-sigCtx.Done():
		a.logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	a.logger.Info("api server stopped")
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
