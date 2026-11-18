// Command server is a tiny proxy between the iOS app and the Anthropic API.
// It holds the API key so the app never has to.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/BonnieDooneConsulting/swiftui-ai-chat/server/internal/chat"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	limits := chat.DefaultLimits
	limits.MaxBodyBytes = int64(envInt("MAX_BODY_BYTES", int(limits.MaxBodyBytes)))
	limits.MaxMessages = envInt("MAX_MESSAGES", limits.MaxMessages)
	limits.StreamTimeout = envDuration("STREAM_TIMEOUT", limits.StreamTimeout)

	var streamer chat.Streamer
	if os.Getenv("MOCK") == "1" {
		logger.Info("MOCK=1: streaming a canned reply, no API calls")
		streamer = chat.MockStreamer{Delay: envDuration("MOCK_DELAY", 60*time.Millisecond)}
	} else {
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return errors.New("ANTHROPIC_API_KEY is not set (or run with MOCK=1)")
		}
		model := envString("ANTHROPIC_MODEL", chat.DefaultModel)
		logger.Info("using Anthropic", "model", model)
		streamer = &chat.AnthropicStreamer{
			APIKey:    key,
			BaseURL:   envString("ANTHROPIC_BASE_URL", chat.DefaultBaseURL),
			Model:     model,
			Effort:    envString("ANTHROPIC_EFFORT", chat.DefaultEffort),
			MaxTokens: envInt("ANTHROPIC_MAX_TOKENS", chat.DefaultMaxTokens),
			System:    os.Getenv("SYSTEM_PROMPT"),
			Fallbacks: os.Getenv("ANTHROPIC_FALLBACKS") != "off",
			// No overall client timeout: replies stream for a while. The
			// handler's per-request context enforces STREAM_TIMEOUT instead.
			Client: &http.Client{Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       90 * time.Second,
				MaxIdleConnsPerHost:   10,
			}},
			Logger: logger,
		}
	}

	h := &chat.Handler{
		Streamer: streamer,
		Limiter: chat.NewRateLimiter(
			envFloat("RATE_LIMIT_PER_MINUTE", 10),
			envInt("RATE_LIMIT_BURST", 5),
		),
		Limits: limits,
		Logger: logger,
	}

	addr := ":" + envString("PORT", "8080")
	srv := &http.Server{
		Addr:              addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Leave room for the longest allowed streamed reply.
		WriteTimeout:   limits.StreamTimeout + 15*time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 16 * 1024,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(key)); err == nil {
		return v
	}
	return def
}
