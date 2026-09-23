package failover

import (
	"testing"
	"time"
)

func TestRetryableUpstreamStatus(t *testing.T) {
	retryable := []int{401, 403, 408, 429, 500, 502, 503, 504}
	for _, status := range retryable {
		if !RetryableUpstreamStatus(status) {
			t.Errorf("expected %d to be retryable", status)
		}
	}

	nonRetryable := []int{200, 400, 404, 422}
	for _, status := range nonRetryable {
		if RetryableUpstreamStatus(status) {
			t.Errorf("expected %d not to be retryable", status)
		}
	}
}

func TestKeyCandidateAvailability(t *testing.T) {
	now := time.Now()
	past := now.Add(-1 * time.Minute)
	future := now.Add(1 * time.Minute)

	k1 := KeyCandidate{ID: "k1", CoolingUntil: nil}
	if !k1.IsAvailable(now) {
		t.Errorf("k1 should be available")
	}

	k2 := KeyCandidate{ID: "k2", CoolingUntil: &past}
	if !k2.IsAvailable(now) {
		t.Errorf("k2 with past cooling should be available")
	}

	k3 := KeyCandidate{ID: "k3", CoolingUntil: &future}
	if k3.IsAvailable(now) {
		t.Errorf("k3 with future cooling should not be available")
	}

	k4 := KeyCandidate{ID: "k4", MaxTokensLimit: 1000, TotalTokens: 1000}
	if k4.IsAvailable(now) {
		t.Errorf("k4 with exceeded tokens should not be available")
	}
}
