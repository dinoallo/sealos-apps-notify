// Package aliyun_sms provides an Alibaba Cloud SMS adapter.
package aliyun_sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labring/sealos-notify/pkg/adapter"
)

const (
	defaultEndpoint = "dysmsapi.aliyuncs.com"
	defaultTimeout  = 10 * time.Second
	apiVersion      = "2017-05-25"
)

var phonePattern = regexp.MustCompile(`^\+?[0-9]{6,20}$`)

// Config holds Alibaba Cloud SMS adapter configuration.
type Config struct {
	Endpoint        string
	AccessKeyID     string
	AccessKeySecret string
	SignName        string
	OutID           string
	Timeout         time.Duration
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Adapter implements the SMS notification channel through Alibaba Cloud.
type Adapter struct {
	config Config
	client httpClient
}

// New creates an Alibaba Cloud SMS adapter from provider data.
// Expected keys: endpoint, accessKeyId, accessKeySecret, signName, outId,
// and timeoutSeconds.
func New(data map[string]interface{}) (*Adapter, error) {
	cfg := Config{
		Endpoint:        getString(data, "endpoint"),
		AccessKeyID:     firstNonEmpty(getString(data, "accessKeyId"), getString(data, "accessKey")),
		AccessKeySecret: firstNonEmpty(getString(data, "accessKeySecret"), getString(data, "secretKey")),
		SignName:        firstNonEmpty(getString(data, "signName"), getString(data, "sign")),
		OutID:           getString(data, "outId"),
		Timeout:         defaultTimeout,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint
	}
	if timeoutSeconds := getInt(data, "timeoutSeconds"); timeoutSeconds > 0 {
		cfg.Timeout = time.Duration(timeoutSeconds) * time.Second
	}

	a := &Adapter{config: cfg, client: &http.Client{Timeout: cfg.Timeout}}
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if _, err := endpointURL(cfg.Endpoint); err != nil {
		return nil, err
	}
	return a, nil
}

// Send sends one SMS using Alibaba's SendSms RPC API.
func (a *Adapter) Send(ctx context.Context, req *adapter.SendRequest) (*adapter.SendResponse, error) {
	if req == nil {
		err := fmt.Errorf("aliyun_sms: request is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	phoneNumber := strings.TrimSpace(req.RecipientValue)
	if !phonePattern.MatchString(phoneNumber) {
		err := fmt.Errorf("aliyun_sms: invalid phone number %q", req.RecipientValue)
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	if strings.TrimSpace(req.TemplateCode) == "" {
		err := fmt.Errorf("aliyun_sms: template code is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	variables := req.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	templateParam, err := json.Marshal(variables)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: marshal template parameters: %w", err)}, nil
	}

	params := url.Values{
		"AccessKeyId":      []string{a.config.AccessKeyID},
		"Action":           []string{"SendSms"},
		"Format":           []string{"JSON"},
		"PhoneNumbers":     []string{phoneNumber},
		"SignName":         []string{a.config.SignName},
		"SignatureMethod":  []string{"HMAC-SHA1"},
		"SignatureNonce":   []string{uuid.NewString()},
		"SignatureVersion": []string{"1.0"},
		"Timestamp":        []string{time.Now().UTC().Format("2006-01-02T15:04:05Z")},
		"TemplateCode":     []string{req.TemplateCode},
		"TemplateParam":    []string{string(templateParam)},
		"Version":          []string{apiVersion},
	}
	if a.config.OutID != "" {
		params.Set("OutId", a.config.OutID)
	}
	params.Set("Signature", sign(params, a.config.AccessKeySecret, http.MethodPost))

	endpoint, err := endpointURL(a.config.Endpoint)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewBufferString(params.Encode()))
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: create request: %w", err)}, nil
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: send request: %w", err)}, nil
	}
	if resp == nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: empty response")}, nil
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: read response: %w", readErr)}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: HTTP status %d", resp.StatusCode)}, nil
	}

	var response sendResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("aliyun_sms: decode response: %w", err)}, nil
	}
	if response.Code != "OK" {
		return &adapter.SendResponse{
			Success: false,
			Error:   fmt.Errorf("aliyun_sms: provider error [%s]: %s", response.Code, response.Message),
		}, nil
	}

	details := map[string]interface{}{
		"status_code": resp.StatusCode,
		"recipient":   phoneNumber,
	}
	if response.RequestID != "" {
		details["request_id"] = response.RequestID
	}
	if response.BizID != "" {
		details["biz_id"] = response.BizID
	}
	return &adapter.SendResponse{Success: true, Details: details}, nil
}

type sendResponse struct {
	RequestID string `json:"RequestId"`
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	BizID     string `json:"BizId"`
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "aliyun_sms" }

// ChannelType returns the SMS channel type.
func (a *Adapter) ChannelType() adapter.ChannelType { return adapter.ChannelTypeSMS }

// Validate validates the provider configuration.
func (a *Adapter) Validate() error {
	if strings.TrimSpace(a.config.Endpoint) == "" {
		return fmt.Errorf("aliyun_sms: endpoint is required")
	}
	if strings.TrimSpace(a.config.AccessKeyID) == "" {
		return fmt.Errorf("aliyun_sms: accessKeyId is required")
	}
	if strings.TrimSpace(a.config.AccessKeySecret) == "" {
		return fmt.Errorf("aliyun_sms: accessKeySecret is required")
	}
	if strings.TrimSpace(a.config.SignName) == "" {
		return fmt.Errorf("aliyun_sms: signName is required")
	}
	if a.config.Timeout <= 0 {
		return fmt.Errorf("aliyun_sms: timeout must be positive")
	}
	return nil
}

func endpointURL(endpoint string) (*url.URL, error) {
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
		return nil, fmt.Errorf("aliyun_sms: invalid endpoint %q", endpoint)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("aliyun_sms: unsupported endpoint scheme %q", parsed.Scheme)
	}
	parsed.Path = "/"
	return parsed, nil
}

func sign(params url.Values, accessKeySecret, method string) string {
	canonicalizedQuery := canonicalQuery(params)
	stringToSign := strings.Join([]string{
		strings.ToUpper(method),
		percentEncode("/"),
		percentEncode(canonicalizedQuery),
	}, "&")
	mac := hmac.New(sha1.New, []byte(accessKeySecret+"&"))
	_, _ = mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func canonicalQuery(params url.Values) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		values := append([]string(nil), params[key]...)
		sort.Strings(values)
		for _, value := range values {
			parts = append(parts, percentEncode(key)+"="+percentEncode(value))
		}
	}
	return strings.Join(parts, "&")
}

func percentEncode(value string) string {
	encoded := url.QueryEscape(value)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	return encoded
}

func getString(data map[string]interface{}, key string) string {
	if value, ok := data[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func getInt(data map[string]interface{}, key string) int {
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(value))
		return parsed
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
