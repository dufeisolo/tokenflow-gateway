package failover

import (
	"time"
)

const (
	// DefaultMaxAttempts 默认最大服务商/Key重试次数
	DefaultMaxAttempts = 3
	// DefaultAttemptTimeout 单次 Key 握手超时时间（首字前）
	DefaultAttemptTimeout = 60 * time.Second
	// DefaultFailoverBudget 总重试时间预算
	DefaultFailoverBudget = 180 * time.Second
	// DefaultCoolingPeriod 临时限流或故障冷却时长
	DefaultCoolingPeriod = 30 * time.Second
)

// UpstreamFailure 记录上游调用的失败特征
type UpstreamFailure struct {
	Status  int
	Message string
	Retry   bool
	Node    string
}

// RetryableUpstreamStatus 判断 HTTP 状态码是否属于可安全换 Key 重试的异常类型。
func RetryableUpstreamStatus(status int) bool {
	switch status {
	case 401, 403, 408, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

// KeyCandidate 代表一个候选 Key 凭据的调度元数据
type KeyCandidate struct {
	ID                 string
	ProviderID         string
	KeyHash            string
	Priority           int
	LastProbeLatencyMs int
	CoolingUntil       *time.Time
	TotalTokens        int64
	MaxTokensLimit     int64
}

// IsAvailable 判断候选 Key 是否脱离冷却且未超额
func (k *KeyCandidate) IsAvailable(now time.Time) bool {
	if k.CoolingUntil != nil && k.CoolingUntil.After(now) {
		return false
	}
	if k.MaxTokensLimit > 0 && k.TotalTokens >= k.MaxTokensLimit {
		return false
	}
	return true
}
