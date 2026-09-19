package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"mywebsite/internal/app"
	"mywebsite/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	config, err := app.LoadConfig()
	if err != nil {
		logger.Error("configuration_invalid", "error", err)
		os.Exit(1)
	}

	handler, err := app.NewHandler(app.HandlerOptions{
		Logger: logger,
		Clock:  platform.NewShanghaiClock(),
	})
	if err != nil {
		logger.Error("handler_initialization_failed", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              config.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		WriteTimeout:      config.WriteTimeout,
		IdleTimeout:       config.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("server_starting", "addr", config.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)

	select {
	case signal := <-signals:
		logger.Info("shutdown_requested", "signal", signal.String())
		ctx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("server_shutdown_failed", "error", err)
			return
		}
		logger.Info("server_stopped")
	case err := <-serverErrors:
		logger.Error("server_failed", "error", err)
	}
}
