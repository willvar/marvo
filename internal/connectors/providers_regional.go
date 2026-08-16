package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func registerRegionalProviders(r *Registry) {
	definitions := []Provider{
		provider("dingding", "钉钉", categoryRegional, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), secretField("secret", "加签密钥", false),
			textField("mentioned_mobiles", "@手机号（逗号分隔）", false),
			textField("mentioned_users", "@用户 ID（逗号分隔）", false), booleanField("mention_all", "@所有人", false),
		}, r.sendRegional("dingding")),
		provider("feishu", "飞书", categoryRegional, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true),
		}, r.sendRegional("feishu")),
		provider("wecom", "企业微信", categoryRegional, "", []Field{
			secretField("bot_key", "机器人 Key", true), textField("mentioned_mobiles", "@手机号（逗号分隔）", false),
		}, r.sendRegional("wecom")),
		provider("yzj", "云之家", categoryRegional, "", []Field{
			sensitiveURLField("webhook_url", "Webhook 地址", true), secretField("token", "Token", true),
		}, r.sendRegional("yzj")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) sendRegional(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		text := message.Text()
		switch id {
		case "dingding":
			target := stringValue(config, "webhook_url")
			if secret := stringValue(config, "secret"); secret != "" {
				timestamp := time.Now().UnixMilli()
				mac := hmac.New(sha256.New, []byte(secret))
				_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "\n" + secret))
				signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
				parsed, err := url.Parse(target)
				if err != nil {
					return &DeliveryError{Err: err, Permanent: true}
				}
				query := parsed.Query()
				query.Set("timestamp", strconv.FormatInt(timestamp, 10))
				query.Set("sign", signature)
				parsed.RawQuery = query.Encode()
				target = parsed.String()
			}
			mobiles := splitComma(stringValue(config, "mentioned_mobiles"))
			users := splitComma(stringValue(config, "mentioned_users"))
			mentions := append(append([]string{}, mobiles...), users...)
			if len(mentions) > 0 {
				text += "\n" + "@" + strings.Join(mentions, " @")
			}
			response, err := r.requestJSON(ctx, http.MethodPost, target, nil, map[string]any{
				"msgtype": "text", "text": map[string]string{"content": text},
				"at": map[string]any{"isAtAll": boolValue(config, "mention_all"), "atMobiles": mobiles, "atUserIds": users},
			})
			if err != nil {
				return err
			}
			return validateProviderResponse(response, "errcode", float64(0), "errmsg")
		case "feishu":
			response, err := r.requestJSON(ctx, http.MethodPost, stringValue(config, "webhook_url"), nil, map[string]any{
				"msg_type": "text", "content": map[string]string{"text": text},
			})
			if err != nil {
				return err
			}
			return validateAnySuccessCode(response, []string{"code", "StatusCode"})
		case "wecom":
			target := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + url.QueryEscape(stringValue(config, "bot_key"))
			textPayload := map[string]any{"content": text}
			if mobiles := splitComma(stringValue(config, "mentioned_mobiles")); len(mobiles) > 0 {
				textPayload["mentioned_mobile_list"] = mobiles
			}
			response, err := r.requestJSON(ctx, http.MethodPost, target, nil, map[string]any{"msgtype": "text", "text": textPayload})
			if err != nil {
				return err
			}
			return validateProviderResponse(response, "errcode", float64(0), "errmsg")
		case "yzj":
			parsed, err := url.Parse(stringValue(config, "webhook_url"))
			if err != nil {
				return &DeliveryError{Err: err, Permanent: true}
			}
			query := parsed.Query()
			query.Set("yzjtype", "0")
			query.Set("yzjtoken", stringValue(config, "token"))
			parsed.RawQuery = query.Encode()
			response, err := r.requestJSON(ctx, http.MethodPost, parsed.String(), nil, map[string]string{"content": text})
			if err != nil {
				return err
			}
			var body struct {
				Success bool   `json:"success"`
				Message string `json:"errmsg"`
			}
			if err := json.Unmarshal(response, &body); err != nil {
				return &DeliveryError{Err: fmt.Errorf("云之家返回了无效响应: %w", err)}
			}
			if !body.Success {
				return &DeliveryError{Err: errors.New(fallback(body.Message, "云之家拒绝了请求")), Permanent: true}
			}
			return nil
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown regional provider %s", id), Permanent: true}
		}
	}
}

func validateProviderResponse(response []byte, codeKey string, expected any, messageKey string) error {
	var body map[string]any
	if err := json.Unmarshal(response, &body); err != nil {
		return &DeliveryError{Err: fmt.Errorf("服务返回了无效响应: %w", err)}
	}
	if fmt.Sprint(body[codeKey]) == fmt.Sprint(expected) {
		return nil
	}
	message := strings.TrimSpace(fmt.Sprint(body[messageKey]))
	if message == "" || message == "<nil>" {
		message = "服务拒绝了请求"
	}
	return &DeliveryError{Err: errors.New(message), Permanent: true}
}

func validateAnySuccessCode(response []byte, keys []string) error {
	if len(response) == 0 {
		return nil
	}
	var body map[string]any
	if err := json.Unmarshal(response, &body); err != nil {
		return &DeliveryError{Err: fmt.Errorf("服务返回了无效响应: %w", err)}
	}
	for _, key := range keys {
		if value, exists := body[key]; exists {
			if fmt.Sprint(value) == "0" {
				return nil
			}
			return &DeliveryError{Err: fmt.Errorf("服务返回错误码 %v", value), Permanent: true}
		}
	}
	return nil
}
