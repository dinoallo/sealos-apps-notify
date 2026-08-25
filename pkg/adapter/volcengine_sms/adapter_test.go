package volcengine_sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/labring/sealos-notify/pkg/adapter"
)

func TestNewDefaultsAndValidation(t *testing.T) {
	a, err := New(map[string]interface{}{
		"accessKey":  "test-ak",
		"secretKey":  "test-sk",
		"smsAccount": "notify",
		"sign":       "Sealos",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if a.config.Endpoint != defaultEndpoint {
		t.Fatalf("Endpoint = %q, want %q", a.config.Endpoint, defaultEndpoint)
	}
	if a.config.Region != defaultRegion {
		t.Fatalf("Region = %q, want %q", a.config.Region, defaultRegion)
	}
	if a.config.Timeout != defaultTimeout {
		t.Fatalf("Timeout = %s, want %s", a.config.Timeout, defaultTimeout)
	}
	if err := a.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestNewRequiresRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
	}{
		{name: "missing access key", data: map[string]interface{}{"secretKey": "sk", "smsAccount": "notify", "sign": "Sealos"}},
		{name: "missing secret key", data: map[string]interface{}{"accessKey": "ak", "smsAccount": "notify", "sign": "Sealos"}},
		{name: "missing account", data: map[string]interface{}{"accessKey": "ak", "secretKey": "sk", "sign": "Sealos"}},
		{name: "missing sign", data: map[string]interface{}{"accessKey": "ak", "secretKey": "sk", "smsAccount": "notify"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.data); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSendUsesVolcengineSMSAPI(t *testing.T) {
	client := &fakeClient{body: `{"ResponseMetadata":{"RequestId":"req-1"},"Result":{"MessageID":["msg-1"]}}`, status: http.StatusOK}
	a := &Adapter{
		config: Config{
			Endpoint:   "https://sms.volcengineapi.com",
			Region:     "cn-north-1",
			AccessKey:  "test-ak",
			SecretKey:  "test-sk",
			SmsAccount: "notify",
			Sign:       "Sealos",
			Tag:        "notify-test",
			Timeout:    defaultTimeout,
		},
		client: client,
	}
	resp, err := a.Send(context.Background(), &adapter.SendRequest{
		RecipientValue: "+8613800000000",
		TemplateCode:   "ST_alert",
		Variables:      map[string]string{"name": "Alice", "severity": "P1"},
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("Send response = %#v", resp)
	}
	request := client.request
	if request == nil {
		t.Fatal("client did not receive request")
	}
	if request.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", request.Method)
	}
	if request.URL.Query().Get("Action") != "SendSms" {
		t.Fatalf("Action = %q, want SendSms", request.URL.Query().Get("Action"))
	}
	if request.URL.Query().Get("Version") != "2020-01-01" {
		t.Fatalf("Version = %q, want 2020-01-01", request.URL.Query().Get("Version"))
	}
	if request.Header.Get("Authorization") == "" {
		t.Fatal("missing Authorization header")
	}
	if request.Header.Get("X-Date") == "" {
		t.Fatal("missing X-Date header")
	}
	if !strings.Contains(request.Header.Get("Authorization"), "SignedHeaders=content-type;host;x-date") {
		t.Fatalf("unexpected signed headers: %q", request.Header.Get("Authorization"))
	}
	var received map[string]interface{}
	if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if received["SmsAccount"] != "notify" || received["Sign"] != "Sealos" {
		t.Fatalf("account/sign = %#v", received)
	}
	if received["TemplateID"] != "ST_alert" || received["PhoneNumbers"] != "+8613800000000" {
		t.Fatalf("template/phone = %#v", received)
	}
	if received["Tag"] != "notify-test" {
		t.Fatalf("tag = %#v", received["Tag"])
	}
	params, ok := received["TemplateParam"].(string)
	if !ok || !strings.Contains(params, `"name":"Alice"`) || !strings.Contains(params, `"severity":"P1"`) {
		t.Fatalf("TemplateParam = %#v", received["TemplateParam"])
	}
	messageIDs, ok := resp.Details["message_ids"].([]string)
	if !ok || len(messageIDs) != 1 || messageIDs[0] != "msg-1" {
		t.Fatalf("message_ids = %#v", resp.Details["message_ids"])
	}
}

func TestSendReturnsProviderError(t *testing.T) {
	a := &Adapter{
		config: Config{Endpoint: "https://sms.volcengineapi.com", Region: defaultRegion, AccessKey: "ak", SecretKey: "sk", SmsAccount: "notify", Sign: "Sealos"},
		client: &fakeClient{body: `{"ResponseMetadata":{"Error":{"Code":"InvalidParameter","Message":"invalid template"}}}`, status: http.StatusOK},
	}
	resp, err := a.Send(context.Background(), &adapter.SendRequest{
		RecipientValue: "13800000000",
		TemplateCode:   "ST_alert",
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || resp.Success || resp.Error == nil {
		t.Fatalf("Send response = %#v", resp)
	}
	if !strings.Contains(resp.Error.Error(), "InvalidParameter") {
		t.Fatalf("error = %v", resp.Error)
	}
}

func TestSendRejectsInvalidRequest(t *testing.T) {
	a := &Adapter{config: Config{}, client: &fakeClient{status: http.StatusOK}}
	for name, req := range map[string]*adapter.SendRequest{
		"nil request":      nil,
		"invalid phone":    {RecipientValue: "not-a-phone", TemplateCode: "ST_alert"},
		"missing template": {RecipientValue: "13800000000"},
	} {
		t.Run(name, func(t *testing.T) {
			resp, err := a.Send(context.Background(), req)
			if err != nil {
				t.Fatalf("Send returned error: %v", err)
			}
			if resp == nil || resp.Success || resp.Error == nil {
				t.Fatalf("Send response = %#v", resp)
			}
		})
	}
}

type fakeClient struct {
	body    string
	status  int
	err     error
	request *http.Request
}

func (f *fakeClient) Do(request *http.Request) (*http.Response, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.request = request
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
	}, nil
}
