package egress

import (
	"net/http"
	"reflect"
	"testing"
)

func TestNormalizeAllowedDomains(t *testing.T) {
	got, err := NormalizeAllowedDomains([]string{
		"API.Example.com",
		"https://api.example.com/v1",
		"gateway.example.com",
	})
	if err != nil {
		t.Fatalf("NormalizeAllowedDomains() error = %v", err)
	}
	want := []string{"api.example.com", "gateway.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAllowedDomains() = %#v, want %#v", got, want)
	}
}

func TestValidateAPIBaseURL(t *testing.T) {
	allowed := []string{"api.a.com", "api.b.com"}
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "allowed path", value: "https://api.a.com/custom/v4"},
		{name: "allowed 443", value: "https://api.b.com:443/v1"},
		{name: "http", value: "http://api.a.com/v1", wantErr: "HTTPS"},
		{name: "prefix spoof", value: "https://api.a.com.evil.com/v1", wantErr: "不在服务商允许列表"},
		{name: "userinfo spoof", value: "https://api.a.com@evil.com/v1", wantErr: "用户名或密码"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateAPIBaseURL(tc.value, allowed)
			if tc.wantErr != "" {
				if err == nil || !containsSubstring(err.Error(), tc.wantErr) {
					t.Fatalf("ValidateAPIBaseURL(%q) err = %v, want substring %q", tc.value, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateAPIBaseURL(%q) unexpected error: %v", tc.value, err)
			}
			if got == "" {
				t.Fatalf("ValidateAPIBaseURL(%q) returned empty string", tc.value)
			}
		})
	}
}

func TestBuildUpstreamEndpoint(t *testing.T) {
	tests := []struct {
		url      string
		protocol string
		want     string
	}{
		{"https://api.openai.com/v1", "openai", "https://api.openai.com/v1/chat/completions"},
		{"https://api.openai.com/v1/chat/completions", "openai", "https://api.openai.com/v1/chat/completions"},
		{"https://api.anthropic.com/v1", "anthropic", "https://api.anthropic.com/v1/messages"},
		{"https://api.anthropic.com/v1/messages", "anthropic", "https://api.anthropic.com/v1/messages"},
	}

	for _, tt := range tests {
		got, err := BuildUpstreamEndpoint(tt.url, tt.protocol)
		if err != nil {
			t.Fatalf("BuildUpstreamEndpoint(%q, %q) error: %v", tt.url, tt.protocol, err)
		}
		if got != tt.want {
			t.Errorf("BuildUpstreamEndpoint(%q, %q) = %q, want %q", tt.url, tt.protocol, got, tt.want)
		}
	}
}

func TestSanitizeHeaders(t *testing.T) {
	h := make(http.Header)
	h.Set("X-Forwarded-For", "1.2.3.4")
	h.Set("CF-Connecting-IP", "5.6.7.8")
	h.Set("X-Real-IP", "9.10.11.12")
	h.Set("User-Agent", "curl/7.64.1")

	SanitizeHeaders(h, "MyCustomAgent/1.0")

	if h.Get("X-Forwarded-For") != "" {
		t.Errorf("X-Forwarded-For was not stripped")
	}
	if h.Get("CF-Connecting-IP") != "" {
		t.Errorf("CF-Connecting-IP was not stripped")
	}
	if h.Get("User-Agent") != "MyCustomAgent/1.0" {
		t.Errorf("User-Agent was not sanitized, got: %s", h.Get("User-Agent"))
	}
}

func containsSubstring(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || searchSub(s, sub)))
}

func searchSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
