// Package chat holds the HTTP handler, the rate limiter, and the model
// backends (the real Anthropic client and a mock) for the chat proxy.
package chat

import (
	"context"
	"errors"
)

// Message is one turn in the conversation, as sent by the app.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request is the body of POST /v1/chat.
type Request struct {
	Messages []Message `json:"messages"`
}

// Event is one Server-Sent Event sent back to the app.
type Event struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

// Streamer produces a reply for a conversation, calling emit once per text
// chunk, in order. It returns when the reply is complete or something fails.
type Streamer interface {
	Stream(ctx context.Context, messages []Message, emit func(text string) error) error
}

// UserError is an error whose message is safe to show to the app user.
// Anything else is logged on the server and replaced with a generic message.
type UserError struct {
	Msg string
}

func (e *UserError) Error() string { return e.Msg }

// userMessage returns the text we're willing to send to the client for err.
func userMessage(err error) string {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue.Msg
	}
	return "Something went wrong talking to the model. Please try again."
}
