package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"

	"github.com/tokenflow-ai/gateway/pkg/budget"
	"github.com/tokenflow-ai/gateway/pkg/egress"
	"github.com/tokenflow-ai/gateway/pkg/failover"
	"github.com/tokenflow-ai/gateway/pkg/stream"
)

type UpstreamKey struct {
	Key     string `yaml:"key"`
	BaseURL string `yaml:"base_url"`
}

type ModelConfig struct {
	UpstreamModel string        `yaml:"upstream_model"`
	Protocol      string        `yaml:"protocol"` // openai, anthropic, responses
	Keys          []UpstreamKey `yaml:"keys"`
}

type Config struct {
	Port   int                    `yaml:"port"`
	Models map[string]ModelConfig `yaml:"models"`
}

func printBanner() {
	banner := `
  _____     _              _____ _                 ____       _                         
 |_   _|___| | _____ _ __ |  ___| | _____      __ / ___| __ _| |_ _____      ____ _ _   _ 
   | | / _ \ |/ / _ \ '_ \| |_  | |/ _ \ \ /\ / /| |  _ / _` + "`" + ` | __/ _ \ \ /\ / / _` + "`" + ` | | | |
   | || (_) |   <  __/ | | |  _| | | (_) \ V  V / | |_| | (_| | ||  __/\ V  V / (_| | |_| |
   |_| \___/|_|\_\___|_| |_|_|   |_|\___/ \_/\_/   \____|\__,_|\__\___| \_/\_/ \__,_|\__, |
                                                                                      |___/ 
`
	fmt.Print(banner)
	fmt.Println("  ⚡ High-Performance Streaming LLM Gateway with Anti-Ban & In-Flight Budget Enforcer")
	fmt.Println("  🌐 Powered by TokenFlow (https://tokenflow.cool) - Global AI Token Exchange")
	fmt.Println("-------------------------------------------------------------------------------------")
}

func main() {
	printBanner()

	configFile := "config.yaml"
	if len(os.Args) > 1 {
		configFile = os.Args[1]
	}

	cfg := Config{Port: 8080, Models: make(map[string]ModelConfig)}
	if data, err := os.ReadFile(configFile); err == nil {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			log.Printf("[WARN] Failed to parse %s: %v, using default config", configFile, err)
		} else {
			log.Printf("[INFO] Loaded configuration from %s with %d models", configFile, len(cfg.Models))
		}
	} else {
		log.Printf("[INFO] %s not found, running in zero-config demo mode on port %d", configFile, cfg.Port)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"engine":  "tokenflow-gateway",
			"version": "v1.0.0",
			"powered": "TokenFlow (https://tokenflow.cool)",
		})
	})

	// GET /v1/models
	r.GET("/v1/models", func(c *gin.Context) {
		modelsList := make([]gin.H, 0, len(cfg.Models))
		for id, m := range cfg.Models {
			modelsList = append(modelsList, gin.H{
				"id":       id,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "tokenflow-gateway",
				"protocol": m.Protocol,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"object": "list",
			"data":   modelsList,
		})
	})

	// POST /v1/chat/completions
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		rawBody, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
			return
		}

		var req struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.Unmarshal(rawBody, &req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON format"})
			return
		}

		modelCfg, exists := cfg.Models[req.Model]
		if !exists || len(modelCfg.Keys) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Model '%s' not configured or no healthy keys available", req.Model)})
			return
		}

		// Pre-TTFT Failover across configured upstream keys
		var lastStatus int
		var lastErr error

		for i, k := range modelCfg.Keys {
			endpoint, err := egress.BuildUpstreamEndpoint(k.BaseURL, modelCfg.Protocol)
			if err != nil {
				continue
			}

			// Rewrite model name to upstream model
			var parsed map[string]interface{}
			_ = json.Unmarshal(rawBody, &parsed)
			if modelCfg.UpstreamModel != "" {
				parsed["model"] = modelCfg.UpstreamModel
			}
			outBody, _ := json.Marshal(parsed)

			upReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", endpoint, bytes.NewReader(outBody))
			if err != nil {
				continue
			}

			upReq.Header.Set("Content-Type", "application/json")
			upReq.Header.Set("Authorization", "Bearer "+k.Key)
			if modelCfg.Protocol == "anthropic" {
				upReq.Header.Set("x-api-key", k.Key)
				upReq.Header.Set("anthropic-version", "2023-06-01")
			}
			egress.SanitizeHeaders(upReq.Header, "TokenFlow-Gateway/1.0")

			client := &http.Client{Timeout: failover.DefaultAttemptTimeout}
			resp, err := client.Do(upReq)
			if err != nil {
				lastErr = err
				lastStatus, _ = stream.ClassifyUpstreamError(err)
				log.Printf("[WARN] Key %d failed with network error: %v, attempting next candidate", i+1, err)
				continue
			}

			if failover.RetryableUpstreamStatus(resp.StatusCode) {
				resp.Body.Close()
				lastStatus = resp.StatusCode
				log.Printf("[WARN] Key %d failed with status %d, triggering Pre-TTFT failover", i+1, resp.StatusCode)
				continue
			}

			// Forward response headers
			for k, v := range resp.Header {
				if strings.HasPrefix(strings.ToLower(k), "content-") {
					c.Writer.Header()[k] = v
				}
			}
			c.Writer.WriteHeader(resp.StatusCode)

			// Stream chunk copy with flush
			defer resp.Body.Close()
			buf := make([]byte, 32*1024)
			var totalUsage budget.CallUsage

			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					chunk := buf[:n]
					totalUsage.ObserveFrame(chunk)
					_, _ = c.Writer.Write(chunk)
					c.Writer.Flush()
				}
				if readErr != nil {
					break
				}
			}
			return
		}

		c.JSON(lastStatus, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("All upstream keys failed. Last error: %v", lastErr),
				"status":  lastStatus,
			},
		})
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: r,
	}

	go func() {
		log.Printf("[INFO] TokenFlow-Gateway listening on 0.0.0.0:%d", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	log.Println("[INFO] Gateway shut down gracefully")
}
