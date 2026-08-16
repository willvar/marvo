package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

func registerUniversalProviders(registry *Registry) {
	registry.register(provider("webhook", "Webhook", categoryUniversal, "", []Field{
		sensitiveURLField("url", "请求地址", true),
		selectField("method", "请求方法", true, "POST", option("POST", "POST"), option("GET", "GET")),
		selectField("content_type", "请求体", true, "json", option("JSON", "json"), option("表单", "form"), option("自定义文本", "text")),
		{Key: "body_template", Label: "自定义请求体", Type: FieldTextarea},
		{Key: "headers", Label: "附加请求头（JSON）", Type: FieldTextarea, Placeholder: "{\"Authorization\": \"Bearer …\"}", Sensitive: true},
	}, registry.sendWebhook))
}

func (r *Registry) sendWebhook(ctx context.Context, config map[string]any, message Message) error {
	target := stringValue(config, "url")
	method := strings.ToUpper(stringValue(config, "method"))
	headers := map[string]string{}
	if rawHeaders := stringValue(config, "headers"); rawHeaders != "" {
		if err := json.Unmarshal([]byte(rawHeaders), &headers); err != nil {
			return &DeliveryError{Err: fmt.Errorf("附加请求头不是有效 JSON: %w", err), Permanent: true}
		}
		for key, value := range headers {
			if strings.TrimSpace(key) == "" || strings.ContainsAny(key+value, "\r\n") {
				return &DeliveryError{Err: fmt.Errorf("附加请求头无效"), Permanent: true}
			}
		}
	}
	payload := map[string]any{
		"delivery_id": message.DeliveryID,
		"activity": map[string]any{
			"id": message.ActivityID, "kind": message.Kind, "title": message.Title,
			"content": message.Content, "url": message.URL, "created_at": message.CreatedAt,
		},
	}
	if method == http.MethodGet {
		parsed, err := url.Parse(target)
		if err != nil {
			return &DeliveryError{Err: err, Permanent: true}
		}
		query := parsed.Query()
		query.Set("delivery_id", message.DeliveryID)
		query.Set("activity_id", message.ActivityID)
		query.Set("title", message.Title)
		query.Set("content", message.Content)
		query.Set("url", message.URL)
		parsed.RawQuery = query.Encode()
		return r.sendRequest(ctx, method, parsed.String(), headers, nil)
	}
	switch stringValue(config, "content_type") {
	case "form":
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		encoded, _ := json.Marshal(payload)
		_ = writer.WriteField("data", string(encoded))
		_ = writer.Close()
		headers["Content-Type"] = writer.FormDataContentType()
		return r.sendRequest(ctx, http.MethodPost, target, headers, &body)
	case "text":
		body := stringValue(config, "body_template")
		if body == "" {
			body = message.Text()
		}
		body = replaceMessageVariables(body, message)
		if headers["Content-Type"] == "" {
			headers["Content-Type"] = "text/plain; charset=utf-8"
		}
		return r.sendRequest(ctx, http.MethodPost, target, headers, strings.NewReader(body))
	default:
		return r.sendJSON(ctx, http.MethodPost, target, headers, payload)
	}
}

func replaceMessageVariables(template string, message Message) string {
	replacements := strings.NewReplacer(
		"{{delivery_id}}", message.DeliveryID,
		"{{activity_id}}", message.ActivityID,
		"{{kind}}", message.Kind,
		"{{title}}", message.Title,
		"{{content}}", message.Content,
		"{{url}}", message.URL,
		"{{created_at}}", message.CreatedAt.Format(timeRFC3339),
	)
	return replacements.Replace(template)
}

const timeRFC3339 = "2006-01-02T15:04:05Z07:00"
