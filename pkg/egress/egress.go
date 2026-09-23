package egress

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// NormalizeAllowedDomains 将管理员输入的域名或 HTTPS URL 规范化为精确主机名白名单。
func NormalizeAllowedDomains(inputs []string) ([]string, error) {
	seen := make(map[string]struct{})
	result := make([]string, 0, len(inputs))
	for _, input := range inputs {
		value := strings.TrimSpace(input)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "://") {
			value = "https://" + value
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("无效的允许域名: %s", input)
		}
		if parsed.User != nil {
			return nil, fmt.Errorf("允许域名不能包含用户名或密码: %s", input)
		}
		host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		if host == "localhost" || net.ParseIP(host) != nil {
			return nil, fmt.Errorf("允许域名必须是公开主机名，不能使用 IP 或 localhost: %s", input)
		}
		if parsed.Port() != "" && parsed.Port() != "443" {
			return nil, fmt.Errorf("允许域名只能使用 HTTPS 默认端口 443: %s", input)
		}
		if _, ok := seen[host]; !ok {
			seen[host] = struct{}{}
			result = append(result, host)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("至少需要配置一个允许域名")
	}
	sort.Strings(result)
	return result, nil
}

// ProviderAllowedDomains 计算服务商最终允许的域名列表，支持回退到基地址。
func ProviderAllowedDomains(domains []string, fallbackBaseURL string) ([]string, error) {
	nonEmpty := make([]string, 0, len(domains))
	for _, domain := range domains {
		if strings.TrimSpace(domain) != "" {
			nonEmpty = append(nonEmpty, domain)
		}
	}
	if len(nonEmpty) > 0 {
		return NormalizeAllowedDomains(nonEmpty)
	}
	return NormalizeAllowedDomains([]string{fallbackBaseURL})
}

// ValidateAPIBaseURL 校验 API 基地址并返回规范化的 HTTPS URL。
func ValidateAPIBaseURL(rawURL string, allowedDomains []string) (string, error) {
	value := strings.TrimSpace(rawURL)
	if value == "" {
		return "", errors.New("API 基地址不能为空")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return "", errors.New("API 基地址格式无效")
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("API 基地址只允许使用 HTTPS")
	}
	if parsed.User != nil {
		return "", errors.New("API 基地址不能包含用户名或密码")
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return "", errors.New("API 基地址只允许使用 HTTPS 默认端口 443")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("API 基地址不能包含查询参数或页面片段")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" || net.ParseIP(host) != nil {
		return "", errors.New("API 基地址不能使用 IP 或 localhost")
	}
	allowed := false
	for _, domain := range allowedDomains {
		if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(domain), "."), host) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("API 基地址域名 %s 不在服务商允许列表中", host)
	}
	parsed.Scheme = "https"
	parsed.Host = host
	return strings.TrimRight(parsed.String(), "/"), nil
}

// BuildUpstreamEndpoint 根据基地址和协议类型构建具体的上游接口 URL。
func BuildUpstreamEndpoint(apiBaseURL, protocol string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(apiBaseURL), "/")
	if base == "" {
		return "", errors.New("API 基地址不能为空")
	}
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "anthropic":
		if strings.HasSuffix(strings.ToLower(base), "/messages") {
			return base, nil
		}
		return base + "/messages", nil
	case "openai_responses", "responses":
		if strings.HasSuffix(strings.ToLower(base), "/responses") {
			return base, nil
		}
		return base + "/responses", nil
	case "openai", "":
		if strings.HasSuffix(strings.ToLower(base), "/chat/completions") {
			return base, nil
		}
		return base + "/chat/completions", nil
	default:
		return "", fmt.Errorf("暂不支持的 API 协议: %s", protocol)
	}
}

// RedirectPolicy 返回遵循域名白名单的上游重定向策略。
func RedirectPolicy(allowedDomains []string) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("上游重定向次数过多")
		}
		if _, err := ValidateAPIBaseURL(req.URL.String(), allowedDomains); err != nil {
			return fmt.Errorf("上游重定向已被白名单拦截: %w", err)
		}
		return nil
	}
}

// SanitizeHeaders 清洗反向代理特征头，模拟标准官方 SDK 指纹，降低上游风控封号风险。
func SanitizeHeaders(h http.Header, defaultUserAgent string) {
	// 剥离代理穿透特征头
	h.Del("X-Forwarded-For")
	h.Del("X-Forwarded-Host")
	h.Del("X-Forwarded-Proto")
	h.Del("X-Real-IP")
	h.Del("CF-Connecting-IP")
	h.Del("True-Client-IP")
	h.Del("X-Client-IP")

	// 若未指定 User-Agent，则填充标准官方 SDK 格式
	if h.Get("User-Agent") == "" || strings.Contains(strings.ToLower(h.Get("User-Agent")), "curl") {
		if defaultUserAgent != "" {
			h.Set("User-Agent", defaultUserAgent)
		} else {
			h.Set("User-Agent", "OpenAI/Node.js 4.50.0")
		}
	}
}
