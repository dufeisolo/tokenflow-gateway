package stream

import (
	"bufio"
	"bytes"
	"context"
	"net/http"
	"testing"
)

func TestReadFirstStreamFrame(t *testing.T) {
	raw := "event: ping\ndata: {\"ok\":true}\n\nevent: message\ndata: next\n\n"
	reader := bufio.NewReader(bytes.NewBufferString(raw))

	frame, err := ReadFirstStreamFrame(reader)
	if err != nil {
		t.Fatalf("ReadFirstStreamFrame() unexpected error: %v", err)
	}
	expected := "event: ping\ndata: {\"ok\":true}\n\n"
	if string(frame) != expected {
		t.Errorf("ReadFirstStreamFrame() = %q, want %q", string(frame), expected)
	}
}

func TestStreamFrameHasError(t *testing.T) {
	tests := []struct {
		name     string
		frame    string
		hasError bool
	}{
		{
			name:     "valid frame",
			frame:    "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n",
			hasError: false,
		},
		{
			name:     "event error",
			frame:    "event: error\ndata: {\"message\":\"quota exceeded\"}\n\n",
			hasError: true,
		},
		{
			name:     "json error field",
			frame:    "data: {\"error\":{\"message\":\"invalid key\",\"code\":401}}\n\n",
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StreamFrameHasError([]byte(tt.frame))
			if got != tt.hasError {
				t.Errorf("StreamFrameHasError() = %v, want %v", got, tt.hasError)
			}
		})
	}
}

func TestResponseHasError(t *testing.T) {
	normal := []byte(`{"id":"chatcmpl-123","choices":[{"message":{"content":"Hi"}}]}`)
	if ResponseHasError(normal) {
		t.Errorf("ResponseHasError(normal) should be false")
	}

	errResp := []byte(`{"error":{"message":"Rate limit reached","type":"rate_limit_error"}}`)
	if !ResponseHasError(errResp) {
		t.Errorf("ResponseHasError(errResp) should be true")
	}
}

func TestStreamHasTerminalMarker(t *testing.T) {
	doneFrame := []byte("data: [DONE]\n\n")
	if !StreamHasTerminalMarker(doneFrame, "openai") {
		t.Errorf("StreamHasTerminalMarker(doneFrame) should be true")
	}

	anthropicStop := []byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	if !StreamHasTerminalMarker(anthropicStop, "anthropic") {
		t.Errorf("StreamHasTerminalMarker(anthropicStop) should be true")
	}

	normalFrame := []byte("data: {\"choices\":[{\"delta\":{\"content\":\"test\"}}]}\n\n")
	if StreamHasTerminalMarker(normalFrame, "openai") {
		t.Errorf("StreamHasTerminalMarker(normalFrame) should be false")
	}
}

func TestClassifyUpstreamError(t *testing.T) {
	canceled := context.Canceled
	status, state := ClassifyUpstreamError(canceled)
	if status != 499 || state != "FAILED" {
		t.Errorf("ClassifyUpstreamError(Canceled) = %d, %s, want 499, FAILED", status, state)
	}

	deadline := context.DeadlineExceeded
	status, state = ClassifyUpstreamError(deadline)
	if status != http.StatusGatewayTimeout || state != "TIMEOUT" {
		t.Errorf("ClassifyUpstreamError(DeadlineExceeded) = %d, %s, want 504, TIMEOUT", status, state)
	}
}
