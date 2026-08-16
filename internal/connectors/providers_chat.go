package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func registerChatProviders(r *Registry) {
	definitions := []Provider{
		provider("bale", "Bale", categoryChat, "", []Field{
			secretField("bot_token", "Bot Token", true), textField("chat_id", "Chat ID", true),
		}, r.chatSender("bale")),
		provider("bitrix24", "Bitrix24", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("user_id", "用户 ID", true),
		}, r.chatSender("bitrix24")),
		provider("clickup", "ClickUp", categoryChat, "", []Field{
			secretField("token", "Token", true), textField("workspace_id", "Workspace ID", true), textField("channel_id", "Channel ID", true),
		}, r.chatSender("clickup")),
		provider("discord", "Discord", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "显示名称", false),
			textField("thread_id", "Thread ID", false), booleanField("silent", "静默发送", false),
		}, r.chatSender("discord")),
		provider("evolution", "WhatsApp（Evolution）", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("api_key", "API Key", true),
			textField("instance", "实例名称", true), textField("recipient", "接收号码", true),
		}, r.chatSender("evolution")),
		provider("fluxer", "Fluxer", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "显示名称", false),
		}, r.chatSender("fluxer")),
		provider("google-chat", "Google Chat", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true),
		}, r.chatSender("google-chat")),
		provider("kook", "KOOK", categoryChat, "", []Field{
			secretField("bot_token", "Bot Token", true), textField("target_id", "目标 ID", true),
		}, r.chatSender("kook")),
		provider("line", "LINE Messenger", categoryChat, "", []Field{
			secretField("channel_access_token", "Channel Access Token", true), textField("user_id", "用户 ID", true),
		}, r.chatSender("line")),
		provider("matrix", "Matrix", categoryChat, "", []Field{
			urlField("homeserver_url", "Homeserver 地址", true), secretField("access_token", "Access Token", true),
			textField("room_id", "Room ID", true),
		}, r.chatSender("matrix")),
		provider("mattermost", "Mattermost", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "显示名称", false), textField("channel", "频道", false),
		}, r.chatSender("mattermost")),
		provider("max", "MAX", categoryChat, "", []Field{
			urlField("api_url", "API 地址", false), secretField("bot_token", "Bot Token", true), textField("chat_id", "Chat ID", true),
		}, r.chatSender("max")),
		provider("milky", "Milky（QQ）", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("access_token", "Access Token", false),
			selectField("message_type", "消息类型", true, "private", option("私聊", "private"), option("群聊", "group")),
			textField("recipient_id", "接收方 ID", true),
		}, r.chatSender("milky")),
		provider("nextcloudtalk", "Nextcloud Talk", categoryChat, "", []Field{
			urlField("server_url", "Nextcloud 地址", true), secretField("bot_secret", "Bot Secret", true),
			sensitiveTextField("conversation_token", "Conversation Token", true),
		}, r.chatSender("nextcloudtalk")),
		provider("onebot", "OneBot", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("access_token", "Access Token", false),
			selectField("message_type", "消息类型", true, "private", option("私聊", "private"), option("群聊", "group")),
			textField("recipient_id", "接收方 ID", true),
		}, r.chatSender("onebot")),
		provider("onechat", "OneChat", categoryChat, "", []Field{
			secretField("access_token", "Access Token", true), textField("bot_id", "Bot ID", true), textField("recipient_id", "接收方 ID", true),
		}, r.chatSender("onechat")),
		provider("onesender", "OneSender", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("token", "Token", true), textField("recipient", "接收方", true),
			selectField("recipient_type", "接收方类型", true, "private", option("个人", "private"), option("群组", "group")),
		}, r.chatSender("onesender")),
		provider("openwa", "WhatsApp（OpenWA）", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("api_key", "API Key", true), textField("session", "Session", true),
			textField("chat_ids", "Chat ID（多个用逗号分隔）", true),
		}, r.chatSender("openwa")),
		provider("pumble", "Pumble", categoryChat, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.chatSender("pumble")),
		provider("rocket-chat", "Rocket.Chat", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "显示名称", false), textField("channel", "频道", false),
		}, r.chatSender("rocket-chat")),
		provider("signal", "Signal", categoryChat, "", []Field{
			urlField("api_url", "Signal REST API 地址", true), textField("sender", "发送号码", true),
			textField("recipients", "接收号码（多个用逗号分隔）", true),
		}, r.chatSender("signal")),
		provider("slack", "Slack", categoryChat, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), textField("username", "显示名称", false), textField("channel", "频道", false),
		}, r.chatSender("slack")),
		provider("stackfield", "Stackfield", categoryChat, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.chatSender("stackfield")),
		provider("teams", "Microsoft Teams", categoryChat, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.chatSender("teams")),
		provider("telegram", "Telegram", categoryChat, "", []Field{
			secretField("bot_token", "Bot Token", true), textField("chat_id", "Chat ID", true), textField("thread_id", "话题 ID", false),
			urlField("api_url", "Bot API 地址", false), booleanField("silent", "静默发送", false), booleanField("protect_content", "保护内容", false),
		}, r.chatSender("telegram")),
		provider("threema", "Threema", categoryChat, "", []Field{
			textField("sender_identity", "发送方 Identity", true), secretField("secret", "API Secret", true),
			selectField("recipient_type", "接收方类型", true, "identity", option("Identity", "identity"), option("手机号", "phone"), option("邮箱", "email")),
			textField("recipient", "接收方", true),
		}, r.chatSender("threema")),
		provider("vk", "VK", categoryChat, "", []Field{
			secretField("access_token", "Access Token", true), textField("peer_id", "Peer ID", true),
			textField("api_version", "API 版本", false), booleanField("dont_parse_links", "不解析链接", false),
		}, r.chatSender("vk")),
		provider("vkteams", "VK Teams", categoryChat, "", []Field{
			urlField("api_url", "API 地址", false), secretField("bot_token", "Bot Token", true), textField("chat_id", "Chat ID", true),
		}, r.chatSender("vkteams")),
		provider("waha", "WhatsApp（WAHA）", categoryChat, "", []Field{
			urlField("api_url", "API 地址", true), secretField("api_key", "API Key", false), textField("session", "Session", true), textField("chat_id", "Chat ID", true),
		}, r.chatSender("waha")),
		provider("whapi", "WhatsApp（Whapi）", categoryChat, "", []Field{
			urlField("api_url", "API 地址", false), secretField("token", "Token", true), textField("recipient", "接收方", true),
		}, r.chatSender("whapi")),
		provider("zoho-cliq", "Zoho Cliq", categoryChat, "", []Field{sensitiveURLField("webhook_url", "Webhook 地址", true)}, r.chatSender("zoho-cliq")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) chatSender(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		text := message.Text()
		switch id {
		case "bale":
			return r.sendJSON(ctx, http.MethodPost, "https://tapi.bale.ai/bot"+url.PathEscape(stringValue(config, "bot_token"))+"/sendMessage", nil,
				map[string]any{"chat_id": stringValue(config, "chat_id"), "text": text})
		case "bitrix24":
			target := strings.TrimRight(stringValue(config, "webhook_url"), "/") + "/im.notify.system.add.json"
			query := url.Values{"user_id": {stringValue(config, "user_id")}, "message": {"[B]Marvo[/B]"}, "ATTACH[BLOCKS][0][MESSAGE]": {text}}
			return r.sendRequest(ctx, http.MethodGet, target+"?"+query.Encode(), nil, nil)
		case "clickup":
			target := fmt.Sprintf("https://api.clickup.com/api/v3/workspaces/%s/chat/channels/%s/messages", url.PathEscape(stringValue(config, "workspace_id")), url.PathEscape(stringValue(config, "channel_id")))
			return r.sendJSON(ctx, http.MethodPost, target, map[string]string{"Authorization": stringValue(config, "token")}, map[string]any{"type": "message", "content": text, "content_format": "text/md"})
		case "discord", "fluxer":
			target := stringValue(config, "webhook_url")
			if id == "discord" && stringValue(config, "thread_id") != "" {
				parsed, err := url.Parse(target)
				if err != nil {
					return &DeliveryError{Err: err, Permanent: true}
				}
				query := parsed.Query()
				query.Set("thread_id", stringValue(config, "thread_id"))
				parsed.RawQuery = query.Encode()
				target = parsed.String()
			}
			payload := map[string]any{"username": fallback(stringValue(config, "username"), "Marvo"), "content": text}
			if boolValue(config, "silent") {
				payload["flags"] = 1 << 12
			}
			return r.sendJSON(ctx, http.MethodPost, target, nil, payload)
		case "evolution":
			target := strings.TrimRight(stringValue(config, "api_url"), "/") + "/message/sendText/" + url.PathEscape(stringValue(config, "instance"))
			return r.sendJSON(ctx, http.MethodPost, target, map[string]string{"apikey": stringValue(config, "api_key")}, map[string]any{"number": stringValue(config, "recipient"), "text": text})
		case "google-chat":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"text": text})
		case "kook":
			return r.sendJSON(ctx, http.MethodPost, "https://www.kookapp.cn/api/v3/message/create", map[string]string{"Authorization": "Bot " + stringValue(config, "bot_token")}, map[string]any{"target_id": stringValue(config, "target_id"), "content": text})
		case "line":
			return r.sendJSON(ctx, http.MethodPost, "https://api.line.me/v2/bot/message/push", map[string]string{"Authorization": "Bearer " + stringValue(config, "channel_access_token")}, map[string]any{"to": stringValue(config, "user_id"), "messages": []any{map[string]any{"type": "text", "text": text}}})
		case "matrix":
			target := strings.TrimRight(stringValue(config, "homeserver_url"), "/") + "/_matrix/client/v3/rooms/" + url.PathEscape(stringValue(config, "room_id")) + "/send/m.room.message/" + url.PathEscape(message.DeliveryID)
			return r.sendJSON(ctx, http.MethodPut, target, map[string]string{"Authorization": "Bearer " + stringValue(config, "access_token")}, map[string]any{"msgtype": "m.text", "body": text})
		case "mattermost":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"username": fallback(stringValue(config, "username"), "Marvo"), "channel": stringValue(config, "channel"), "text": text})
		case "max":
			target := strings.TrimRight(fallback(stringValue(config, "api_url"), "https://platform-api.max.ru"), "/") + "/messages?chat_id=" + url.QueryEscape(stringValue(config, "chat_id"))
			return r.sendJSON(ctx, http.MethodPost, target, map[string]string{"Authorization": stringValue(config, "bot_token")}, map[string]any{"text": text})
		case "milky":
			target := strings.TrimRight(stringValue(config, "api_url"), "/") + "/api/send_" + stringValue(config, "message_type") + "_message"
			payload := map[string]any{"message": []any{map[string]any{"type": "text", "data": map[string]any{"text": text}}}}
			if stringValue(config, "message_type") == "group" {
				payload["group_id"] = stringValue(config, "recipient_id")
			} else {
				payload["user_id"] = stringValue(config, "recipient_id")
			}
			return r.sendJSON(ctx, http.MethodPost, target, bearerHeader(config, "access_token"), payload)
		case "nextcloudtalk":
			random := message.DeliveryID
			mac := hmac.New(sha256.New, []byte(stringValue(config, "bot_secret")))
			_, _ = mac.Write([]byte(random + text))
			headers := map[string]string{"X-Nextcloud-Talk-Bot-Random": random, "X-Nextcloud-Talk-Bot-Signature": hex.EncodeToString(mac.Sum(nil)), "OCS-APIRequest": "true"}
			target := strings.TrimRight(stringValue(config, "server_url"), "/") + "/ocs/v2.php/apps/spreed/api/v1/bot/" + url.PathEscape(stringValue(config, "conversation_token")) + "/message"
			return r.sendJSON(ctx, http.MethodPost, target, headers, map[string]any{"message": text, "silent": false})
		case "onebot":
			target := strings.TrimRight(stringValue(config, "api_url"), "/") + "/send_msg"
			payload := map[string]any{"auto_escape": true, "message": text, "message_type": stringValue(config, "message_type")}
			if stringValue(config, "message_type") == "group" {
				payload["group_id"] = stringValue(config, "recipient_id")
			} else {
				payload["user_id"] = stringValue(config, "recipient_id")
			}
			return r.sendJSON(ctx, http.MethodPost, target, bearerHeader(config, "access_token"), payload)
		case "onechat":
			return r.sendJSON(ctx, http.MethodPost, "https://chat-api.one.th/message/api/v1/push_message", bearerHeader(config, "access_token"), map[string]any{"to": stringValue(config, "recipient_id"), "bot_id": stringValue(config, "bot_id"), "type": "text", "message": text})
		case "onesender":
			recipient := stringValue(config, "recipient")
			recipientType := "individual"
			if stringValue(config, "recipient_type") == "group" {
				recipient += "@g.us"
				recipientType = "group"
			} else {
				recipient += "@s.whatsapp.net"
			}
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "api_url"), bearerHeader(config, "token"), map[string]any{"to": recipient, "type": "text", "recipient_type": recipientType, "text": map[string]any{"body": text}})
		case "openwa":
			target := strings.TrimRight(stringValue(config, "api_url"), "/") + "/api/sessions/" + url.PathEscape(stringValue(config, "session")) + "/messages/send-text"
			for _, chatID := range splitComma(stringValue(config, "chat_ids")) {
				if err := r.sendJSON(ctx, http.MethodPost, target, map[string]string{"X-Api-Key": stringValue(config, "api_key")}, map[string]any{"chatId": chatID, "text": text}); err != nil {
					return err
				}
			}
			return nil
		case "pumble":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"attachments": []any{map[string]any{"title": message.Title, "text": message.Content + linkSuffix(message.URL), "color": "#5BDD8B"}}})
		case "rocket-chat":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"text": text, "channel": stringValue(config, "channel"), "username": fallback(stringValue(config, "username"), "Marvo")})
		case "signal":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "api_url"), nil, map[string]any{"message": text, "number": stringValue(config, "sender"), "recipients": splitComma(stringValue(config, "recipients"))})
		case "slack":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"text": text, "channel": stringValue(config, "channel"), "username": fallback(stringValue(config, "username"), "Marvo")})
		case "stackfield":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"Title": "+" + message.Title + "+\n" + message.Content + linkSuffix(message.URL)})
		case "teams":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, teamsPayload(message))
		case "telegram":
			base := strings.TrimRight(fallback(stringValue(config, "api_url"), "https://api.telegram.org"), "/")
			payload := map[string]any{"chat_id": stringValue(config, "chat_id"), "text": text, "disable_notification": boolValue(config, "silent"), "protect_content": boolValue(config, "protect_content"), "link_preview_options": map[string]any{"is_disabled": true}}
			if threadID := stringValue(config, "thread_id"); threadID != "" {
				payload["message_thread_id"] = threadID
			}
			return r.sendJSON(ctx, http.MethodPost, base+"/bot"+stringValue(config, "bot_token")+"/sendMessage", nil, payload)
		case "threema":
			values := url.Values{"from": {stringValue(config, "sender_identity")}, "secret": {stringValue(config, "secret")}, "text": {text}}
			values.Set(map[string]string{"identity": "to", "phone": "phone", "email": "email"}[stringValue(config, "recipient_type")], stringValue(config, "recipient"))
			return r.sendForm(ctx, http.MethodPost, "https://msgapi.threema.ch/send_simple", nil, values)
		case "vk":
			values := url.Values{"access_token": {stringValue(config, "access_token")}, "v": {fallback(stringValue(config, "api_version"), "5.199")}, "peer_id": {stringValue(config, "peer_id")}, "message": {text}, "random_id": {deliveryNumericID(message.DeliveryID)}}
			if boolValue(config, "dont_parse_links") {
				values.Set("dont_parse_links", "1")
			}
			return r.sendForm(ctx, http.MethodPost, "https://api.vk.ru/method/messages.send", nil, values)
		case "vkteams":
			base := strings.TrimRight(fallback(stringValue(config, "api_url"), "https://myteam.mail.ru"), "/")
			query := url.Values{"token": {stringValue(config, "bot_token")}, "chatId": {stringValue(config, "chat_id")}, "text": {text}}
			return r.sendRequest(ctx, http.MethodGet, base+"/bot/v1/messages/sendText?"+query.Encode(), nil, nil)
		case "waha":
			return r.sendJSON(ctx, http.MethodPost, strings.TrimRight(stringValue(config, "api_url"), "/")+"/api/sendText", optionalAPIKeyHeader(config, "api_key", "X-Api-Key"), map[string]any{"session": stringValue(config, "session"), "chatId": stringValue(config, "chat_id"), "text": text})
		case "whapi":
			base := strings.TrimRight(fallback(stringValue(config, "api_url"), "https://gate.whapi.cloud"), "/")
			return r.sendJSON(ctx, http.MethodPost, base+"/messages/text", bearerHeader(config, "token"), map[string]any{"to": stringValue(config, "recipient"), "body": text})
		case "zoho-cliq":
			return r.sendJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{"text": text})
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown chat provider %s", id), Permanent: true}
		}
	}
}

func bearerHeader(config map[string]any, key string) map[string]string {
	if token := stringValue(config, key); token != "" {
		return map[string]string{"Authorization": "Bearer " + token}
	}
	return nil
}

func optionalAPIKeyHeader(config map[string]any, key, header string) map[string]string {
	if value := stringValue(config, key); value != "" {
		return map[string]string{header: value}
	}
	return nil
}

func splitComma(value string) []string {
	result := make([]string, 0)
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func fallback(value, fallbackValue string) string {
	if value != "" {
		return value
	}
	return fallbackValue
}
func linkSuffix(value string) string {
	if value == "" {
		return ""
	}
	return "\n\n" + value
}
func deliveryNumericID(value string) string {
	if len(value) > 8 {
		value = value[:8]
	}
	var result uint32
	for _, character := range value {
		result = result*33 + uint32(character)
	}
	return fmt.Sprint(result & 0x7fffffff)
}

func teamsPayload(message Message) map[string]any {
	facts := []any{map[string]any{"title": "内容", "value": message.Content}}
	actions := []any{}
	if message.URL != "" {
		actions = append(actions, map[string]any{"type": "Action.OpenUrl", "title": "打开活动", "url": message.URL})
	}
	body := []any{map[string]any{"type": "TextBlock", "size": "Medium", "weight": "Bolder", "text": message.Title}, map[string]any{"type": "FactSet", "facts": facts}}
	if len(actions) > 0 {
		body = append(body, map[string]any{"type": "ActionSet", "actions": actions})
	}
	return map[string]any{"type": "message", "summary": message.Title, "attachments": []any{map[string]any{"contentType": "application/vnd.microsoft.card.adaptive", "content": map[string]any{"type": "AdaptiveCard", "$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "version": "1.4", "body": body}}}}
}
