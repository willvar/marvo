package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func registerOtherProviders(r *Registry) {
	definitions := []Provider{
		provider("google-sheets", "Google Sheets", categoryOther, "", []Field{
			sensitiveURLField("webhook_url", "Apps Script Webhook 地址", true),
		}, r.sendOther("google-sheets")),
		provider("home-assistant", "Home Assistant", categoryOther, "", []Field{
			urlField("server_url", "Home Assistant 地址", true), secretField("access_token", "长期访问令牌", true),
			textField("service", "通知服务", false),
		}, r.sendOther("home-assistant")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) sendOther(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		switch id {
		case "google-sheets":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{
				"deliveryId": message.DeliveryID,
				"activityId": message.ActivityID,
				"timestamp":  message.CreatedAt,
				"kind":       message.Kind,
				"title":      message.Title,
				"content":    message.Content,
				"url":        message.URL,
			})
		case "home-assistant":
			service := fallback(stringValue(config, "service"), "notify")
			if strings.ContainsAny(service, "/?#") {
				return &DeliveryError{Err: fmt.Errorf("通知服务名称无效：Home Assistant"), Permanent: true}
			}
			target, err := url.JoinPath(strings.TrimRight(stringValue(config, "server_url"), "/"), "api", "services", "notify", service)
			if err != nil {
				return &DeliveryError{Err: err, Permanent: true}
			}
			return r.sendJSON(ctx, http.MethodPost, target, bearerHeader(config, "access_token"), map[string]any{
				"title": message.Title, "message": message.Text(),
				"data": map[string]any{"activity_id": message.ActivityID, "url": message.URL, "channel": "Marvo"},
			})
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown provider %s", id), Permanent: true}
		}
	}
}
