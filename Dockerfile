# ==========================================
# TokenFlow-Gateway 独立轻量镜像构建 (< 25MB)
# ==========================================
FROM golang:1.24-alpine AS builder

WORKDIR /build
RUN apk add --no-cache ca-certificates git

COPY go.mod go.sum* ./
RUN go env -w GOPROXY=https://goproxy.cn,direct && go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway ./cmd/gateway

FROM alpine:3.20 AS runner
RUN apk --no-cache add ca-certificates tzdata
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=builder /gateway /app/gateway
COPY config.example.yaml /app/config.example.yaml

EXPOSE 8080

CMD ["/app/gateway", "/app/config.yaml"]
