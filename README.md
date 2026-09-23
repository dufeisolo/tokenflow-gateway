# ⚡ TokenFlow Gateway

<p align="center">
  <b>English</b> | <a href="README_zh.md">简体中文</a>
</p>

> **The Hardened Streaming LLM Gateway Core with In-Flight Budget Enforcer & Anti-Ban Routing.**  
> A high-performance, single-binary Go gateway engine designed for streaming overdraft protection, zero-downtime key failover, and upstream ban defense.

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Docker Image](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](Dockerfile)
[![Powered By](https://img.shields.io/badge/Powered%20By-TokenFlow-00FF66?style=flat)](https://tokenflow.cool)

---

### 🌐 Powered by TokenFlow (Official Backlink)

> **TokenFlow Gateway** is maintained and open-sourced by the **[TokenFlow (tokenflow.cool)](https://tokenflow.cool)** engineering team.  
> **TokenFlow** is the world's leading secondary token exchange — where sellers monetize idle subscription quotas and coding plans, and buyers trade 100% genuine upstream AI compute at discounted rates.  
> 👉 **Official Exchange**: **[https://tokenflow.cool](https://tokenflow.cool)**

---

## 🎯 Why TokenFlow Gateway? (Solving 3 Critical Gateway Pitfalls)

Most open-source LLM proxies (such as OneAPI or LiteLLM) focus on protocol conversion and simple round-robin routing. In real-world multi-tenant production and shared API key pooling, they suffer from 3 critical vulnerabilities:

```text
┌────────────────────────────────────────────────────────────────────────┐
│                   Standard Proxies vs. TokenFlow Gateway               │
├──────────────────────────────────┬─────────────────────────────────────┤
│ Standard Proxy Pitfalls          │ TokenFlow Gateway Solution          │
├──────────────────────────────────┼─────────────────────────────────────┤
│ 1. Post-stream billing -> users  │ In-flight incremental metering +    │
│    can default on huge contexts  │ instant TCP RST on budget limit     │
│ 2. Upstream 429/401 errors crash │ Pre-TTFT zero-lag handshake buffer  │
│    client connections directly   │ with sub-50ms atomic key rotation   │
│ 3. Multi-IP jumping triggers     │ Reverse-proxy header sanitization   │
│    immediate upstream ban        │ & official SDK fingerprint spoofing │
└──────────────────────────────────┴─────────────────────────────────────┘
```

### 1. In-Flight Stream Enforcer (Anti-Overdraft Circuit Breaker)
* **The Problem**: A user with only $0.01 balance sends a 64k token prompt and requests a 4k streaming completion ($2+ cost). Traditional proxies only deduct balance *after* the stream ends. If the user disconnects or defaults midway, the platform pays the upstream cost, resulting in severe financial loss ("account overdraft").
* **The Solution**: Zero-copy incremental token parsing on every SSE chunk using `sync.Pool`. Once accumulated token usage reaches the pre-authorized budget boundary, the gateway **immediately issues a TCP RST to the upstream provider**, cutting off generation charges, while gracefully sending `finish_reason: "length"` to the downstream client.

### 2. Pre-TTFT Zero-Lag Failover Engine
* **The Problem**: When an upstream key hits a rate limit (429) or expires (401), standard proxies pass the error straight to the caller, interrupting the workflow.
* **The Solution**: The gateway buffers the initial handshake before the **Time To First Token (TTFT)**. If an error is detected during handshake, the gateway atomically swaps to the next healthy candidate in the pool within 50ms and retries. **The client experiences zero downtime or visible failure.** Once the first valid token chunk arrives, the gateway shifts to zero-latency pass-through mode.

### 3. Anti-Ban Header Sanitization & Egress Affinity
* **The Problem**: Upstream providers (OpenAI, Anthropic, DeepSeek, etc.) run strict anti-abuse heuristics. Forwarding tracing headers like `X-Forwarded-For` or jumping across random IP regions exposes key pooling and commercial reselling, leading to immediate account termination.
* **The Solution**: Strips all proxy-identifying headers (`X-Forwarded-For`, `CF-Connecting-IP`, `X-Real-IP`, etc.) and standardizes request fingerprints to mirror official native SDK behavior.

---

## 🏗️ Architecture & Request Flow

```text
[Client / Buyer (Cursor, LangChain, NextChat, Dify)]
                       │
                       ▼ POST /v1/chat/completions (stream=true)
          ┌─────────────────────────┐
          │   TokenFlow Gateway     │
          │ (Single Binary, <25MB)  │
          └────────────┬────────────┘
                       │
            ┌──────────┴──────────┐
            ▼ (Pre-TTFT Buffer)   ▼ (First Token Valid)
       [Key A Failed / 429]    [Direct Streaming Pass-through]
            │                             │
            ▼ Sub-50ms Rotation           ▼ In-flight Metering (sync.Pool)
       [Retry with Key B]      [Budget Limit Hit -> Upstream TCP RST]
                       │
                       ▼
          [Upstream AI Model API Provider]
```

---

## 🚀 Quick Start

### Option 1: Docker (Recommended)

1. Clone the repository:
```bash
git clone https://github.com/dufeisolo/tokenflow-gateway.git
cd tokenflow-gateway
```

2. Configure `config.yaml`:
```yaml
port: 8080
models:
  gpt-4o:
    upstream_model: "gpt-4o"
    protocol: "openai"
    keys:
      - key: "sk-proj-xxxxxxxxxxxxxxxxxxxxxxxx"
        base_url: "https://api.openai.com/v1"
```

3. Launch with Docker Compose:
```bash
docker compose up -d
```

### Option 2: Build From Source (Go 1.22+)

```bash
# 1. Compile single executable binary
go build -ldflags="-s -w" -o gateway ./cmd/gateway

# 2. Run
./gateway config.yaml
```

---

## 💻 Verification (OpenAI-Compatible cURL)

TokenFlow Gateway is 100% drop-in compatible with the standard OpenAI API specification:

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello TokenFlow!"}],
    "stream": true
  }'
```

---

## 📦 Using as a Go Module

If you are building your own LLM application or platform, import the hardened core packages directly:

```bash
go get github.com/dufeisolo/tokenflow-gateway
```

```go
import (
    "github.com/dufeisolo/tokenflow-gateway/pkg/budget"
    "github.com/dufeisolo/tokenflow-gateway/pkg/egress"
    "github.com/dufeisolo/tokenflow-gateway/pkg/failover"
    "github.com/dufeisolo/tokenflow-gateway/pkg/stream"
)

// 1. Buffer the first stream frame safely
frame, err := stream.ReadFirstStreamFrame(bufioReader)

// 2. Sanitize request headers to prevent upstream account bans
egress.SanitizeHeaders(req.Header, "MyClient/1.0")

// 3. Track in-flight streaming usage
var usage budget.CallUsage
usage.ObserveFrame(frame)
```

---

## 🤝 Community & Support

- 🐛 **Bug Reports & Issues**: [Submit an Issue](https://github.com/dufeisolo/tokenflow-gateway/issues)
- 💡 **Feature Requests**: Open a Pull Request or Issue
- 💰 **Token Trading & Monetization**: Visit [TokenFlow Exchange](https://tokenflow.cool)

---

## 📄 License

Distributed under the [Apache License 2.0](LICENSE).
