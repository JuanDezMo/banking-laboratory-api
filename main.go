// Punto de entrada: configuración, BD, migraciones, repos, services, rutas y servidor.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib" // registra el driver "pgx" para database/sql

	"accounts-api/internal/handler"
	"accounts-api/internal/service"
	"accounts-api/migrations"
)

const (
	listenAddr         = ":3000"
	minJWTSecretLength = 32 // bank-lab-spec.md: JWT_SECRET de al menos 32 caracteres
	dbPingTimeout      = 5 * time.Second
	shutdownTimeout    = 10 * time.Second
)

type config struct {
	databaseURL string
	jwtSecret   string
}

func main() {
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
	slog.Info("server stopped")
}

func run() error {
	if err := loadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	// ctx se cancela con Ctrl+C (SIGINT) o con SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := openDB(ctx, cfg.databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	slog.Info("database connected")

	if err := migrations.Apply(ctx, db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("migrations applied")

	// Dependencias: services → handlers → rutas.
	passwords := service.NewPasswordHasher(runtime.NumCPU()) // un hash argon2 por CPU a la vez
	tokens := service.NewTokenIssuer(cfg.jwtSecret)
	authService := service.NewAuthService(db, passwords, tokens)
	handlers := handler.NewHandlers(authService, tokens)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           handler.NewRouter(handlers),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return serve(ctx, server)
}

// loadConfig lee y valida las variables de entorno obligatorias.
func loadConfig() (config, error) {
	cfg := config{
		databaseURL: os.Getenv("DATABASE_URL"),
		jwtSecret:   os.Getenv("JWT_SECRET"),
	}
	if cfg.databaseURL == "" {
		return cfg, errors.New("DATABASE_URL is required")
	}
	if utf8.RuneCountInString(cfg.jwtSecret) < minJWTSecretLength {
		return cfg, fmt.Errorf("JWT_SECRET is required and must have at least %d characters", minJWTSecretLength)
	}
	return cfg, nil
}

// openDB abre el pool de conexiones y comprueba que la BD responde.
func openDB(ctx context.Context, url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, dbPingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return db, nil
}

// serve atiende peticiones hasta que ctx se cancela y luego apaga el servidor de forma ordenada:
// deja de aceptar conexiones y espera a que terminen las peticiones en curso.
func serve(ctx context.Context, server *http.Server) error {
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", server.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		// El servidor terminó sin que se pidiera apagarlo (por ejemplo, puerto ocupado).
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
