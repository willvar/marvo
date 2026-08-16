package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func registerIncidentProviders(r *Registry) {
	definitions := []Provider{
		provider("halopsa", "Halo PSA", categoryIncident, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "用户名", false), secretField("password", "密码", false),
		}, r.incidentSender("halopsa")),
		provider("alerta", "Alerta", categoryIncident, "", []Field{
			urlField("api_url", "API 地址", true), secretField("api_key", "API Key", true), textField("environment", "Environment", true),
			textField("severity", "Severity", false),
		}, r.incidentSender("alerta")),
		provider("alertnow", "AlertNow", categoryIncident, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.incidentSender("alertnow")),
		provider("flashduty", "FlashDuty", categoryIncident, "", []Field{
			secretField("integration_key", "Integration Key 或完整地址", true),
			selectField("severity", "严重程度", false, "Info", option("信息", "Info"), option("警告", "Warning"), option("严重", "Critical")),
		}, r.incidentSender("flashduty")),
		provider("flowtriq", "Flowtriq", categoryIncident, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), secretField("api_key", "API Key", false),
		}, r.incidentSender("flowtriq")),
		provider("goalert", "GoAlert", categoryIncident, "", []Field{
			urlField("server_url", "服务地址", true), secretField("token", "Integration Token", true),
		}, r.incidentSender("goalert")),
		provider("grafana-oncall", "Grafana OnCall", categoryIncident, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.incidentSender("grafana-oncall")),
		provider("heii-oncall", "Heii On-Call", categoryIncident, "", []Field{
			secretField("api_key", "API Key", true), textField("trigger_id", "Trigger ID", true),
		}, r.incidentSender("heii-oncall")),
		provider("jira-service-management", "Jira Service Management", categoryIncident, "", []Field{
			textField("cloud_id", "Cloud ID", true), textField("email", "邮箱", true), secretField("api_token", "API Token", true),
			selectField("priority", "优先级", false, "P3", option("P1", "P1"), option("P2", "P2"), option("P3", "P3"), option("P4", "P4"), option("P5", "P5")),
		}, r.incidentSender("jira-service-management")),
		provider("keep", "Keep", categoryIncident, "", []Field{
			urlField("server_url", "服务地址", true), secretField("api_key", "API Key", true),
		}, r.incidentSender("keep")),
		provider("opsgenie", "Opsgenie", categoryIncident, "", []Field{
			secretField("api_key", "API Key", true), selectField("region", "区域", false, "us", option("美国", "us"), option("欧洲", "eu")),
			selectField("priority", "优先级", false, "P3", option("P1", "P1"), option("P2", "P2"), option("P3", "P3"), option("P4", "P4"), option("P5", "P5")),
		}, r.incidentSender("opsgenie")),
		provider("pagerduty", "PagerDuty", categoryIncident, "", []Field{
			urlField("integration_url", "Integration 地址", false), secretField("integration_key", "Routing Key", true),
			selectField("severity", "严重程度", false, "warning", option("Info", "info"), option("Warning", "warning"), option("Error", "error"), option("Critical", "critical")),
		}, r.incidentSender("pagerduty")),
		provider("pagertree", "PagerTree", categoryIncident, "", []Field{
			sensitiveURLField("integration_url", "Integration 地址", true), selectField("urgency", "紧急程度", false, "medium", option("低", "low"), option("中", "medium"), option("高", "high"), option("严重", "critical")),
		}, r.incidentSender("pagertree")),
		provider("signl4", "SIGNL4", categoryIncident, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.incidentSender("signl4")),
		provider("splunk", "Splunk On-Call", categoryIncident, "", []Field{
			sensitiveURLField("rest_url", "REST 地址", true), textField("severity", "严重程度", false),
		}, r.incidentSender("splunk")),
		provider("squadcast", "Squadcast", categoryIncident, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.incidentSender("squadcast")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) incidentSender(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		text := message.Content + linkSuffix(message.URL)
		switch id {
		case "halopsa":
			headers := map[string]string{}
			if stringValue(config, "username") != "" {
				headers["Authorization"] = basicAuth(stringValue(config, "username"), stringValue(config, "password"))
			}
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), headers, map[string]any{"title": message.Title, "status": "NOTIFICATION", "message": text, "timestamp": message.CreatedAt, "source": "Marvo", "activity_id": message.ActivityID})
		case "alerta":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "api_url"), map[string]string{"Authorization": "Key " + stringValue(config, "api_key")}, map[string]any{"environment": stringValue(config, "environment"), "severity": fallback(stringValue(config, "severity"), "informational"), "service": []string{"Marvo"}, "event": "activity", "text": text, "group": "marvo-activity", "resource": message.Title, "origin": "marvo", "type": "activity", "tags": []string{"marvo"}, "attributes": map[string]any{"activity_id": message.ActivityID}})
		case "alertnow":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"summary": message.Title + " - " + text, "status": "open", "event_type": "INFO", "event_id": message.ActivityID})
		case "flashduty":
			key := stringValue(config, "integration_key")
			target := key
			if !strings.HasPrefix(key, "http://") && !strings.HasPrefix(key, "https://") {
				target = "https://api.flashcat.cloud/event/push/alert/standard?integration_key=" + url.QueryEscape(key)
			}
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"description": text, "title": message.Title, "event_status": fallback(stringValue(config, "severity"), "Info"), "alert_key": message.ActivityID, "labels": map[string]any{"resource": message.URL, "check": message.Title}, "client": "Marvo", "client_url": message.URL})
		case "flowtriq":
			headers := optionalAPIKeyHeader(config, "api_key", "X-API-Key")
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), headers, map[string]any{"source": "marvo", "status": "info", "activity": map[string]any{"id": message.ActivityID, "kind": message.Kind, "title": message.Title, "content": message.Content, "url": message.URL, "created_at": message.CreatedAt}})
		case "goalert":
			target := strings.TrimRight(stringValue(config, "server_url"), "/") + "/api/v2/generic/incoming?token=" + url.QueryEscape(stringValue(config, "token"))
			return r.sendForm(ctx, http.MethodPost, target, nil, url.Values{"summary": {message.Text()}})
		case "grafana-oncall":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"title": message.Title, "message": text, "state": "alerting", "activity_id": message.ActivityID})
		case "heii-oncall":
			target := "https://heiioncall.com/triggers/" + url.PathEscape(stringValue(config, "trigger_id")) + "/alert"
			return r.sendJSON(ctx, http.MethodPost, target, bearerHeader(config, "api_key"), map[string]any{"msg": text, "title": message.Title, "url": message.URL, "activity_id": message.ActivityID})
		case "jira-service-management":
			target := "https://api.atlassian.com/jsm/ops/api/" + url.PathEscape(stringValue(config, "cloud_id")) + "/v1/alerts"
			headers := map[string]string{"Authorization": basicAuth(stringValue(config, "email"), stringValue(config, "api_token"))}
			return r.sendJSON(ctx, http.MethodPost, target, headers, map[string]any{"message": message.Title, "alias": "marvo-" + message.ActivityID, "description": text, "source": "Marvo", "priority": fallback(stringValue(config, "priority"), "P3"), "tags": []string{"Marvo"}})
		case "keep":
			target := strings.TrimRight(stringValue(config, "server_url"), "/") + "/alerts/event/marvo"
			return r.sendJSON(ctx, http.MethodPost, target, map[string]string{"x-api-key": stringValue(config, "api_key")}, map[string]any{"msg": text, "source": "marvo", "activity": map[string]any{"id": message.ActivityID, "kind": message.Kind, "title": message.Title, "url": message.URL}})
		case "opsgenie":
			target := "https://api.opsgenie.com/v2/alerts"
			if stringValue(config, "region") == "eu" {
				target = "https://api.eu.opsgenie.com/v2/alerts"
			}
			return r.sendJSON(ctx, http.MethodPost, target, map[string]string{"Authorization": "GenieKey " + stringValue(config, "api_key")}, map[string]any{"message": message.Title, "alias": "marvo-" + message.ActivityID, "description": text, "source": "Marvo", "priority": fallback(stringValue(config, "priority"), "P3")})
		case "pagerduty":
			target := fallback(stringValue(config, "integration_url"), "https://events.pagerduty.com/v2/enqueue")
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"payload": map[string]any{"summary": message.Title + ": " + message.Content, "severity": fallback(stringValue(config, "severity"), "warning"), "source": "Marvo", "custom_details": map[string]any{"activity_url": message.URL}}, "routing_key": stringValue(config, "integration_key"), "event_action": "trigger", "dedup_key": "marvo/" + message.ActivityID, "client": "Marvo", "client_url": message.URL})
		case "pagertree":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "integration_url"), nil, map[string]any{"event_type": "create", "id": message.ActivityID, "title": message.Title, "description": text, "urgency": fallback(stringValue(config, "urgency"), "medium"), "client": "Marvo", "client_url": message.URL})
		case "signl4":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"title": message.Title, "message": text, "X-S4-SourceSystem": "Marvo", "X-S4-ExternalID": "Marvo-" + message.ActivityID, "X-S4-Status": "new", "activity_url": message.URL})
		case "splunk":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "rest_url"), nil, map[string]any{"message_type": fallback(stringValue(config, "severity"), "INFO"), "state_message": "[" + message.Title + "] " + text, "entity_display_name": message.Title, "entity_id": "Marvo/" + message.ActivityID, "client": "Marvo", "client_url": message.URL})
		case "squadcast":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"message": message.Title, "description": text, "event_id": message.ActivityID, "status": "trigger", "source": "marvo", "tags": map[string]any{}})
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown incident provider %s", id), Permanent: true}
		}
	}
}
