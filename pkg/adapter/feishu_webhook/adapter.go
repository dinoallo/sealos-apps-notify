// Package feishu_webhook provides a Feishu custom bot webhook adapter.
package feishu_webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labring/sealos-notify/pkg/adapter"
)

const defaultTimeout = 10 * time.Second

// Config holds the configuration for the Feishu webhook adapter.
type Config struct {
	Webhook string
	Secret  string
	MsgType string
	Timeout time.Duration
}

// Adapter implements the Feishu custom bot webhook notification channel.
type Adapter struct {
	config Config
	client *http.Client
}

// New creates a new Feishu webhook adapter from provider data.
// Expected keys: webhook, secret, msgType, timeoutSeconds.
func New(data map[string]interface{}) (*Adapter, error) {
	cfg := Config{
		Webhook: getString(data, "webhook"),
		Secret:  getString(data, "secret"),
		MsgType: getString(data, "msgType"),
		Timeout: defaultTimeout,
	}
	if cfg.MsgType == "" {
		cfg.MsgType = "interactive"
	}
	if timeoutSeconds := getInt(data, "timeoutSeconds"); timeoutSeconds > 0 {
		cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	}

	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// Send posts the rendered message to a Feishu custom bot webhook. A per-request
// webhook in Metadata takes precedence over the provider-level webhook.
func (a *Adapter) Send(ctx context.Context, req *adapter.SendRequest) (*adapter.SendResponse, error) {
	webhook := a.resolveWebhook(req)
	if webhook == "" {
		err := fmt.Errorf("feishu_webhook: webhook is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	msgType := req.MsgType
	if msgType == "" {
		msgType = a.config.MsgType
	}
	if msgType == "" {
		msgType = "interactive"
	}

	payload, err := a.buildPayload(req.Body, msgType)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	if a.config.Secret != "" {
		a.signPayload(payload, time.Now().Unix())
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("marshal feishu webhook payload: %w", err)}, nil
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(body))
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("create feishu webhook request: %w", err)}, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("send feishu webhook request: %w", err)}, nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		err := fmt.Errorf("feishu webhook HTTP status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return &adapter.SendResponse{Success: false, Error: fmt.Errorf("decode feishu webhook response: %w", err)}, nil
		}
		if result.Code != 0 {
			return &adapter.SendResponse{
				Success: false,
				Error:   fmt.Errorf("feishu webhook error [%d]: %s", result.Code, result.Msg),
			}, nil
		}
	}

	return &adapter.SendResponse{
		Success: true,
		Details: map[string]interface{}{
			"status_code": resp.StatusCode,
			"msg_type":    msgType,
		},
	}, nil
}

func (a *Adapter) resolveWebhook(req *adapter.SendRequest) string {
	if req.Metadata != nil {
		for _, key := range []string{"webhook", "webhookURL", "webhook_url", "feishu_webhook"} {
			if v := strings.TrimSpace(req.Metadata[key]); v != "" {
				return v
			}
		}
	}
	if strings.HasPrefix(req.RecipientValue, "http://") || strings.HasPrefix(req.RecipientValue, "https://") {
		return req.RecipientValue
	}
	return a.config.Webhook
}

func (a *Adapter) buildPayload(body, msgType string) (map[string]interface{}, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, fmt.Errorf("feishu_webhook: message body is required")
	}

	switch msgType {
	case "text", "":
		return map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": body},
		}, nil
	case "post":
		var content interface{}
		if err := json.Unmarshal([]byte(body), &content); err != nil {
			return nil, fmt.Errorf("post message body must be valid JSON: %w", err)
		}
		return map[string]interface{}{
			"msg_type": "post",
			"content":  content,
		}, nil
	case "interactive":
		var card interface{}
		if err := json.Unmarshal([]byte(body), &card); err != nil {
			card = map[string]interface{}{
				"config": map[string]bool{"wide_screen_mode": true},
				"elements": []map[string]interface{}{
					{
						"tag":     "markdown",
						"content": body,
					},
				},
			}
		}
		return map[string]interface{}{
			"msg_type": "interactive",
			"card":     card,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported feishu webhook msgType %q", msgType)
	}
}

func (a *Adapter) signPayload(payload map[string]interface{}, timestamp int64) {
	payload["timestamp"] = strconv.FormatInt(timestamp, 10)
	payload["sign"] = sign(timestamp, a.config.Secret)
}

func sign(timestamp int64, secret string) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "feishu_webhook" }

// ChannelType returns the channel type.
func (a *Adapter) ChannelType() adapter.ChannelType { return adapter.ChannelTypeFeishuWebhook }

// Validate validates the adapter configuration.
func (a *Adapter) Validate() error { return nil }

func getString(data map[string]interface{}, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(data map[string]interface{}, key string) int {
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	default:
		return 0
	}
}
