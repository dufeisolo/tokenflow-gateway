package budget

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
)

// CallUsage 统一汇总并提取各大上游模型（OpenAI/Anthropic/Responses）返回的 Token 消耗计量
type CallUsage struct {
	Input     int
	Output    int
	HasInput  bool
	HasOutput bool
	Final     bool
	Invalid   bool
}

// Observe 从响应体 JSON 数据中解析提取 Usage 数据
func (u *CallUsage) Observe(body []byte) {
	var data map[string]interface{}
	if json.Unmarshal(body, &data) != nil {
		return
	}
	usage, _ := data["usage"].(map[string]interface{})
	if message, ok := data["message"].(map[string]interface{}); ok {
		if v, ok := message["usage"].(map[string]interface{}); ok {
			usage = v
		}
	}
	if response, ok := data["response"].(map[string]interface{}); ok {
		if v, ok := response["usage"].(map[string]interface{}); ok {
			usage = v
		}
	}
	if usage == nil {
		return
	}
	value := func(key string) (int, bool) {
		v, ok := usage[key].(float64)
		valid := ok && v >= 0 && v <= 1e9 && math.Trunc(v) == v
		if _, present := usage[key]; present && !valid {
			u.Invalid = true
		}
		if !valid {
			return 0, false
		}
		return int(v), true
	}
	if n, ok := value("prompt_tokens"); ok {
		u.Input = n
		u.HasInput = true
	}
	if n, ok := value("completion_tokens"); ok {
		u.Output = n
		u.HasOutput = true
		u.Final = true
	}
	if n, ok := value("input_tokens"); ok {
		for _, key := range []string{"cache_creation_input_tokens", "cache_read_input_tokens"} {
			if cached, ok := value(key); ok {
				n += cached
			}
		}
		u.Input = n
		u.HasInput = true
	}
	if n, ok := value("output_tokens"); ok {
		u.Output = n
		u.HasOutput = true
		if data["type"] != "message_start" {
			u.Final = true
		}
	}
	if data["type"] == "response.done" || data["type"] == "response.completed" || data["status"] == "completed" {
		u.Final = true
	}
}

// ObserveFrame 从流式 SSE 数据帧（含 data: 前缀）中解析提取 Usage 数据
func (u *CallUsage) ObserveFrame(frame []byte) {
	for _, line := range bytes.Split(frame, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			u.Observe(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:"))))
		}
	}
}

// Complete 校验是否已完整获取到终态的输入与输出用量
func (u CallUsage) Complete() bool {
	return u.HasInput && u.HasOutput && u.Final && !u.Invalid
}

// SanitizeAndEstimateBudget 检查请求参数边界、注入 include_usage，并计算预占资金预算。
func SanitizeAndEstimateBudget(raw []byte, protocol string, sellPriceInput, sellPriceOutput float64) ([]byte, float64, error) {
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, 0, err
	}
	if n, ok := body["n"]; ok && n != float64(1) {
		return nil, 0, errors.New("目前仅支持 n=1，以确保单次调用预算可控")
	}
	unsupportedKeys := []string{"audio", "modalities", "web_search_options"}
	if protocol != "openai_responses" && protocol != "responses" {
		unsupportedKeys = append(unsupportedKeys, "max_output_tokens")
	}
	for _, key := range unsupportedKeys {
		if _, ok := body[key]; ok {
			return nil, 0, errors.New("当前请求包含尚未支持独立预算的参数：" + key)
		}
	}
	var textOnly func(interface{}) bool
	textOnly = func(v interface{}) bool {
		switch x := v.(type) {
		case []interface{}:
			for _, item := range x {
				if !textOnly(item) {
					return false
				}
			}
		case map[string]interface{}:
			if kind, ok := x["type"].(string); ok {
				switch kind {
				case "image", "image_url", "input_image", "input_audio", "audio", "video", "video_url", "file", "document":
					return false
				}
			}
			for _, item := range x {
				if !textOnly(item) {
					return false
				}
			}
		}
		return true
	}
	if protocol == "openai_responses" || protocol == "responses" {
		if body["input"] == nil {
			return nil, 0, errors.New("缺少必须的输入参数 input")
		}
		if !textOnly(body["input"]) {
			return nil, 0, errors.New("当前资金预占仅支持文本和函数工具调用，暂不接受多模态输入")
		}
	} else {
		if !textOnly(body["messages"]) {
			return nil, 0, errors.New("当前资金预占仅支持文本和函数工具调用，暂不接受多模态输入")
		}
	}
	if tools, ok := body["tools"].([]interface{}); ok {
		for _, item := range tools {
			tool, ok := item.(map[string]interface{})
			if !ok {
				return nil, 0, errors.New("工具格式无效")
			}
			if kind, ok := tool["type"]; ok && kind != "function" && kind != "custom" {
				return nil, 0, errors.New("暂不支持有额外计费的服务端工具")
			}
		}
	}
	field := "max_tokens"
	if protocol == "openai_responses" || protocol == "responses" {
		field = "max_output_tokens"
		if _, ok := body["max_output_tokens"]; ok {
			field = "max_output_tokens"
		} else if _, ok := body["max_completion_tokens"]; ok {
			field = "max_completion_tokens"
		} else if _, ok := body["max_tokens"]; ok {
			field = "max_tokens"
		}
	} else if _, ok := body["max_completion_tokens"]; ok {
		if protocol == "anthropic" {
			return nil, 0, errors.New("该接口请使用 max_tokens")
		}
		if _, both := body["max_tokens"]; both {
			return nil, 0, errors.New("请只提供一个输出上限参数")
		}
		field = "max_completion_tokens"
	}
	limit := 4096.0
	if value, ok := body[field]; ok {
		var numeric bool
		limit, numeric = value.(float64)
		if !numeric || limit < 1 || limit > 131072 || math.Trunc(limit) != limit {
			return nil, 0, errors.New("输出上限必须是 1 至 131072 的整数")
		}
	}
	body[field] = int(limit)
	if stream, _ := body["stream"].(bool); stream && protocol != "anthropic" && protocol != "openai_responses" && protocol != "responses" {
		options, _ := body["stream_options"].(map[string]interface{})
		if options == nil {
			options = map[string]interface{}{}
		}
		options["include_usage"] = true
		body["stream_options"] = options
	}
	inputBudget := len(raw)*2 + 1024
	amount := math.Ceil((float64(inputBudget)*sellPriceInput+limit*sellPriceOutput)*100) / 1e8
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || amount >= 1e8 {
		return nil, 0, errors.New("调用预算超出支持范围")
	}
	updated, err := json.Marshal(body)
	return updated, amount, err
}
