package aliyun_sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/labring/sealos-notify/pkg/adapter"
)

func TestNewDefaultsAndValidation(t *testing.T) {
	a, err := New(map[string]interface{}{
		"accessKeyId":     "test-ak",
		"accessKeySecret": "test-sk",
		"signName":        "Sealos",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if a.config.Endpoint != defaultEndpoint {
		t.Fatalf("Endpoint = %q, want %q", a.config.Endpoint, defaultEndpoint)
	}
	if a.config.Timeout != defaultTimeout {
		t.Fatalf("Timeout = %s, want %s", a.config.Timeout, defaultTimeout)
	}
}

func TestNewRequiresRequiredFields(t *testing.T) {
	tests := []map[string]interface{}{
		{"accessKeySecret": "sk", "signName": "Sealos"},
		{"accessKeyId": "ak", "signName": "Sealos"},
		{"accessKeyId": "ak", "accessKeySecret": "sk"},
	}
	for _, data := range tests {
		if _, err := New(data); err == nil {
			t.Fatalf("New(%#v) expected error", data)
		}
	}
}

func TestSendUsesAliyunSMSAPI(t *testing.T) {
	client := &fakeClient{
		body:   `{"RequestId":"req-1","Code":"OK","Message":"OK","BizId":"biz-1"}`,
		status: http.StatusOK,
	}
	a := &Adapter{
		config: Config{
			Endpoint:        "https://dysmsapi.aliyuncs.com",
			AccessKeyID:     "test-ak",
			AccessKeySecret: "test-sk",
			SignName:        "Sealos",
			OutID:           "notify-1",
			Timeout:         defaultTimeout,
		},
		client: client,
	}

	resp, err := a.Send(context.Background(), &adapter.SendRequest{
		RecipientValue: "+8613800000000",
		TemplateCode:   "SMS_123",
		Variables:      map[string]string{"name": "Alice", "severity": "P1"},
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("Send response = %#v", resp)
	}
	if client.request == nil {
		t.Fatal("client did not receive request")
	}
	if client.request.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", client.request.Method)
	}
	if client.request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("content-type = %q", client.request.Header.Get("Content-Type"))
	}
	body, err := io.ReadAll(client.request.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	params, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("parse request body: %v", err)
	}
	for key, want := range map[string]string{
		"Action":       "SendSms",
		"Version":      apiVersion,
		"AccessKeyId":  "test-ak",
		"SignName":     "Sealos",
		"TemplateCode": "SMS_123",
		"PhoneNumbers": "+8613800000000",
		"OutId":        "notify-1",
	} {
		if got := params.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
	if params.Get("Signature") == "" {
		t.Fatal("missing Signature")
	}
	var templateParams map[string]string
	if err := json.Unmarshal([]byte(params.Get("TemplateParam")), &templateParams); err != nil {
		t.Fatalf("decode TemplateParam: %v", err)
	}
	if templateParams["name"] != "Alice" || templateParams["severity"] != "P1" {
		t.Fatalf("TemplateParam = %#v", templateParams)
	}
	unsignedParams := url.Values{}
	for key, values := range params {
		unsignedParams[key] = append([]string(nil), values...)
	}
	unsignedParams.Del("Signature")
	if got := params.Get("Signature"); got != sign(unsignedParams, "test-sk", http.MethodPost) {
		t.Fatalf("Signature = %q does not match request parameters", got)
	}
	if resp.Details["biz_id"] != "biz-1" {
		t.Fatalf("details = %#v", resp.Details)
	}
}

func TestSendReturnsProviderError(t *testing.T) {
	a := &Adapter{
		config: Config{Endpoint: "https://dysmsapi.aliyuncs.com", AccessKeyID: "ak", AccessKeySecret: "sk", SignName: "Sealos", Timeout: defaultTimeout},
		client: &fakeClient{body: `{"RequestId":"req-1","Code":"isv.INVALID_PARAMETERS","Message":"invalid template"}`, status: http.StatusOK},
	}
	resp, err := a.Send(context.Background(), &adapter.SendRequest{RecipientValue: "13800000000", TemplateCode: "SMS_123"})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || resp.Success || resp.Error == nil || !strings.Contains(resp.Error.Error(), "isv.INVALID_PARAMETERS") {
		t.Fatalf("Send response = %#v", resp)
	}
}

type fakeClient struct {
	body    string
	status  int
	request *http.Request
}

func (f *fakeClient) Do(request *http.Request) (*http.Response, error) {
	f.request = request
	return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader(f.body))}, nil
}
