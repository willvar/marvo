package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCatalogMatchesSupportedUptimeKumaHTTPAndSMTPProviders(t *testing.T) {
	want := strings.Fields(`
		360messenger 46elks alerta alertnow aliyun-sms bale bark bearsms bitrix24 brevo
		call-me-bot cellsynt clicksendsms clickup dingding discord egosms evolution feishu flashduty
		flowtriq fluxer freemobile goalert google-chat google-sheets gorush gotify grafana-oncall gtx-messaging
		halopsa heii-oncall home-assistant jira-service-management keep kook line lunasea matrix mattermost
		max milky nextcloudtalk notifery ntfy octopush onebot onechat onesender ooredoo
		openwa opsgenie pagerduty pagertree pinglet plivo promosms pumble pushbullet pushdeer
		pushover pushplus pushy resend rocket-chat send-grid serverchan serwersms sevenio signal
		signl4 slack sms-planet smsc smseagle smsir smsmanager smspartner smtp splunk
		spugpush squadcast stackfield teams techulus-push telegram telnyx teltonika threema turbosmtp
		twilio vk vkteams waha webhook wecom whapi wpush wxpusher yzj zoho-cliq
	`)
	registry := NewRegistry(nil)
	catalog := registry.Catalog()
	if len(catalog) != len(want) || len(catalog) != 101 {
		t.Fatalf("catalog size = %d, want %d", len(catalog), len(want))
	}
	actual := make(map[string]bool, len(catalog))
	for _, provider := range catalog {
		if actual[provider.ID] {
			t.Fatalf("duplicate provider %q", provider.ID)
		}
		actual[provider.ID] = true
		if provider.send != nil {
			t.Fatalf("catalog leaked sender for %q", provider.ID)
		}
	}
	for _, id := range want {
		if !actual[id] {
			t.Errorf("missing provider %q", id)
		}
	}
	for _, excluded := range []string{"apprise", "nostr", "webpush"} {
		if actual[excluded] {
			t.Errorf("unsupported provider %q was registered", excluded)
		}
	}
}

func TestRegistryValidatesURLsAndRejectsUnknownSettings(t *testing.T) {
	registry := NewRegistry(nil)
	if _, err := registry.Validate("webhook", map[string]any{"url": "file:///tmp/result", "method": "POST", "content_type": "json"}); err == nil {
		t.Fatal("non-HTTP URL was accepted")
	}
	if _, err := registry.Validate("webhook", map[string]any{"url": "https://example.test/hook", "method": "POST", "content_type": "json", "unknown": "value"}); err == nil {
		t.Fatal("unknown setting was accepted")
	}
}

func TestEveryHTTPProviderBuildsAndSendsANativeRequest(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"success":true,"errcode":0,"code":0,"StatusCode":0,"data":{"token":"test-token"}}`,
			)),
			Request: request,
		}, nil
	})}
	registry := NewRegistry(client)
	message := Message{
		DeliveryID: "delivery", ActivityID: "activity", Kind: "notice",
		Title: "测试活动", Content: "测试内容", URL: "https://marvo.example/user/test/activity?activity=activity",
		CreatedAt: time.Date(2026, 8, 16, 1, 2, 3, 0, time.UTC),
	}

	for _, provider := range registry.Catalog() {
		if provider.ID == "smtp" {
			continue
		}
		t.Run(provider.ID, func(t *testing.T) {
			before := requests
			config := make(map[string]any)
			for _, field := range provider.Fields {
				if field.Default != nil {
					config[field.Key] = field.Default
					continue
				}
				switch field.Type {
				case FieldURL:
					config[field.Key] = "https://connector.example.test/path"
				case FieldNumber:
					config[field.Key] = float64(1)
				case FieldBoolean:
					config[field.Key] = false
				case FieldTextarea:
					if field.Key == "headers" {
						config[field.Key] = `{}`
					} else {
						config[field.Key] = "test-value"
					}
				default:
					config[field.Key] = connectorTestText(field.Key)
				}
			}
			if err := registry.Send(context.Background(), provider.ID, config, message); err != nil {
				t.Fatalf("Send() failed: %v; config = %#v", err, config)
			}
			if requests == before {
				t.Fatal("provider returned without making an HTTP request")
			}
		})
	}
}

func TestPingletUsesItsNativeMessageProtocol(t *testing.T) {
	var captured *http.Request
	var payload map[string]any
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		captured = request
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: request,
		}, nil
	})}
	registry := NewRegistry(client)
	err := registry.Send(context.Background(), "pinglet", map[string]any{
		"publish_url": "https://pinglet.example.test/namespace/activity", "api_key": "secret",
	}, Message{ActivityID: "activity-id", Title: "研究完成", Content: "结果已整理", URL: "https://marvo.example/activity"})
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.URL.Query().Get("rewrite") != "" {
		t.Fatalf("Pinglet request unexpectedly uses an integration rewriter: %v", captured)
	}
	if payload["title"] != "研究完成" || payload["message"] != "结果已整理\n\nhttps://marvo.example/activity" {
		t.Fatalf("Pinglet payload = %#v", payload)
	}
	if _, exists := payload["monitor"]; exists {
		t.Fatalf("Pinglet payload leaked monitoring fields: %#v", payload)
	}
	if _, exists := payload["heartbeat"]; exists {
		t.Fatalf("Pinglet payload leaked monitoring fields: %#v", payload)
	}
}

func TestProviderErrorsCanBeRedactedBeforePersistence(t *testing.T) {
	registry := NewRegistry(nil)
	secretURL := "https://example.test/hooks/private-token"
	secretHeader := `{"Authorization":"Bearer private-value"}`
	message := registry.RedactError("webhook", map[string]any{
		"url": secretURL, "headers": secretHeader,
	}, "request to "+secretURL+" failed with "+secretHeader)
	if strings.Contains(message, "private-token") || strings.Contains(message, "private-value") {
		t.Fatalf("redacted error still exposes credentials: %q", message)
	}
}

func connectorTestText(key string) string {
	if strings.Contains(key, "email") || key == "to" || key == "from" || key == "cc" || key == "bcc" {
		return "test@example.test"
	}
	return "test-value"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
