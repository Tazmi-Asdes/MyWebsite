package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mywebsite/internal/app"
	"mywebsite/internal/article"
	"mywebsite/internal/auth"
	"mywebsite/internal/database"
	"mywebsite/internal/markdown"
	"mywebsite/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	config, err := app.LoadConfig()
	if err != nil {
		logger.Error("configuration_invalid", "error", err)
		os.Exit(1)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	databaseConnection, err := database.Open(startupContext, config.Database)
	if err == nil {
		err = databaseConnection.CheckReady(startupContext)
	}
	cancelStartup()
	if err != nil {
		if databaseConnection != nil {
			_ = databaseConnection.Close()
		}
		logger.Error("database_initialization_failed")
		os.Exit(1)
	}
	defer databaseConnection.Close()
	queries := databaseConnection.Queries()
	authService := auth.NewService(queries, auth.WithSessionTimeouts(config.SessionIdleDuration, config.SessionAbsoluteDuration))
	articleService := article.NewService(queries, markdown.NewRenderer())

	handler, err := app.NewHandler(app.HandlerOptions{
		Logger:        logger,
		Clock:         platform.NewShanghaiClock(),
		Readiness:     databaseConnection,
		Auth:          authService,
		Articles:      articleService,
		PublicBaseURL: config.PublicBaseURL,
		CookieSecure:  config.CookieSecure,
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
