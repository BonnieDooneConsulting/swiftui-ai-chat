package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// Limits caps what a single request may contain.
type Limits struct {
	MaxBodyBytes    int64         // whole request body
	MaxMessages     int           // turns per conversation
	MaxContentChars int           // characters per message
	MaxTotalChars   int           // characters across all messages
	StreamTimeout   time.Duration // how long one reply may take end to end
}

// DefaultLimits are reasonable for a demo chat app. Tune them for yours.
var DefaultLimits = Limits{
	MaxBodyBytes:    64 * 1024,
	MaxMessages:     50,
	MaxContentChars: 8000,
	MaxTotalChars:   48000,
	StreamTimeout:   2 * time.Minute,
}

// Handler serves the chat API.
type Handler struct {
	Streamer Streamer
	Limiter  *RateLimiter
	Limits   Limits
	Logger   *slog.Logger
}

// deviceIDPattern accepts UUIDs and similar opaque IDs.
var deviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// Routes returns the server's mux.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("POST /v1/chat", h.chat)
	return mux
}

func (h *Handler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}` + "\n"))
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	deviceID := r.Header.Get("X-Device-ID")
	if !deviceIDPattern.MatchString(deviceID) {
		writeJSONError(w, http.StatusBadRequest, "missing or invalid X-Device-ID header")
		return
	}

	if h.Limiter != nil {
		if ok, wait := h.Limiter.Allow(deviceID); !ok {
			secs := int(math.Ceil(wait.Seconds()))
			w.Header().Set("Retry-After", strconv.Itoa(max(secs, 1)))
			writeJSONError(w, http.StatusTooManyRequests, "Slow down a little. Try again in a few seconds.")
			return
		}
	}

	limits := h.Limits
	if limits.MaxBodyBytes == 0 {
		limits = DefaultLimits
	}

	r.Body = http.MaxBytesReader(w, r.Body, limits.MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var req Request
	if err := dec.Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "request body must be JSON like {\"messages\":[{\"role\":\"user\",\"content\":\"...\"}]}")
		return
	}
	if err := validate(req, limits); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ctx := r.Context()
	if limits.StreamTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limits.StreamTimeout)
		defer cancel()
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // stop nginx-style proxies from buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	send := func(ev Event) error {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	start := time.Now()
	err := h.Streamer.Stream(ctx, req.Messages, func(text string) error {
		return send(Event{Type: "delta", Text: text})
	})
	switch {
	case err == nil:
		_ = send(Event{Type: "done"})
		h.logger().Info("chat complete", "device", deviceID, "duration", time.Since(start))
	case errors.Is(r.Context().Err(), context.Canceled):
		// The app hung up (for example, the user tapped Stop). Nothing to send.
		h.logger().Info("client disconnected", "device", deviceID)
	case errors.Is(err, context.DeadlineExceeded):
		_ = send(Event{Type: "error", Message: "The reply took too long. Please try again."})
	default:
		h.logger().Error("chat failed", "device", deviceID, "err", err)
		_ = send(Event{Type: "error", Message: userMessage(err)})
	}
}

func validate(req Request, l Limits) error {
	if len(req.Messages) == 0 {
		return errors.New("messages must not be empty")
	}
	if len(req.Messages) > l.MaxMessages {
		return fmt.Errorf("too many messages (max %d)", l.MaxMessages)
	}
	total := 0
	for i, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return fmt.Errorf("messages[%d].role must be \"user\" or \"assistant\"", i)
		}
		n := len([]rune(m.Content))
		if n == 0 {
			return fmt.Errorf("messages[%d].content must not be empty", i)
		}
		if n > l.MaxContentChars {
			return fmt.Errorf("messages[%d].content is too long (max %d characters)", i, l.MaxContentChars)
		}
		total += n
	}
	if total > l.MaxTotalChars {
		return fmt.Errorf("conversation is too long (max %d characters)", l.MaxTotalChars)
	}
	if req.Messages[0].Role != "user" {
		return errors.New("the first message must be from the user")
	}
	if req.Messages[len(req.Messages)-1].Role != "user" {
		return errors.New("the last message must be from the user")
	}
	return nil
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Event{Type: "error", Message: msg})
}
