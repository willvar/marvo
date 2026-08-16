package connectors

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func registerPushProviders(r *Registry) {
	definitions := []Provider{
		provider("bark", "Bark", categoryPush, "", []Field{
			sensitiveURLField("endpoint", "推送地址", true), textField("group", "分组", false), textField("sound", "提示音", false),
		}, r.pushSender("bark")),
		provider("gorush", "Gorush", categoryPush, "", []Field{
			urlField("server_url", "服务地址", true), sensitiveTextField("device_token", "设备 Token", true),
			selectField("platform", "平台", true, "android", option("Android", "android"), option("iOS", "ios"), option("Huawei", "huawei")),
			textField("topic", "Topic", false), numberField("priority", "优先级", false, 0), numberField("retry", "重试次数", false, 0),
		}, r.pushSender("gorush")),
		provider("gotify", "Gotify", categoryPush, "", []Field{
			urlField("server_url", "服务地址", true), secretField("application_token", "Application Token", true), numberField("priority", "优先级", false, 8),
		}, r.pushSender("gotify")),
		provider("lunasea", "LunaSea", categoryPush, "", []Field{
			selectField("target_type", "目标类型", true, "device", option("设备", "device"), option("用户", "user")), sensitiveTextField("target", "目标 ID", true),
		}, r.pushSender("lunasea")),
		provider("notifery", "Notifery", categoryPush, "", []Field{
			secretField("api_key", "API Key", true), textField("group", "分组", false),
		}, r.pushSender("notifery")),
		provider("ntfy", "ntfy", categoryPush, "", []Field{
			urlField("server_url", "服务地址", true), textField("topic", "Topic", true), numberField("priority", "优先级", false, 3),
			selectField("auth_type", "认证方式", false, "none", option("无", "none"), option("用户名和密码", "basic"), option("Access Token", "token")),
			textField("username", "用户名", false), secretField("password", "密码", false), secretField("access_token", "Access Token", false),
		}, r.pushSender("ntfy")),
		provider("pinglet", "Pinglet", categoryPush, "", []Field{
			sensitiveURLField("publish_url", "Publish URL", true), secretField("api_key", "API Key", true),
		}, r.pushSender("pinglet")),
		provider("pushbullet", "Pushbullet", categoryPush, "", []Field{secretField("access_token", "Access Token", true)}, r.pushSender("pushbullet")),
		provider("pushdeer", "PushDeer", categoryPush, "", []Field{
			secretField("push_key", "Push Key", true), urlField("server_url", "服务地址", false),
		}, r.pushSender("pushdeer")),
		provider("pushover", "Pushover", categoryPush, "", []Field{
			secretField("app_token", "Application Token", true), secretField("user_key", "User Key", true), textField("device", "设备", false),
			textField("sound", "提示音", false), numberField("priority", "优先级", false, 0), numberField("ttl", "TTL", false, 0),
		}, r.pushSender("pushover")),
		provider("pushplus", "PushPlus", categoryPush, "", []Field{secretField("send_key", "Send Key", true)}, r.pushSender("pushplus")),
		provider("pushy", "Pushy", categoryPush, "", []Field{
			secretField("api_key", "API Key", true), secretField("device_token", "Device Token", true),
		}, r.pushSender("pushy")),
		provider("serverchan", "Server酱", categoryPush, "", []Field{secretField("send_key", "SendKey", true)}, r.pushSender("serverchan")),
		provider("spugpush", "SpugPush", categoryPush, "", []Field{secretField("template_key", "模板 Key", true)}, r.pushSender("spugpush")),
		provider("techulus-push", "Push by Techulus", categoryPush, "", []Field{
			secretField("api_key", "API Key", true), textField("channel", "频道", false), textField("sound", "提示音", false), booleanField("time_sensitive", "时效性通知", true),
		}, r.pushSender("techulus-push")),
		provider("wpush", "WPush", categoryPush, "", []Field{
			secretField("api_key", "API Key", true), textField("channel", "通道", true),
		}, r.pushSender("wpush")),
		provider("wxpusher", "WxPusher", categoryPush, "", []Field{sensitiveTextField("spt", "SPT（多个用逗号分隔）", true)}, r.pushSender("wxpusher")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) pushSender(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		switch id {
		case "bark":
			return r.sendJSON(ctx, http.MethodPost, strings.TrimRight(stringValue(config, "endpoint"), "/"), nil, map[string]any{"title": message.Title, "body": message.Content + linkSuffix(message.URL), "group": fallback(stringValue(config, "group"), "Marvo"), "sound": fallback(stringValue(config, "sound"), "telegraph"), "url": message.URL})
		case "gorush":
			platform := map[string]int{"ios": 1, "android": 2, "huawei": 3}[stringValue(config, "platform")]
			return r.sendJSON(ctx, http.MethodPost, strings.TrimRight(stringValue(config, "server_url"), "/")+"/api/push", nil, map[string]any{"notifications": []any{map[string]any{"tokens": []string{stringValue(config, "device_token")}, "platform": platform, "message": message.Content + linkSuffix(message.URL), "title": message.Title, "priority": intValue(config, "priority", 0), "retry": intValue(config, "retry", 0), "topic": stringValue(config, "topic")}}})
		case "gotify":
			target := strings.TrimRight(stringValue(config, "server_url"), "/") + "/message?token=" + url.QueryEscape(stringValue(config, "application_token"))
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"message": message.Content + linkSuffix(message.URL), "priority": intValue(config, "priority", 8), "title": message.Title})
		case "lunasea":
			target := "https://notify.lunasea.app/v1/custom/" + stringValue(config, "target_type") + "/" + url.PathEscape(stringValue(config, "target"))
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"title": message.Title, "body": message.Content + linkSuffix(message.URL)})
		case "notifery":
			payload := map[string]any{"title": message.Title, "message": message.Content + linkSuffix(message.URL)}
			if group := stringValue(config, "group"); group != "" {
				payload["group"] = group
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api.notifery.com/event", map[string]string{"x-api-key": stringValue(config, "api_key")}, payload)
		case "ntfy":
			headers := map[string]string{}
			switch stringValue(config, "auth_type") {
			case "basic":
				headers["Authorization"] = basicAuth(stringValue(config, "username"), stringValue(config, "password"))
			case "token":
				headers["Authorization"] = "Bearer " + stringValue(config, "access_token")
			}
			payload := map[string]any{"topic": stringValue(config, "topic"), "title": message.Title, "message": message.Content, "priority": intValue(config, "priority", 3)}
			if message.URL != "" {
				payload["actions"] = []any{map[string]any{"action": "view", "label": "打开活动", "url": message.URL}}
			}
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "server_url"), headers, payload)
		case "pinglet":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "publish_url"), bearerHeader(config, "api_key"), map[string]any{
				"title": message.Title, "message": message.Content + linkSuffix(message.URL),
				"priority": "normal", "level": "info", "data": map[string]any{"activity_id": message.ActivityID},
			})
		case "pushbullet":
			return r.sendJSON(ctx, http.MethodPost, "https://api.pushbullet.com/v2/pushes", map[string]string{"Access-Token": stringValue(config, "access_token")}, map[string]any{"type": "note", "title": message.Title, "body": message.Content + linkSuffix(message.URL)})
		case "pushdeer":
			target := strings.TrimRight(fallback(stringValue(config, "server_url"), "https://api2.pushdeer.com"), "/") + "/message/push"
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"pushkey": stringValue(config, "push_key"), "text": message.Title, "desp": strings.ReplaceAll(message.Content+linkSuffix(message.URL), "\n", "\n\n"), "type": "markdown"})
		case "pushover":
			values := url.Values{"message": {message.Content}, "user": {stringValue(config, "user_key")}, "token": {stringValue(config, "app_token")}, "title": {message.Title}, "priority": {fmt.Sprint(intValue(config, "priority", 0))}}
			for key, field := range map[string]string{"device": "device", "sound": "sound"} {
				if value := stringValue(config, field); value != "" {
					values.Set(key, value)
				}
			}
			if ttl := intValue(config, "ttl", 0); ttl > 0 {
				values.Set("ttl", fmt.Sprint(ttl))
			}
			if intValue(config, "priority", 0) == 2 {
				values.Set("retry", "30")
				values.Set("expire", "3600")
			}
			if message.URL != "" {
				values.Set("url", message.URL)
				values.Set("url_title", "打开活动")
			}
			return r.sendForm(ctx, http.MethodPost, "https://api.pushover.net/1/messages.json", nil, values)
		case "pushplus":
			return r.sendJSON(ctx, http.MethodPost, "https://www.pushplus.plus/send", nil, map[string]any{"token": stringValue(config, "send_key"), "title": message.Title, "content": message.Content + linkSuffix(message.URL), "template": "html"})
		case "pushy":
			target := "https://api.pushy.me/push?api_key=" + url.QueryEscape(stringValue(config, "api_key"))
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"to": stringValue(config, "device_token"), "data": map[string]any{"activity_id": message.ActivityID}, "notification": map[string]any{"title": message.Title, "body": message.Content + linkSuffix(message.URL), "badge": 1, "sound": "ping.aiff"}})
		case "serverchan":
			key := stringValue(config, "send_key")
			target := "https://sctapi.ftqq.com/" + url.PathEscape(key) + ".send"
			if match := regexp.MustCompile(`(?i)^sctp(\d+)t`).FindStringSubmatch(key); len(match) > 1 {
				target = "https://" + match[1] + ".push.ft07.com/send/" + url.PathEscape(key) + ".send"
			}
			return r.sendJSON(ctx, http.MethodPost, target, nil, map[string]any{"title": message.Title, "desp": message.Content + linkSuffix(message.URL)})
		case "spugpush":
			return r.sendJSON(ctx, http.MethodPost, "https://push.spug.cc/send/"+url.PathEscape(stringValue(config, "template_key")), nil, map[string]any{"title": message.Title, "content": message.Content + linkSuffix(message.URL)})
		case "techulus-push":
			payload := map[string]any{"title": message.Title, "body": message.Content + linkSuffix(message.URL), "timeSensitive": boolValue(config, "time_sensitive")}
			if value := stringValue(config, "channel"); value != "" {
				payload["channel"] = value
			}
			if value := stringValue(config, "sound"); value != "" {
				payload["sound"] = value
			}
			return r.sendJSON(ctx, http.MethodPost, "https://push.techulus.com/api/v1/notify/"+url.PathEscape(stringValue(config, "api_key")), nil, payload)
		case "wpush":
			return r.sendJSON(ctx, http.MethodPost, "https://api.wpush.cn/api/v1/send", nil, map[string]any{"title": message.Title, "content": message.Content + linkSuffix(message.URL), "apikey": stringValue(config, "api_key"), "channel": stringValue(config, "channel")})
		case "wxpusher":
			values := splitComma(stringValue(config, "spt"))
			for start := 0; start < len(values); start += 10 {
				end := start + 10
				if end > len(values) {
					end = len(values)
				}
				if err := r.sendJSON(ctx, http.MethodPost, "https://wxpusher.zjiecode.com/api/send/message/simple-push", nil, map[string]any{"content": message.Content + linkSuffix(message.URL), "summary": truncateText(message.Title, 100), "contentType": 1, "sptList": values[start:end]}); err != nil {
					return err
				}
			}
			return nil
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown push provider %s", id), Permanent: true}
		}
	}
}

func truncateText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
