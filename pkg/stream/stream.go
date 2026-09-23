package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
)

// ReadFirstStreamFrame 首帧完整到达前不向下游发送响应头，若首帧检测到上游报错仍可以安全切换备用 Key。
func ReadFirstStreamFrame(reader *bufio.Reader) ([]byte, error) {
	var frame bytes.Buffer
	for frame.Len() < 1024*1024 {
		line, err := reader.ReadSlice('\n')
		frame.Write(line)
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			return nil, err
		}
		if bytes.Contains(frame.Bytes(), []byte("\n\n")) || bytes.Contains(frame.Bytes(), []byte("\r\n\r\n")) {
			return frame.Bytes(), nil
		}
	}
	return nil, errors.New("upstream stream frame exceeds limit")
}

// StreamFrameHasError 检查 SSE 首帧数据中是否包含错误信息。
func StreamFrameHasError(frame []byte) bool {
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.Equal(line, []byte("event: error")) {
			return true
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			var data map[string]interface{}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &data) == nil {
				if data["error"] != nil || data["type"] == "error" {
					return true
				}
			}
		}
	}
	return false
}

// ResponseHasError 检查非流式响应体是否返回了标准错误格式。
func ResponseHasError(body []byte) bool {
	var data map[string]json.RawMessage
	if json.Unmarshal(body, &data) != nil || data == nil {
		return true
	}
	if value, ok := data["error"]; ok && string(value) != "null" {
		return true
	}
	return string(data["type"]) == `"error"`
}

// StreamHasTerminalMarker 检查当前数据块是否包含 SSE 结束标志（如 data: [DONE]）。
func StreamHasTerminalMarker(frame []byte, upstreamProtocol string) bool {
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.Equal(line, []byte("data: [DONE]")) {
			return true
		}
		if bytes.Equal(line, []byte("event: response.done")) || bytes.Equal(line, []byte("event: response.completed")) {
			return true
		}
		if bytes.HasPrefix(line, []byte("data:")) {
			var event struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			}
			if json.Unmarshal(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))), &event) == nil {
				if event.Type == "message_stop" || event.Type == "response.done" || event.Type == "response.completed" {
					return true
				}
				if event.Status == "completed" {
					return true
				}
			}
		}
	}
	return false
}

// ClassifyUpstreamError 将底层网络错误归类为 HTTP 状态码与审计日志状态。
func ClassifyUpstreamError(err error) (int, string) {
	if errors.Is(err, context.Canceled) {
		return 499, "FAILED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "TIMEOUT"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return http.StatusGatewayTimeout, "TIMEOUT"
	}
	return http.StatusBadGateway, "UPSTREAM_ERROR"
}

// TruncateAuditMessage 限制审计日志长度，防止超大响应撑爆存储。
func TruncateAuditMessage(message string) string {
	message = strings.TrimSpace(message)
	const maxLength = 1000
	if len(message) > maxLength {
		return message[:maxLength]
	}
	return message
}
