package connectors

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

func registerSMSProviders(r *Registry) {
	definitions := []Provider{
		provider("360messenger", "WhatsApp（360Messenger）", categorySMS, "", []Field{
			secretField("token", "Auth Token", true), textField("recipients", "接收号码（多个用逗号分隔）", false), textField("group_ids", "群组 ID（多个用逗号分隔）", false),
		}, r.smsSender("360messenger")),
		provider("46elks", "46elks", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("auth_token", "Auth Token", true), textField("from", "发送号码", true), textField("to", "接收号码", true),
		}, r.smsSender("46elks")),
		provider("aliyun-sms", "阿里云短信", categorySMS, "", []Field{
			textField("access_key_id", "AccessKey ID", true), secretField("access_key_secret", "AccessKey Secret", true),
			textField("phone_numbers", "接收号码", true), textField("sign_name", "签名名称", true), textField("template_code", "模板 Code", true),
		}, r.smsSender("aliyun-sms")),
		provider("bearsms", "BearSMS", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("hash_key", "Hash Key", true), textField("phone_number", "接收号码", true), textField("sender_id", "Sender ID", false),
		}, r.smsSender("bearsms")),
		provider("call-me-bot", "CallMeBot", categorySMS, "", []Field{sensitiveURLField("endpoint", "调用地址", true)}, r.smsSender("call-me-bot")),
		provider("cellsynt", "Cellsynt", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("password", "密码", true), textField("destination", "接收号码", true),
			textField("originator", "发送方", true), selectField("originator_type", "发送方类型", true, "alpha", option("文字", "alpha"), option("数字", "numeric")), booleanField("allow_long", "允许长短信", false),
		}, r.smsSender("cellsynt")),
		provider("clicksendsms", "ClickSend SMS", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("api_key", "API Key", true), textField("from", "发送方", false), textField("to", "接收号码", true),
		}, r.smsSender("clicksendsms")),
		provider("egosms", "EgoSMS", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("password", "密码", true), textField("sender", "发送方", false), textField("phone_number", "接收号码", true),
		}, r.smsSender("egosms")),
		provider("freemobile", "Free Mobile", categorySMS, "", []Field{textField("user", "用户", true), secretField("password", "密码", true)}, r.smsSender("freemobile")),
		provider("gtx-messaging", "GTX Messaging", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("from", "发送方", true), textField("to", "接收号码", true),
		}, r.smsSender("gtx-messaging")),
		provider("octopush", "Octopush", categorySMS, "", []Field{
			textField("login", "API Login", true), secretField("api_key", "API Key", true), textField("phone_number", "接收号码", true),
			textField("sender", "发送方", true), textField("sms_type", "短信类型", false),
		}, r.smsSender("octopush")),
		provider("ooredoo", "Ooredoo", categorySMS, "", []Field{
			urlField("server_url", "服务地址", false), textField("username", "用户名", true), secretField("access_key", "Access Key", true),
			secretField("bearer_token", "Bearer Token", true), textField("recipients", "接收号码", true),
		}, r.smsSender("ooredoo")),
		provider("plivo", "Plivo", categorySMS, "", []Field{
			textField("auth_id", "Auth ID", true), secretField("auth_token", "Auth Token", true), textField("from", "发送号码", true), textField("to", "接收号码", true),
			selectField("message_type", "类型", true, "sms", option("短信", "sms"), option("语音呼叫", "call")), urlField("answer_url", "Answer URL", false),
		}, r.smsSender("plivo")),
		provider("promosms", "PromoSMS", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("password", "密码", true), textField("phone_number", "接收号码", true),
			textField("sender", "发送方", true), numberField("sms_type", "短信类型", false, 0), booleanField("allow_long", "允许长短信", false),
		}, r.smsSender("promosms")),
		provider("serwersms", "SerwerSMS", categorySMS, "", []Field{
			textField("username", "用户名", true), secretField("password", "密码", true), textField("sender", "发送方", true),
			selectField("recipient_type", "接收方类型", true, "phone", option("号码", "phone"), option("群组", "group")), textField("recipient", "号码或群组 ID", true),
		}, r.smsSender("serwersms")),
		provider("sevenio", "seven.io", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("from", "发送方", false), textField("to", "接收号码", true),
		}, r.smsSender("sevenio")),
		provider("sms-planet", "SMSPlanet", categorySMS, "", []Field{
			secretField("api_token", "API Token", true), textField("from", "发送方", true), textField("to", "接收号码", true),
		}, r.smsSender("sms-planet")),
		provider("smsc", "SMSC", categorySMS, "", []Field{
			textField("login", "Login", true), secretField("password", "密码", true), textField("to", "接收号码", true), textField("sender", "发送方", false), booleanField("transliterate", "转写为拉丁字符", false),
		}, r.smsSender("smsc")),
		provider("smseagle", "SMSEagle", categorySMS, "", []Field{
			urlField("server_url", "服务地址", true), secretField("token", "Access Token", true), selectField("api_version", "API 版本", true, "v2", option("v2", "v2"), option("v1", "v1")),
			selectField("recipient_type", "接收方类型", true, "phone", option("号码", "phone"), option("联系人", "contact"), option("群组", "group")), textField("recipient", "接收方", true),
			booleanField("unicode", "Unicode", true), numberField("priority", "优先级", false, 0),
		}, r.smsSender("smseagle")),
		provider("smsir", "SMS.ir", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("phone_numbers", "接收号码", true), textField("template_id", "Template ID", true), textField("parameter_name", "模板参数名", false),
		}, r.smsSender("smsir")),
		provider("smsmanager", "SMSManager", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("phone_numbers", "接收号码", true), textField("gateway", "Gateway", true),
		}, r.smsSender("smsmanager")),
		provider("smspartner", "SMSPartner", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("phone_numbers", "接收号码", true), textField("sender", "发送方", true),
		}, r.smsSender("smspartner")),
		provider("telnyx", "Telnyx", categorySMS, "", []Field{
			secretField("api_key", "API Key", true), textField("from", "发送号码", true), textField("to", "接收号码", true), textField("messaging_profile_id", "Messaging Profile ID", false),
		}, r.smsSender("telnyx")),
		provider("teltonika", "Teltonika", categorySMS, "", []Field{
			urlField("server_url", "设备地址", true), textField("username", "用户名", true), secretField("password", "密码", true),
			textField("modem", "Modem", true), textField("phone_number", "接收号码", true),
		}, r.smsSender("teltonika")),
		provider("twilio", "Twilio", categorySMS, "", []Field{
			textField("account_sid", "Account SID", true), textField("api_key", "API Key", false), secretField("auth_token", "Auth Token", true),
			textField("from", "发送号码", false), textField("to", "接收号码", true), textField("messaging_service_sid", "Messaging Service SID", false),
		}, r.smsSender("twilio")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) smsSender(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		text := message.Text()
		switch id {
		case "360messenger":
			headers := bearerHeader(config, "token")
			for _, recipient := range splitComma(stringValue(config, "recipients")) {
				if err := r.sendJSON(ctx, http.MethodPost, "https://api.360messenger.com/v2/sendMessage", headers, map[string]any{"phonenumber": recipient, "text": text}); err != nil {
					return err
				}
			}
			for _, groupID := range splitComma(stringValue(config, "group_ids")) {
				if err := r.sendJSON(ctx, http.MethodPost, "https://api.360messenger.com/v2/sendGroup", headers, map[string]any{"groupId": groupID, "text": text}); err != nil {
					return err
				}
			}
			if len(splitComma(stringValue(config, "recipients")))+len(splitComma(stringValue(config, "group_ids"))) == 0 {
				return &DeliveryError{Err: fmt.Errorf("至少需要一个接收号码或群组"), Permanent: true}
			}
			return nil
		case "46elks":
			return r.sendForm(ctx, http.MethodPost, "https://api.46elks.com/a1/sms", map[string]string{"Authorization": basicAuth(stringValue(config, "username"), stringValue(config, "auth_token"))}, url.Values{"from": {stringValue(config, "from")}, "to": {stringValue(config, "to")}, "message": {text}})
		case "aliyun-sms":
			return r.sendAliyunSMS(ctx, config, message)
		case "bearsms":
			query := url.Values{"app": {"ws"}, "u": {stringValue(config, "username")}, "h": {stringValue(config, "hash_key")}, "op": {"pv"}, "to": {stringValue(config, "phone_number")}, "msg": {text}}
			if sender := stringValue(config, "sender_id"); sender != "" {
				query.Set("from", sender)
			}
			if !isASCII(text) {
				query.Set("unicode", "1")
			}
			return r.sendRequest(ctx, http.MethodGet, "https://app.bearsms.com/index.php?"+query.Encode(), nil, nil)
		case "call-me-bot":
			parsed, err := url.Parse(stringValue(config, "endpoint"))
			if err != nil {
				return &DeliveryError{Err: err, Permanent: true}
			}
			query := parsed.Query()
			query.Set("text", text)
			parsed.RawQuery = query.Encode()
			return r.sendRequest(ctx, http.MethodGet, parsed.String(), nil, nil)
		case "cellsynt":
			values := url.Values{"username": {stringValue(config, "username")}, "password": {stringValue(config, "password")}, "destination": {stringValue(config, "destination")}, "text": {asciiOnly(text)}, "originatortype": {stringValue(config, "originator_type")}, "originator": {stringValue(config, "originator")}, "allowconcat": {map[bool]string{true: "6", false: "1"}[boolValue(config, "allow_long")]}}
			return r.sendForm(ctx, http.MethodPost, "https://se-1.cellsynt.net/sms.php", nil, values)
		case "clicksendsms":
			return r.sendJSON(ctx, http.MethodPost, "https://rest.clicksend.com/v3/sms/send", map[string]string{"Authorization": basicAuth(stringValue(config, "username"), stringValue(config, "api_key"))}, map[string]any{"messages": []any{map[string]any{"body": asciiOnly(text), "to": stringValue(config, "to"), "source": "marvo", "from": stringValue(config, "from")}}})
		case "egosms":
			query := url.Values{"number": {stringValue(config, "phone_number")}, "message": {text}, "username": {stringValue(config, "username")}, "password": {stringValue(config, "password")}, "sender": {fallback(stringValue(config, "sender"), "MARVO")}, "priority": {"0"}}
			return r.sendRequest(ctx, http.MethodGet, "https://www.egosms.co/api/v1/plain/?"+query.Encode(), nil, nil)
		case "freemobile":
			target := "https://smsapi.free-mobile.fr/sendmsg?" + url.Values{"msg": {text}, "user": {stringValue(config, "user")}, "pass": {stringValue(config, "password")}}.Encode()
			return r.sendRequest(ctx, http.MethodGet, target, nil, nil)
		case "gtx-messaging":
			return r.sendForm(ctx, http.MethodPost, "https://rest.gtx-messaging.net/smsc/sendsms/"+url.PathEscape(stringValue(config, "api_key"))+"/json", nil, url.Values{"from": {stringValue(config, "from")}, "to": {stringValue(config, "to")}, "text": {text}})
		case "octopush":
			return r.sendJSON(ctx, http.MethodPost, "https://api.octopush.com/v1/public/sms-campaign/send", map[string]string{"api-key": stringValue(config, "api_key"), "api-login": stringValue(config, "login")}, map[string]any{"recipients": []any{map[string]any{"phone_number": stringValue(config, "phone_number")}}, "text": asciiOnly(text), "type": fallback(stringValue(config, "sms_type"), "sms_premium"), "purpose": "alert", "sender": stringValue(config, "sender")})
		case "ooredoo":
			target := fallback(stringValue(config, "server_url"), "https://o-papi1-lb01.ooredoo.mv/bulk_sms/v2")
			recipients := splitComma(stringValue(config, "recipients"))
			for start := 0; start < len(recipients); start += 20 {
				end := start + 20
				if end > len(recipients) {
					end = len(recipients)
				}
				values := url.Values{"username": {stringValue(config, "username")}, "access_key": {base64.StdEncoding.EncodeToString([]byte(stringValue(config, "access_key")))}, "message": {text}, "batch": {strings.Join(recipients[start:end], " ")}}
				if err := r.sendForm(ctx, http.MethodPost, target, bearerHeader(config, "bearer_token"), values); err != nil {
					return err
				}
			}
			return nil
		case "plivo":
			base := "https://api.plivo.com/v1/Account/" + url.PathEscape(stringValue(config, "auth_id"))
			headers := map[string]string{"Authorization": basicAuth(stringValue(config, "auth_id"), stringValue(config, "auth_token"))}
			if stringValue(config, "message_type") == "call" {
				answer, err := url.Parse(stringValue(config, "answer_url"))
				if err != nil {
					return &DeliveryError{Err: err, Permanent: true}
				}
				query := answer.Query()
				query.Set("message", text)
				answer.RawQuery = query.Encode()
				return r.sendJSON(ctx, http.MethodPost, base+"/Call/", headers, map[string]any{"from": stringValue(config, "from"), "to": stringValue(config, "to"), "answer_url": answer.String(), "answer_method": "GET"})
			}
			return r.sendJSON(ctx, http.MethodPost, base+"/Message/", headers, map[string]any{"src": stringValue(config, "from"), "dst": stringValue(config, "to"), "text": text})
		case "promosms":
			clean := asciiOnly(text)
			limit := 159
			if boolValue(config, "allow_long") {
				limit = 639
			}
			clean = truncateBytes(clean, limit)
			return r.sendJSON(ctx, http.MethodPost, "https://promosms.com/api/rest/v3_2/sms", map[string]string{"Authorization": basicAuth(stringValue(config, "username"), stringValue(config, "password"))}, map[string]any{"recipients": []string{stringValue(config, "phone_number")}, "text": clean, "long-sms": boolValue(config, "allow_long"), "type": intValue(config, "sms_type", 0), "sender": stringValue(config, "sender")})
		case "serwersms":
			payload := map[string]any{"username": stringValue(config, "username"), "password": stringValue(config, "password"), "text": asciiOnly(text), "sender": stringValue(config, "sender")}
			if stringValue(config, "recipient_type") == "group" {
				payload["group_id"] = stringValue(config, "recipient")
			} else {
				payload["phone"] = stringValue(config, "recipient")
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api2.serwersms.pl/messages/send_sms", nil, payload)
		case "sevenio":
			return r.sendJSON(ctx, http.MethodPost, "https://gateway.seven.io/api/sms", map[string]string{"X-API-Key": stringValue(config, "api_key")}, map[string]any{"to": stringValue(config, "to"), "from": fallback(stringValue(config, "from"), "Marvo"), "text": text})
		case "sms-planet":
			return r.sendForm(ctx, http.MethodPost, "https://api2.smsplanet.pl/sms", bearerHeader(config, "api_token"), url.Values{"from": {stringValue(config, "from")}, "to": {stringValue(config, "to")}, "msg": {text}})
		case "smsc":
			query := url.Values{"fmt": {"3"}, "translit": {map[bool]string{true: "1", false: "0"}[boolValue(config, "transliterate")]}, "login": {stringValue(config, "login")}, "psw": {stringValue(config, "password")}, "phones": {stringValue(config, "to")}, "mes": {asciiOnly(text)}}
			if sender := stringValue(config, "sender"); sender != "" {
				query.Set("sender", sender)
			}
			return r.sendRequest(ctx, http.MethodGet, "https://smsc.kz/sys/send.php?"+query.Encode(), nil, nil)
		case "smseagle":
			return r.sendSMSEagle(ctx, config, text)
		case "smsir":
			parameter := fallback(stringValue(config, "parameter_name"), "marvoactivity")
			short := truncateText(strings.ReplaceAll(text, " ", ""), 20)
			for _, mobile := range splitComma(stringValue(config, "phone_numbers")) {
				if err := r.sendJSON(ctx, http.MethodPost, "https://api.sms.ir/v1/send/verify", map[string]string{"X-API-Key": stringValue(config, "api_key")}, map[string]any{"mobile": mobile, "templateId": stringValue(config, "template_id"), "parameters": []any{map[string]any{"name": parameter, "value": short}}}); err != nil {
					return err
				}
			}
			return nil
		case "smsmanager":
			query := url.Values{"apikey": {stringValue(config, "api_key")}, "message": {asciiOnly(text)}, "number": {stringValue(config, "phone_numbers")}, "gateway": {stringValue(config, "gateway")}}
			return r.sendRequest(ctx, http.MethodGet, "https://http-api.smsmanager.cz/Send?"+query.Encode(), nil, nil)
		case "smspartner":
			return r.sendJSON(ctx, http.MethodPost, "https://api.smspartner.fr/v1/send", nil, map[string]any{"apiKey": stringValue(config, "api_key"), "sender": truncateText(stringValue(config, "sender"), 11), "phoneNumbers": stringValue(config, "phone_numbers"), "message": truncateBytes(asciiOnly(text), 639)})
		case "telnyx":
			payload := map[string]any{"from": stringValue(config, "from"), "to": stringValue(config, "to"), "text": text}
			if value := stringValue(config, "messaging_profile_id"); value != "" {
				payload["messaging_profile_id"] = value
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api.telnyx.com/v2/messages", bearerHeader(config, "api_key"), payload)
		case "teltonika":
			return r.sendTeltonika(ctx, config, truncateText(text, 159))
		case "twilio":
			apiKey := fallback(stringValue(config, "api_key"), stringValue(config, "account_sid"))
			values := url.Values{"To": {stringValue(config, "to")}, "Body": {text}}
			if from := stringValue(config, "from"); from != "" {
				values.Set("From", from)
			}
			if service := stringValue(config, "messaging_service_sid"); service != "" {
				values.Set("MessagingServiceSid", service)
			}
			target := "https://api.twilio.com/2010-04-01/Accounts/" + url.PathEscape(stringValue(config, "account_sid")) + "/Messages.json"
			return r.sendForm(ctx, http.MethodPost, target, map[string]string{"Authorization": basicAuth(apiKey, stringValue(config, "auth_token"))}, values)
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown SMS provider %s", id), Permanent: true}
		}
	}
}

func (r *Registry) sendAliyunSMS(ctx context.Context, config map[string]any, message Message) error {
	parameters := map[string]string{
		"PhoneNumbers": stringValue(config, "phone_numbers"), "TemplateCode": stringValue(config, "template_code"), "SignName": stringValue(config, "sign_name"),
		"TemplateParam": mustJSONText(map[string]any{"title": message.Title, "content": message.Content, "url": message.URL}), "AccessKeyId": stringValue(config, "access_key_id"),
		"Format": "JSON", "SignatureMethod": "HMAC-SHA1", "SignatureVersion": "1.0", "SignatureNonce": message.DeliveryID, "Timestamp": time.Now().UTC().Format(timeRFC3339), "Action": "SendSms", "Version": "2017-05-25",
	}
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, aliyunEscape(key)+"="+aliyunEscape(parameters[key]))
	}
	toSign := "POST&%2F&" + aliyunEscape(strings.Join(pairs, "&"))
	mac := hmac.New(sha1.New, []byte(stringValue(config, "access_key_secret")+"&"))
	_, _ = mac.Write([]byte(toSign))
	parameters["Signature"] = base64.StdEncoding.EncodeToString(mac.Sum(nil))
	values := url.Values{}
	for key, value := range parameters {
		values.Set(key, value)
	}
	return r.sendForm(ctx, http.MethodPost, "https://dysmsapi.aliyuncs.com/", nil, values)
}

func (r *Registry) sendSMSEagle(ctx context.Context, config map[string]any, text string) error {
	base := strings.TrimRight(stringValue(config, "server_url"), "/")
	recipientType := stringValue(config, "recipient_type")
	recipient := stringValue(config, "recipient")
	if stringValue(config, "api_version") == "v1" {
		endpoint := "/http_api/send_sms"
		key := "to"
		if recipientType == "contact" {
			endpoint = "/http_api/send_tocontact"
			key = "contactname"
		} else if recipientType == "group" {
			endpoint = "/http_api/send_togroup"
			key = "groupname"
		}
		query := url.Values{"access_token": {stringValue(config, "token")}, key: {recipient}, "message": {text}, "unicode": {map[bool]string{true: "1", false: "0"}[boolValue(config, "unicode")]}, "highpriority": {fmt.Sprint(intValue(config, "priority", 0))}}
		return r.sendRequest(ctx, http.MethodGet, base+endpoint+"?"+query.Encode(), nil, nil)
	}
	payload := map[string]any{"text": text, "encoding": map[bool]string{true: "unicode", false: "standard"}[boolValue(config, "unicode")], "priority": intValue(config, "priority", 0)}
	switch recipientType {
	case "contact":
		payload["contacts"] = splitComma(recipient)
	case "group":
		payload["groups"] = splitComma(recipient)
	default:
		payload["to"] = splitComma(recipient)
	}
	return r.sendJSON(ctx, http.MethodPost, base+"/api/v2/messages/sms", map[string]string{"access-token": stringValue(config, "token")}, payload)
}

func (r *Registry) sendTeltonika(ctx context.Context, config map[string]any, text string) error {
	base := strings.TrimRight(stringValue(config, "server_url"), "/")
	body, err := r.requestJSON(ctx, http.MethodPost, base+"/api/login", nil, map[string]any{"username": stringValue(config, "username"), "password": stringValue(config, "password")})
	if err != nil {
		return err
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || !response.Success || response.Data.Token == "" {
		return &DeliveryError{Err: fmt.Errorf("登录响应无效：Teltonika"), Permanent: true}
	}
	return r.sendJSON(ctx, http.MethodPost, base+"/api/messages/actions/send", map[string]string{"Authorization": "Bearer " + response.Data.Token}, map[string]any{"data": map[string]any{"modem": stringValue(config, "modem"), "number": stringValue(config, "phone_number"), "message": text}})
}

func aliyunEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(url.QueryEscape(value), "+", "%20"), "%7E", "~")
}
func mustJSONText(value any) string { data, _ := json.Marshal(value); return string(data) }
func isASCII(value string) bool {
	for _, character := range value {
		if character > 127 {
			return false
		}
	}
	return true
}
func asciiOnly(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if character <= 127 {
			builder.WriteRune(character)
		}
	}
	return builder.String()
}
func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
