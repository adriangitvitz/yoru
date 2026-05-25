package service

import (
	"fmt"
	"net/http"
)

// SSEWriter writes Server-Sent Events to an HTTP response.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter creates an SSEWriter and sets the appropriate headers.
// Returns nil if the ResponseWriter does not support flushing.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(200)
	return &SSEWriter{w: w, flusher: flusher}
}

// Data sends a data-only SSE event.
func (s *SSEWriter) Data(data string) {
	_, _ = fmt.Fprintf(s.w, "data: %s\n\n", data)
	s.flusher.Flush()
}

// Event sends a named SSE event.
func (s *SSEWriter) Event(event, data string) {
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, data)
	s.flusher.Flush()
}

// Done sends the [DONE] sentinel event (OpenAI streaming convention).
func (s *SSEWriter) Done() {
	_, _ = fmt.Fprintf(s.w, "data: [DONE]\n\n")
	s.flusher.Flush()
}
