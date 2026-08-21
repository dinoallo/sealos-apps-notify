// Package volcengine_sms provides a Volcengine SMS adapter.
package volcengine_sms

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/labring/sealos-notify/pkg/adapter"
)

const (
	defaultEndpoint = "sms.volcengineapi.com"
	defaultRegion   = "cn-north-1"
	defaultTimeout  = 10 * time.Second
	serviceName     = "volcSMS"
	apiVersion      = "2020-01-01"
)

var phonePattern = regexp.MustCompile(`^\+?[0-9]{6,20}$`)

// Config holds Volcengine SMS adapter configuration.
type Config struct {
	Endpoint   string
	Region     string
	AccessKey  string
	SecretKey  string
	SmsAccount string
	Sign       string
	Tag        string
	Timeout    time.Duration
}

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Adapter implements the SMS notification channel through Volcengine.
type Adapter struct {
	config Config
	client httpClient
}

// New creates a Volcengine SMS adapter from provider data.
// Expected keys: endpoint, region, accessKey, secretKey, smsAccount, sign,
// tag, and timeoutSeconds.
func New(data map[string]interface{}) (*Adapter, error) {
	cfg := Config{
		Endpoint:   getString(data, "endpoint"),
		Region:     getString(data, "region"),
		AccessKey:  firstNonEmpty(getString(data, "accessKey"), getString(data, "accessKeyId")),
		SecretKey:  firstNonEmpty(getString(data, "secretKey"), getString(data, "secretAccessKey")),
		SmsAccount: firstNonEmpty(getString(data, "smsAccount"), getString(data, "account")),
		Sign:       getString(data, "sign"),
		Tag:        getString(data, "tag"),
		Timeout:    defaultTimeout,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultEndpoint
	}
	if cfg.Region == "" {
		cfg.Region = defaultRegion
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

// Send sends one SMS using the provider template and rendered variables.
func (a *Adapter) Send(ctx context.Context, req *adapter.SendRequest) (*adapter.SendResponse, error) {
	if req == nil {
		err := fmt.Errorf("volcengine_sms: request is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	phoneNumber := strings.TrimSpace(req.RecipientValue)
	if !phonePattern.MatchString(phoneNumber) {
		err := fmt.Errorf("volcengine_sms: invalid phone number %q", req.RecipientValue)
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	if strings.TrimSpace(req.TemplateCode) == "" {
		err := fmt.Errorf("volcengine_sms: template code is required")
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}

	variables := req.Variables
	if variables == nil {
		variables = map[string]string{}
	}
	templateParam, err := json.Marshal(variables)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: marshal template parameters: %w", err)}, nil
	}
	payload, err := json.Marshal(map[string]string{
		"SmsAccount":    a.config.SmsAccount,
		"Sign":          a.config.Sign,
		"TemplateID":    req.TemplateCode,
		"TemplateParam": string(templateParam),
		"PhoneNumbers":  phoneNumber,
		"Tag":           a.config.Tag,
	})
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: marshal request: %w", err)}, nil
	}

	endpoint, err := endpointURL(a.config.Endpoint)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: err}, nil
	}
	query := url.Values{
		"Action":  []string{"SendSms"},
		"Version": []string{apiVersion},
	}
	endpoint.RawQuery = query.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: create request: %w", err)}, nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Host = endpoint.Host
	if err := signRequest(httpReq, payload, a.config.AccessKey, a.config.SecretKey, a.config.Region, serviceName, time.Now().UTC()); err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: sign request: %w", err)}, nil
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: send request: %w", err)}, nil
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: read response: %w", readErr)}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: HTTP status %d", resp.StatusCode)}, nil
	}

	var response smsResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return &adapter.SendResponse{Success: false, Error: fmt.Errorf("volcengine_sms: decode response: %w", err)}, nil
	}
	if providerErr := response.ResponseMetadata.Error; providerErr != nil {
		return &adapter.SendResponse{
			Success: false,
			Error:   fmt.Errorf("volcengine_sms: provider error [%s]: %s", providerErr.Code, providerErr.Message),
		}, nil
	}

	details := map[string]interface{}{
		"status_code": resp.StatusCode,
		"recipient":   phoneNumber,
	}
	if response.Result != nil && len(response.Result.MessageID) > 0 {
		details["message_ids"] = response.Result.MessageID
	}
	return &adapter.SendResponse{Success: true, Details: details}, nil
}

type smsResponse struct {
	ResponseMetadata struct {
		RequestID string         `json:"RequestId"`
		Error     *providerError `json:"Error,omitempty"`
	} `json:"ResponseMetadata"`
	Result *struct {
		MessageID []string `json:"MessageID"`
	} `json:"Result,omitempty"`
}

type providerError struct {
	Code    string `json:"Code"`
	CodeN   int    `json:"CodeN"`
	Message string `json:"Message"`
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return "volcengine_sms" }

// ChannelType returns the SMS channel type.
func (a *Adapter) ChannelType() adapter.ChannelType { return adapter.ChannelTypeSMS }

// Validate validates the provider configuration.
func (a *Adapter) Validate() error {
	if strings.TrimSpace(a.config.Endpoint) == "" {
		return fmt.Errorf("volcengine_sms: endpoint is required")
	}
	if strings.TrimSpace(a.config.Region) == "" {
		return fmt.Errorf("volcengine_sms: region is required")
	}
	if strings.TrimSpace(a.config.AccessKey) == "" {
		return fmt.Errorf("volcengine_sms: accessKey is required")
	}
	if strings.TrimSpace(a.config.SecretKey) == "" {
		return fmt.Errorf("volcengine_sms: secretKey is required")
	}
	if strings.TrimSpace(a.config.SmsAccount) == "" {
		return fmt.Errorf("volcengine_sms: smsAccount is required")
	}
	if strings.TrimSpace(a.config.Sign) == "" {
		return fmt.Errorf("volcengine_sms: sign is required")
	}
	if a.config.Timeout <= 0 {
		return fmt.Errorf("volcengine_sms: timeout must be positive")
	}
	return nil
}

func endpointURL(endpoint string) (*url.URL, error) {
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" {
		return nil, fmt.Errorf("volcengine_sms: invalid endpoint %q", endpoint)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("volcengine_sms: unsupported endpoint scheme %q", parsed.Scheme)
	}
	parsed.Path = "/"
	return parsed, nil
}

func signRequest(req *http.Request, payload []byte, accessKey, secretKey, region, service string, now time.Time) error {
	if req == nil || req.URL == nil {
		return fmt.Errorf("request URL is required")
	}
	date := now.UTC().Format("20060102T150405Z")
	dateOnly := date[:8]
	bodyHash := sha256Hex(payload)
	req.Header.Set("X-Date", date)
	req.Header.Set("X-Content-Sha256", bodyHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	canonicalHeaders := map[string]string{
		"content-type": strings.TrimSpace(req.Header.Get("Content-Type")),
		"host":         canonicalHost(host),
		"x-date":       date,
	}
	keys := make([]string, 0, len(canonicalHeaders))
	for key := range canonicalHeaders {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var headerBuilder strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&headerBuilder, "%s:%s\n", key, canonicalHeaders[key])
	}
	signedHeaders := strings.Join(keys, ";")
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		headerBuilder.String(),
		signedHeaders,
		bodyHash,
	}, "\n")

	algorithm := "HMAC-SHA256"
	scope := strings.Join([]string{dateOnly, region, service, "request"}, "/")
	stringToSign := strings.Join([]string{
		algorithm,
		date,
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256([]byte(secretKey), dateOnly), region), service), "request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s", algorithm, accessKey, scope, signedHeaders, signature))
	return nil
}

func canonicalHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasSuffix(host, ":80") || strings.HasSuffix(host, ":443") {
		return host[:strings.LastIndex(host, ":")]
	}
	return host
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func canonicalQuery(query url.Values) string {
	return strings.ReplaceAll(query.Encode(), "+", "%20")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
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
