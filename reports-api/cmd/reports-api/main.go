// Сервис отчётов BionicPRO: GET /reports отдаёт пользователю его отчёт из витрины ClickHouse.
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

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/MicahParks/keyfunc/v3"

	"bionicpro/reports-api/internal/auth"
	"bionicpro/reports-api/internal/config"
	"bionicpro/reports-api/internal/httpapi"
	"bionicpro/reports-api/internal/report"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("сервис остановлен с ошибкой", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.ClickHouseAddr},
		Auth: clickhouse.Auth{
			Database: cfg.ClickHouseDatabase,
			Username: cfg.ClickHouseUser,
			Password: cfg.ClickHousePassword,
		},
		DialTimeout: 5 * time.Second,
		Settings:    clickhouse.Settings{"max_execution_time": 10},
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	// JWKS кэшируется и обновляется в фоне; недоступность Keycloak при старте не фатальна
	jwks, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.OIDCJWKSURL})
	if err != nil {
		return err
	}

	store := report.NewClickHouseStore(conn)
	api := httpapi.New(
		report.NewService(store, cfg.DefaultPeriodDays, cfg.MaxPeriodDays),
		auth.NewVerifier(jwks.Keyfunc, cfg.OIDCIssuer, cfg.AllowedAZP, cfg.RequiredRole),
		store,
		cfg.CORSAllowedOrigins,
		log,
	)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("reports-api запущен", "addr", cfg.Addr, "issuer", cfg.OIDCIssuer)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}
