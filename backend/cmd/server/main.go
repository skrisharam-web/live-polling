// Command server is the live-polling API: a single Go process serving the REST
// API and the WebSocket endpoint, backed by MongoDB for durable data and Redis
// for live counters and realtime fan-out.
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

	"github.com/skrisharam-web/live-polling/backend/internal/config"
	"github.com/skrisharam-web/live-polling/backend/internal/database"
	"github.com/skrisharam-web/live-polling/backend/internal/handlers"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
	"github.com/skrisharam-web/live-polling/backend/internal/redis"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
	"github.com/skrisharam-web/live-polling/backend/internal/router"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
	"github.com/skrisharam-web/live-polling/backend/internal/websocket"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with an error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Starting with a broken configuration is worse than not starting at all, so
	// this is a hard stop. main logs the error; logging it here too would print
	// the same failure twice.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	configureLogging(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mongo, err := database.Connect(ctx, cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongo.Close(closeCtx); err != nil {
			slog.Warn("mongodb shutdown failed", "error", err)
		}
	}()

	if err := database.EnsureIndexes(ctx, mongo); err != nil {
		return err
	}
	slog.Info("mongodb ready", "database", cfg.MongoDatabase)

	redisClient, err := redis.Connect(ctx, cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Warn("redis shutdown failed", "error", err)
		}
	}()
	slog.Info("redis ready")

	// Composition root: every dependency is constructed once, here, and passed
	// down explicitly. No package reaches for a global.
	userRepo := repositories.NewUserRepository(mongo)
	pollRepo := repositories.NewPollRepository(mongo)
	voteRepo := repositories.NewVoteRepository(mongo)

	authService := services.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTExpiresIn)
	resultService := services.NewResultService(voteRepo, redisClient)
	realtimeService := services.NewRealtimeService(redisClient, redisClient)
	// One object announces poll standings, whether the trigger is a vote or a
	// management action such as closing the poll.
	broadcaster := services.NewResultBroadcaster(resultService, realtimeService)

	pollService := services.NewPollService(pollRepo, voteRepo, resultService, broadcaster)
	voteService := services.NewVoteService(pollRepo, voteRepo, resultService, broadcaster)

	// The hub and its Redis subscription start before the HTTP server, so no
	// connection can be accepted before there is something to feed it.
	hub := websocket.NewHub()
	if err := websocket.NewManager(hub, realtimeService).Run(ctx); err != nil {
		return err
	}
	slog.Info("realtime fan-out ready")

	cookies := middleware.NewCookieSettings(cfg)
	// The voter cookie is signed with the same secret as the session token: both
	// are server-issued identities and both must be unforgeable.
	voterIdentity := middleware.NewVoterIdentity(cfg.JWTSecret, cookies)

	engine := router.New(cfg, router.Dependencies{
		Health:       handlers.NewHealthHandler(mongo, redisClient),
		Auth:         handlers.NewAuthHandler(authService, cookies),
		Poll:         handlers.NewPollHandler(pollService),
		Vote:         handlers.NewVoteHandler(voteService),
		WebSocket:    handlers.NewWebSocketHandler(hub, pollService, resultService, cfg.AllowedOrigins),
		UserResolver: authService,
		Voter:        voterIdentity,
		Limiter:      redisClient,
	})

	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: engine,
		// WebSocket connections are long-lived, so no global write timeout is set;
		// per-connection deadlines live in the websocket package instead.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "port", cfg.Port, "env", string(cfg.AppEnv))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining connections")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	slog.Info("server stopped cleanly")
	return nil
}

// configureLogging installs the process-wide structured logger: JSON in
// production so a log platform can index it, text locally so it stays readable.
func configureLogging(cfg *config.Config) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var handler slog.Handler
	if cfg.IsProduction() {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}
