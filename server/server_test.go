package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/labring/sealos-notify/pkg/adapter"
	"github.com/labring/sealos-notify/pkg/config"
	log "github.com/sirupsen/logrus"
)

func TestLoggingMiddlewareDebugLogsRawRequestAndPreservesBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := log.New()
	logger.SetLevel(log.DebugLevel)
	hook := &captureHook{}
	logger.AddHook(hook)

	s := &Server{logger: log.NewEntry(logger)}
	router := gin.New()
	router.Use(s.loggingMiddleware())
	router.POST("/echo", func(c *gin.Context) {
		var payload map[string]string
		if err := c.ShouldBindJSON(&payload); err != nil {
			t.Fatalf("handler could not bind restored body: %v", err)
		}
		c.JSON(http.StatusOK, payload)
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo?trace=1", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer app:secret")
	req.Header.Set("X-App-Secret", "secret")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if response["message"] != "hello" {
		t.Fatalf("response message = %q, want hello", response["message"])
	}

	entry := hook.entryByMessage("HTTP raw request")
	if entry == nil {
		t.Fatal("expected HTTP raw request debug log")
	}
	if entry.Data["body"] != `{"message":"hello"}` {
		t.Fatalf("debug body = %#v, want raw request body", entry.Data["body"])
	}
	if entry.Data["raw_query"] != "trace=1" {
		t.Fatalf("raw_query = %#v, want trace=1", entry.Data["raw_query"])
	}

	headers, ok := entry.Data["headers"].(http.Header)
	if !ok {
		t.Fatalf("headers field type = %T, want http.Header", entry.Data["headers"])
	}
	if headers.Get("Authorization") != "[REDACTED]" {
		t.Fatalf("Authorization header = %q, want redacted", headers.Get("Authorization"))
	}
	if headers.Get("X-App-Secret") != "[REDACTED]" {
		t.Fatalf("X-App-Secret header = %q, want redacted", headers.Get("X-App-Secret"))
	}
}

func TestLoggingMiddlewareSkipsRawRequestWhenDebugDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := log.New()
	logger.SetLevel(log.InfoLevel)
	hook := &captureHook{}
	logger.AddHook(hook)

	s := &Server{logger: log.NewEntry(logger)}
	router := gin.New()
	router.Use(s.loggingMiddleware())
	router.POST("/echo", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			t.Fatalf("handler could not read body: %v", err)
		}
		c.String(http.StatusOK, string(body))
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("raw-body"))
	router.ServeHTTP(recorder, req)

	if recorder.Body.String() != "raw-body" {
		t.Fatalf("response body = %q, want raw-body", recorder.Body.String())
	}
	if entry := hook.entryByMessage("HTTP raw request"); entry != nil {
		t.Fatal("did not expect raw request log when debug is disabled")
	}
	if entry := hook.entryByMessage("HTTP request"); entry == nil {
		t.Fatal("expected summary HTTP request log")
	}
}

func TestInitAdaptersIncludesFeishuWebhook(t *testing.T) {
	s := &Server{
		config: &config.GlobalConfig{
			Providers: map[string]config.ProviderConfig{
				"feishu-webhook-default": {
					Type: "feishu_webhook",
					Data: map[string]interface{}{"msgType": "interactive"},
				},
			},
		},
		logger: log.NewEntry(log.New()),
	}

	if err := s.initAdapters(); err != nil {
		t.Fatalf("initAdapters returned error: %v", err)
	}
	a, ok := s.adapters["feishu-webhook-default"]
	if !ok {
		t.Fatal("missing feishu webhook adapter")
	}
	if a.ChannelType() != adapter.ChannelTypeFeishuWebhook {
		t.Fatalf("ChannelType = %q, want %q", a.ChannelType(), adapter.ChannelTypeFeishuWebhook)
	}
}

func TestInitAdaptersIncludesSMTPEmail(t *testing.T) {
	s := &Server{
		config: &config.GlobalConfig{
			Providers: map[string]config.ProviderConfig{
				"smtp-default": {
					Type: "smtp",
					Data: map[string]interface{}{
						"host":     "smtp.example.com",
						"port":     25,
						"from":     "notify@example.com",
						"useTLS":   false,
						"fromName": "Sealos Notify",
					},
				},
			},
		},
		logger: log.NewEntry(log.New()),
	}

	if err := s.initAdapters(); err != nil {
		t.Fatalf("initAdapters returned error: %v", err)
	}
	a, ok := s.adapters["smtp-default"]
	if !ok {
		t.Fatal("missing SMTP email adapter")
	}
	if a.ChannelType() != adapter.ChannelTypeEmail {
		t.Fatalf("ChannelType = %q, want %q", a.ChannelType(), adapter.ChannelTypeEmail)
	}
}

type captureHook struct {
	entries []*log.Entry
}

func (h *captureHook) Levels() []log.Level {
	return log.AllLevels
}

func (h *captureHook) Fire(entry *log.Entry) error {
	data := log.Fields{}
	for key, value := range entry.Data {
		data[key] = value
	}
	h.entries = append(h.entries, &log.Entry{
		Logger:  entry.Logger,
		Data:    data,
		Time:    entry.Time,
		Level:   entry.Level,
		Caller:  entry.Caller,
		Message: entry.Message,
		Buffer:  bytes.NewBuffer(nil),
	})
	return nil
}

func (h *captureHook) entryByMessage(message string) *log.Entry {
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].Message == message {
			return h.entries[i]
		}
	}
	return nil
}
