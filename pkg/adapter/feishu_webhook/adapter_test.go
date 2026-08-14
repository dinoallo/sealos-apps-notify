package feishu_webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labring/sealos-notify/pkg/adapter"
)

func TestBuildPayloadInteractiveJSONCard(t *testing.T) {
	a := &Adapter{}
	body := `{"config":{"wide_screen_mode":true},"elements":[{"tag":"markdown","content":"hello"}]}`

	got, err := a.buildPayload(body, "interactive")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	if got["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %#v, want interactive", got["msg_type"])
	}
	card, ok := got["card"].(json.RawMessage)
	if !ok {
		t.Fatalf("card type = %T, want json.RawMessage", got["card"])
	}
	var decodedCard struct {
		Elements []map[string]interface{} `json:"elements"`
	}
	if err := json.Unmarshal(card, &decodedCard); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if len(decodedCard.Elements) != 1 {
		t.Fatalf("card elements = %#v, want one element", decodedCard.Elements)
	}
}

func TestBuildPayloadInteractiveWrapsPlainTextAsMarkdownCard(t *testing.T) {
	a := &Adapter{}

	got, err := a.buildPayload("**firing**", "interactive")
	if err != nil {
		t.Fatalf("buildPayload returned error: %v", err)
	}
	card, ok := got["card"].(json.RawMessage)
	if !ok {
		t.Fatalf("card type = %T, want json.RawMessage", got["card"])
	}
	var decodedCard struct {
		Elements []struct {
			Content string `json:"content"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(card, &decodedCard); err != nil {
		t.Fatalf("decode card: %v", err)
	}
	if len(decodedCard.Elements) != 1 || decodedCard.Elements[0].Content != "**firing**" {
		t.Fatalf("wrapped content = %#v, want **firing**", decodedCard.Elements)
	}
}

func TestResolveWebhookPrefersMetadata(t *testing.T) {
	a := &Adapter{config: Config{Webhook: "https://provider.example/webhook"}}
	req := &adapter.SendRequest{
		RecipientValue: "placeholder",
		Metadata:       map[string]string{"webhook": "https://metadata.example/webhook"},
	}

	if got := a.resolveWebhook(req); got != "https://metadata.example/webhook" {
		t.Fatalf("resolveWebhook = %q", got)
	}
}

func TestSendPostsRenderedCardToWebhook(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer server.Close()

	a, err := New(map[string]interface{}{"msgType": "interactive"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	resp, err := a.Send(context.Background(), &adapter.SendRequest{
		Body:     `{"elements":[{"tag":"markdown","content":"alert"}]}`,
		MsgType:  "interactive",
		Metadata: map[string]string{"webhook": server.URL},
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("Send response = %#v", resp)
	}
	if received["msg_type"] != "interactive" {
		t.Fatalf("msg_type = %#v, want interactive", received["msg_type"])
	}
	if _, ok := received["card"]; !ok {
		t.Fatalf("request missing card: %#v", received)
	}
}
