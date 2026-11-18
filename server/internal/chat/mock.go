package chat

import (
	"context"
	"strings"
	"time"
)

// DefaultMockReply is what MOCK=1 streams back.
const DefaultMockReply = "Hi! I'm the mock backend, so no API key was used for this reply. " +
	"Each word you see is arriving as its own Server-Sent Event, the same way a real " +
	"model reply would stream in. Set ANTHROPIC_API_KEY and drop MOCK=1 to talk to Claude."

// MockStreamer streams a canned reply word by word without calling any API.
type MockStreamer struct {
	Reply string
	Delay time.Duration
}

// Stream implements Streamer.
func (m MockStreamer) Stream(ctx context.Context, _ []Message, emit func(string) error) error {
	reply := m.Reply
	if reply == "" {
		reply = DefaultMockReply
	}
	words := strings.SplitAfter(reply, " ")
	for _, w := range words {
		if m.Delay > 0 {
			t := time.NewTimer(m.Delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return ctx.Err()
			case <-t.C:
			}
		}
		if err := emit(w); err != nil {
			return err
		}
	}
	return nil
}
