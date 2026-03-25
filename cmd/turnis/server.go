package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/atoolz/turnis/internal/api"
	"github.com/atoolz/turnis/internal/config"
	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/notify"
	ntfySender "github.com/atoolz/turnis/internal/notify/ntfy"
	webhookSender "github.com/atoolz/turnis/internal/notify/webhook"
	"github.com/atoolz/turnis/internal/store"
)

func loadConfig(path string) (*config.Config, error) {
	return config.Load(path)
}

func runServer(cfg *config.Config) error {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel(),
	}))
	slog.SetDefault(log)

	db, err := store.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	dispatcher := notify.NewDispatcher()
	dispatcher.Register(webhookSender.New())
	dispatcher.Register(ntfySender.New(cfg.Ntfy.Server))

	sa := &storeAdapter{db: db}
	na := &dispatcherAdapter{dispatcher: dispatcher}
	engine := escalation.NewEngine(sa, na, cfg.Server.BaseURL)

	router := api.NewRouter(db, cfg, engine)

	srv := &http.Server{
		Addr:         cfg.Server.ListenAddr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("turnis server starting",
			"addr", cfg.Server.ListenAddr(),
			"channels", dispatcher.AvailableChannels(),
		)
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	}

	engine.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	slog.Info("turnis stopped")
	return nil
}
