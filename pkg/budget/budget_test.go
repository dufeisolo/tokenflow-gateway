package budget

import (
	"encoding/json"
	"testing"
)

func TestObserveUsageOpenAI(t *testing.T) {
	resp := []byte(`{
		"id": "chatcmpl-test",
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 25,
			"total_tokens": 40
		}
	}`)
	var u CallUsage
	u.Observe(resp)
	if !u.Complete() {
		t.Fatalf("usage should be complete")
	}
	if u.Input != 15 || u.Output != 25 {
		t.Errorf("u.Input = %d, u.Output = %d, want 15, 25", u.Input, u.Output)
	}
}

func TestObserveUsageAnthropic(t *testing.T) {
	resp := []byte(`{
		"type": "message",
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_creation_input_tokens": 10
		}
	}`)
	var u CallUsage
	u.Observe(resp)
	if !u.Complete() {
		t.Fatalf("anthropic usage should be complete")
	}
	if u.Input != 110 || u.Output != 50 {
		t.Errorf("u.Input = %d, u.Output = %d, want 110, 50", u.Input, u.Output)
	}
}

func TestSanitizeAndEstimateBudget(t *testing.T) {
	req := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	updated, amount, err := SanitizeAndEstimateBudget(req, "openai", 0.000005, 0.000015)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if amount <= 0 {
		t.Errorf("budget amount should be > 0, got %f", amount)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(updated, &parsed); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}
	if parsed["max_tokens"] != float64(4096) {
		t.Errorf("expected default max_tokens 4096, got %v", parsed["max_tokens"])
	}
}
