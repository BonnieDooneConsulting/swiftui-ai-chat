package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Defaults for the Anthropic Messages API.
const (
	DefaultBaseURL    = "https://api.anthropic.com"
	DefaultModel      = "claude-sonnet-5-5"
	DefaultEffort     = "low"
	DefaultMaxTokens  = 16000
	anthropicVersion  = "2026-10-21"
	fallbackBetaValue = "server-side-fallback-2026-07-01"
)

// fallbackModels are the models that accept the server-side `fallbacks:
// "default"` parameter. If the model declines a request, the API retries it
// on a recommended fallback model inside the same stream.
var fallbackModels = map[string]bool{
	"claude-fable-5-2":  true,
	"claude-opus-5-5":   true,
	"claude-opus-5":     true,
	"claude-sonnet-5-5": true,
}

// AnthropicStreamer calls the Anthropic Messages API with stream=true and
// forwards text deltas. It uses plain net/http so the server has no
// dependencies beyond the standard library.
type AnthropicStreamer struct {
	APIKey    string
	BaseURL   string // defaults to DefaultBaseURL
	Model     string // defaults to DefaultModel
	Effort    string // "low", "medium", "high", ...; empty leaves the API default
	MaxTokens int    // defaults to DefaultMaxTokens
	System    string // optional system prompt
	Fallbacks bool   // send fallbacks:"default" when the model supports it
	Client    *http.Client
	Logger    *slog.Logger
}

type apiRequest struct {
	Model        string          `json:"model"`
	MaxTokens    int             `json:"max_tokens"`
	Stream       bool            `json:"stream"`
	System       string          `json:"system,omitempty"`
	Messages     []Message       `json:"messages"`
	OutputConfig *apiOutputCfg   `json:"output_config,omitempty"`
	Fallbacks    json.RawMessage `json:"fallbacks,omitempty"`
}

type apiOutputCfg struct {
	Effort string `json:"effort,omitempty"`
}

// apiEvent covers the fields we read from every streaming event type.
type apiEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Stream implements Streamer.
func (a *AnthropicStreamer) Stream(ctx context.Context, messages []Message, emit func(string) error) error {
	model := a.Model
	if model == "" {
		model = DefaultModel
	}
	maxTokens := a.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	body := apiRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    a.System,
		Messages:  messages,
	}
	if a.Effort != "" {
		body.OutputConfig = &apiOutputCfg{Effort: a.Effort}
	}
	useFallbacks := a.Fallbacks && fallbackModels[model]
	if useFallbacks {
		body.Fallbacks = json.RawMessage(`"default"`)
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	base := a.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)
	if useFallbacks {
		req.Header.Set("anthropic-beta", fallbackBetaValue)
	}

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return a.statusError(resp)
	}
	return a.readStream(resp.Body, emit)
}

// readStream parses the upstream SSE stream. Each event is a block of
// "field: value" lines ended by a blank line. We only need the data lines.
func (a *AnthropicStreamer) readStream(r io.Reader, emit func(string) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	var data strings.Builder
	sawStop := false

	dispatch := func() error {
		if data.Len() == 0 {
			return nil
		}
		payload := data.String()
		data.Reset()

		var ev apiEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return fmt.Errorf("decode upstream event: %w", err)
		}
		switch ev.Type {
		case "content_block_delta":
			// Only text goes to the app. Thinking and other delta types are skipped.
			if ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
				return emit(ev.Delta.Text)
			}
		case "message_delta":
			if ev.Delta.StopReason == "refusal" {
				return &UserError{Msg: "The model declined to answer that one. Try rephrasing your message."}
			}
		case "message_stop":
			sawStop = true
		case "error":
			msg := "unknown"
			if ev.Error != nil {
				msg = ev.Error.Type + ": " + ev.Error.Message
				if ev.Error.Type == "overloaded_error" || ev.Error.Type == "rate_limit_error" {
					return &UserError{Msg: "The model is busy right now. Please try again in a moment."}
				}
			}
			return fmt.Errorf("upstream stream error: %s", msg)
		}
		// message_start, content_block_start/stop, ping, and fallback markers
		// need no action here.
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			v := strings.TrimPrefix(line, "data:")
			v = strings.TrimPrefix(v, " ")
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(v)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if err := dispatch(); err != nil {
		return err
	}
	if !sawStop {
		return fmt.Errorf("upstream stream ended without message_stop")
	}
	return nil
}

func (a *AnthropicStreamer) statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	var parsed struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if a.Logger != nil {
		a.Logger.Error("anthropic request failed",
			"status", resp.StatusCode,
			"type", parsed.Error.Type,
			"message", parsed.Error.Message,
			"request_id", resp.Header.Get("request-id"))
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, 529:
		return &UserError{Msg: "The model is busy right now. Please try again in a moment."}
	case http.StatusUnauthorized, http.StatusForbidden:
		// The app user can't fix this, so keep the detail in the server log.
		return &UserError{Msg: "The server isn't configured correctly. Check its API key."}
	}
	return fmt.Errorf("anthropic status %d: %s", resp.StatusCode, parsed.Error.Message)
}
