package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testDevice = "test-device-0001"

// fakeUpstream serves a canned Anthropic-style SSE stream and records the
// request it received.
func fakeUpstream(t *testing.T, chunks []string, gotReq *apiRequest, gotHeaders *http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if gotHeaders != nil {
			*gotHeaders = r.Header.Clone()
		}
		if gotReq != nil {
			if err := json.NewDecoder(r.Body).Decode(gotReq); err != nil {
				t.Errorf("decode upstream request: %v", err)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		write := func(event, data string) {
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			f.Flush()
		}
		write("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[]}}`)
		write("ping", `{"type":"ping"}`)
		write("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`)
		write("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`)
		write("content_block_stop", `{"type":"content_block_stop","index":0}`)
		write("content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`)
		for _, c := range chunks {
			b, _ := json.Marshal(c)
			write("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":%s}}`, b))
		}
		write("content_block_stop", `{"type":"content_block_stop","index":1}`)
		write("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":12}}`)
		write("message_stop", `{"type":"message_stop"}`)
	}))
}

func newTestHandler(s Streamer, limiter *RateLimiter) *Handler {
	if limiter == nil {
		limiter = NewRateLimiter(600, 100)
	}
	return &Handler{Streamer: s, Limiter: limiter, Limits: DefaultLimits}
}

func postChat(t *testing.T, srv *httptest.Server, body, deviceID string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if deviceID != "" {
		req.Header.Set("X-Device-ID", deviceID)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readEvents(t *testing.T, r io.Reader) []Event {
	t.Helper()
	var events []Event
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatalf("bad event %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

const okBody = `{"messages":[{"role":"user","content":"Hello there"}]}`

func TestChatStreamsUpstreamDeltasInOrder(t *testing.T) {
	chunks := []string{"Hel", "lo", ", ", "wor", "ld", "!"}
	var gotReq apiRequest
	var gotHeaders http.Header
	up := fakeUpstream(t, chunks, &gotReq, &gotHeaders)
	defer up.Close()

	streamer := &AnthropicStreamer{APIKey: "sk-test", BaseURL: up.URL, Model: "claude-opus-5-5", Effort: "low", Fallbacks: true}
	srv := httptest.NewServer(newTestHandler(streamer, nil).Routes())
	defer srv.Close()

	resp := postChat(t, srv, okBody, testDevice)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	events := readEvents(t, resp.Body)
	if len(events) != len(chunks)+1 {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(chunks)+1, events)
	}
	for i, c := range chunks {
		if events[i].Type != "delta" || events[i].Text != c {
			t.Errorf("event %d = %+v, want delta %q", i, events[i], c)
		}
	}
	if last := events[len(events)-1]; last.Type != "done" {
		t.Errorf("last event = %+v, want done", last)
	}

	// The proxy, not the app, adds the key and model settings.
	if gotHeaders.Get("x-api-key") != "sk-test" {
		t.Errorf("x-api-key not forwarded")
	}
	if gotHeaders.Get("anthropic-version") == "" {
		t.Errorf("anthropic-version missing")
	}
	if gotHeaders.Get("anthropic-beta") != fallbackBetaValue {
		t.Errorf("anthropic-beta = %q", gotHeaders.Get("anthropic-beta"))
	}
	if !gotReq.Stream || gotReq.Model != "claude-opus-5-5" || len(gotReq.Messages) != 1 {
		t.Errorf("unexpected upstream request: %+v", gotReq)
	}
	if gotReq.OutputConfig == nil || gotReq.OutputConfig.Effort != "low" {
		t.Errorf("effort not sent: %+v", gotReq.OutputConfig)
	}
}

func TestNoFallbacksForUnsupportedModel(t *testing.T) {
	var gotReq apiRequest
	var gotHeaders http.Header
	up := fakeUpstream(t, []string{"ok"}, &gotReq, &gotHeaders)
	defer up.Close()

	streamer := &AnthropicStreamer{APIKey: "k", BaseURL: up.URL, Model: "claude-haiku-5-5", Fallbacks: true}
	srv := httptest.NewServer(newTestHandler(streamer, nil).Routes())
	defer srv.Close()

	resp := postChat(t, srv, okBody, testDevice)
	_ = readEvents(t, resp.Body)
	if gotHeaders.Get("anthropic-beta") != "" || len(gotReq.Fallbacks) != 0 {
		t.Errorf("fallbacks sent for a model that doesn't support them")
	}
}

func TestChatStreamsMockReply(t *testing.T) {
	srv := httptest.NewServer(newTestHandler(MockStreamer{Reply: "one two three"}, nil).Routes())
	defer srv.Close()

	events := readEvents(t, postChat(t, srv, okBody, testDevice).Body)
	var got strings.Builder
	for _, ev := range events[:len(events)-1] {
		if ev.Type != "delta" {
			t.Fatalf("unexpected event %+v", ev)
		}
		got.WriteString(ev.Text)
	}
	if got.String() != "one two three" {
		t.Errorf("reply = %q", got.String())
	}
	if events[len(events)-1].Type != "done" {
		t.Errorf("missing done event")
	}
}

func TestUpstreamErrorBecomesErrorEvent(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(529)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	}))
	defer up.Close()

	streamer := &AnthropicStreamer{APIKey: "k", BaseURL: up.URL}
	srv := httptest.NewServer(newTestHandler(streamer, nil).Routes())
	defer srv.Close()

	events := readEvents(t, postChat(t, srv, okBody, testDevice).Body)
	if len(events) != 1 || events[0].Type != "error" || !strings.Contains(events[0].Message, "busy") {
		t.Fatalf("events = %+v", events)
	}
}

func TestRefusalBecomesErrorEvent(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer up.Close()

	srv := httptest.NewServer(newTestHandler(&AnthropicStreamer{APIKey: "k", BaseURL: up.URL}, nil).Routes())
	defer srv.Close()

	events := readEvents(t, postChat(t, srv, okBody, testDevice).Body)
	if len(events) != 1 || events[0].Type != "error" || !strings.Contains(events[0].Message, "declined") {
		t.Fatalf("events = %+v", events)
	}
}

func TestRateLimitReturns429(t *testing.T) {
	limiter := NewRateLimiter(1, 2) // 2 right away, then 1 per minute
	srv := httptest.NewServer(newTestHandler(MockStreamer{Reply: "hi"}, limiter).Routes())
	defer srv.Close()

	for i := 0; i < 2; i++ {
		resp := postChat(t, srv, okBody, testDevice)
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200", i, resp.StatusCode)
		}
	}
	resp := postChat(t, srv, okBody, testDevice)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Errorf("missing Retry-After")
	}

	// A different device has its own bucket.
	other := postChat(t, srv, okBody, "another-device-01")
	if other.StatusCode != http.StatusOK {
		t.Errorf("other device status = %d, want 200", other.StatusCode)
	}
}

func TestRateLimiterRefills(t *testing.T) {
	l := NewRateLimiter(60, 1) // one per second
	now := time.Unix(1_000, 0)
	l.now = func() time.Time { return now }

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("first request should pass")
	}
	ok, wait := l.Allow("a")
	if ok || wait <= 0 {
		t.Fatalf("second request should be limited, got ok=%v wait=%v", ok, wait)
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow("a"); !ok {
		t.Fatal("request after refill should pass")
	}
}

func TestBadInputReturns400(t *testing.T) {
	srv := httptest.NewServer(newTestHandler(MockStreamer{Reply: "hi"}, nil).Routes())
	defer srv.Close()

	long := strings.Repeat("a", DefaultLimits.MaxContentChars+1)
	cases := map[string]string{
		"not json":       `hello`,
		"empty object":   `{}`,
		"no messages":    `{"messages":[]}`,
		"bad role":       `{"messages":[{"role":"system","content":"hi"}]}`,
		"empty content":  `{"messages":[{"role":"user","content":""}]}`,
		"ends with bot":  `{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"}]}`,
		"starts w/ bot":  `{"messages":[{"role":"assistant","content":"yo"},{"role":"user","content":"hi"}]}`,
		"unknown field":  `{"messages":[{"role":"user","content":"hi"}],"model":"x"}`,
		"too long":       `{"messages":[{"role":"user","content":"` + long + `"}]}`,
		"trailing types": `{"messages":"nope"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postChat(t, srv, body, testDevice)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
			var ev Event
			if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil || ev.Type != "error" || ev.Message == "" {
				t.Errorf("bad error body: %+v, %v", ev, err)
			}
		})
	}

	t.Run("missing device id", func(t *testing.T) {
		if resp := postChat(t, srv, okBody, ""); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
	t.Run("invalid device id", func(t *testing.T) {
		if resp := postChat(t, srv, okBody, "../../etc"); resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", resp.StatusCode)
		}
	})
}

func TestBodyTooLargeReturns413(t *testing.T) {
	h := newTestHandler(MockStreamer{Reply: "hi"}, nil)
	h.Limits.MaxBodyBytes = 100
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	body := `{"messages":[{"role":"user","content":"` + strings.Repeat("x", 500) + `"}]}`
	if resp := postChat(t, srv, body, testDevice); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(newTestHandler(MockStreamer{}, nil).Routes())
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestStreamTimeout(t *testing.T) {
	h := newTestHandler(MockStreamer{Reply: "a b c d e f", Delay: 50 * time.Millisecond}, nil)
	h.Limits.StreamTimeout = 75 * time.Millisecond
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	events := readEvents(t, postChat(t, srv, okBody, testDevice).Body)
	last := events[len(events)-1]
	if last.Type != "error" || !strings.Contains(last.Message, "too long") {
		t.Fatalf("last event = %+v, want timeout error", last)
	}
}

func TestMockStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := MockStreamer{Reply: "a b", Delay: time.Second}.Stream(ctx, nil, func(string) error { return nil })
	if err == nil {
		t.Fatal("expected an error after cancel")
	}
}
