# ⚡ TokenFlow Gateway

<p align="center">
  <a href="README.md">English</a> | <b>简体中文</b>
</p>

> **专为大模型流式防穿仓、抗风控与零感知换 Key 设计的高性能 Go 单二进制网关内核。**  
> 具备逐 Chunk 内存零拷贝边传边算、首字前秒级无感轮转以及官方原生 SDK 指纹伪装能力。

[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat&logo=go)](https://golang.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Docker Image](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker)](Dockerfile)
[![Powered By](https://img.shields.io/badge/Powered%20By-TokenFlow-00FF66?style=flat)](https://tokenflow.cool)

---

### 🌐 Powered by TokenFlow (官方锚点)

> 本开源内核由 **[TokenFlow (tokenflow.cool)](https://tokenflow.cool)** 官方团队维护与开源。  
> **TokenFlow** 是全球领先的二手 Token 与 AI 算力交易平台 —— 卖家闲置额度变现回血，买家低价淘 100% 官方正品算力。  
> 👉 **官方交易市集**：**[https://tokenflow.cool](https://tokenflow.cool)**

---

## 🎯 为什么需要 TokenFlow Gateway？（解决开源代理的三大暗坑）

市面上的大多数大模型代理网关（如 OneAPI、LiteLLM 等）主要定位于协议转换和简单轮询，在**流式超大长文本**和**多 Key 共享池**场景中存在 3 个致命缺陷：

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        传统网关 vs TokenFlow Gateway                   │
├──────────────────────────────────┬─────────────────────────────────────┤
│ 传统网关暗坑                     │ TokenFlow Gateway 解决方案           │
├──────────────────────────────────┼─────────────────────────────────────┤
│ 1. 事后计费，流式长文本被白嫖穿仓│ 边传边算 + 触达红线即刻发 TCP RST   │
│ 2. 上游 Key 限流/失效直接报错返回│ 首字前拦截缓冲 (Pre-TTFT) 秒级换 Key│
│ 3. 频繁跨 IP 转发触发上游封号   │ 剥离代理特征头 + 原生 SDK 指纹模拟  │
└──────────────────────────────────┴─────────────────────────────────────┘
```

### 1. 流式 SSE 边传边算与防穿仓硬中断 (In-Flight Stream Enforcer)
* **痛点**：用户余额仅剩 $0.01，却发送 64k 的长代码让模型流式输出 4k Token（成本 $2+）。普通网关在流结束时才统一扣费，若用户中途断网或拔线，上游仍扣了平台的钱，导致平台被严重欠费穿仓。
* **解法**：逐 Chunk 内存零拷贝解析 Token 增量。一旦累计消耗逼近预设额度，网关**立即向上游大模型发送 TCP RST 硬断流**，停止上游计费，并向下游优雅返回完成标记。

### 2. 首字前零感知熔断换 Key (Pre-TTFT Zero-Lag Failover)
* **痛点**：上游 Key 偶发 429（限流）或 401（额度耗尽）。传统网关把报错直接抛给客户端，造成调用中断。
* **解法**：在首字生成（Time To First Token）之前保持握手缓冲。一旦握手阶段检测到上游报错，网关在 50ms 内无感轮转下一把健康 Key 重发，**调用方完全无感知**。只有在确认首字生成正常后，才切换到零延迟直通模式。

### 3. 反风控请求头清洗与出口亲和 (Anti-Ban Header Sanitization)
* **痛点**：代理层透传的 `X-Forwarded-For`、`CF-Connecting-IP` 暴露了多来源与代理特征，极易被 OpenAI、Anthropic、DeepSeek 等官方风控判定为凭证转售而导致封号。
* **解法**：物理清洗所有代理特征头，并标准化伪装为官方 SDK 的原生请求指纹。

---

## 🏗️ 架构流转原理

```text
【买家/客户端 (Cursor / LangChain / NextChat / Dify)】
                      │
                      ▼ POST /v1/chat/completions (stream=true)
         ┌─────────────────────────┐
         │   TokenFlow Gateway     │
         │  (单二进制 / 内存 <25MB)│
         └────────────┬────────────┘
                      │
           ┌──────────┴──────────┐
           ▼ (握手阶段 Pre-TTFT)  ▼ (首字正常)
      [Key A 挂了/429]      [零延迟直通透传]
           │                      │
           ▼ 秒级无感轮转          ▼ 边传边算 (sync.Pool 零拷贝)
      [换到 Key B 发起]     [若达预算红线 -> TCP RST 熔断上游]
                      │
                      ▼
         【上游厂商官方大模型 API】
```

---

## 🚀 快速上手 (Quick Start)

### 方式一：Docker 极速启动 (推荐)

1. 克隆代码并进入目录：
```bash
git clone https://github.com/dufeisolo/tokenflow-gateway.git
cd tokenflow-gateway
```

2. 准备配置文件 `config.yaml`：
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

3. 一键启动：
```bash
docker compose up -d
```

### 方式二：Go 本地编译运行 (Go 1.22+)

```bash
# 1. 编译极客单二进制
go build -ldflags="-s -w" -o gateway ./cmd/gateway

# 2. 启动服务
./gateway config.yaml
```

---

## 💻 接入验证 (CURL 测试)

标准 100% 兼容 OpenAI 格式：

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

## 📦 作为 Go 模块引入 (Go Package Usage)

如果您正在开发自己的 Go 大模型平台，可以直接引入本模块的核心包：

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

// 1. 首帧读取与安全缓冲
frame, err := stream.ReadFirstStreamFrame(bufioReader)

// 2. 请求头反风控清洗
egress.SanitizeHeaders(req.Header, "MyClient/1.0")

// 3. 统计流式 Usage
var usage budget.CallUsage
usage.ObserveFrame(frame)
```

---

## 🤝 社区与支持

- 🐛 **Bug 报告与新模型支持**：欢迎提交 [GitHub Issues](https://github.com/dufeisolo/tokenflow-gateway/issues)
- 💡 **功能建议**：欢迎开启 Pull Request 或 Issue
- 💰 **算力交易与闲置回血**：欢迎访问 [TokenFlow 交易平台](https://tokenflow.cool)

---

## 📄 开源许可证

本项目基于 [Apache License 2.0](LICENSE) 开源。
