package connectors

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

func registerEmailProviders(r *Registry) {
	definitions := []Provider{
		provider("brevo", "Brevo", categoryEmail, "", []Field{
			secretField("api_key", "API Key", true),
			textField("from_email", "发件邮箱", true),
			textField("from_name", "发件人名称", false),
			textField("to", "收件邮箱", true),
			textField("cc", "抄送（逗号分隔）", false),
			textField("bcc", "密送（逗号分隔）", false),
			textField("subject", "邮件主题", false),
		}, r.emailAPISender("brevo")),
		provider("resend", "Resend", categoryEmail, "", []Field{
			secretField("api_key", "API Key", true),
			textField("from_email", "发件邮箱", true),
			textField("from_name", "发件人名称", false),
			textField("to", "收件邮箱（逗号分隔）", true),
			textField("subject", "邮件主题", false),
		}, r.emailAPISender("resend")),
		provider("send-grid", "SendGrid", categoryEmail, "", []Field{
			secretField("api_key", "API Key", true),
			textField("from_email", "发件邮箱", true),
			textField("to", "收件邮箱", true),
			textField("cc", "抄送（逗号分隔）", false),
			textField("bcc", "密送（逗号分隔）", false),
			textField("subject", "邮件主题", false),
		}, r.emailAPISender("send-grid")),
		provider("smtp", "SMTP", categoryEmail, "", []Field{
			textField("host", "服务器", true),
			numberField("port", "端口", true, 587),
			selectField("security", "连接安全", true, "starttls",
				option("STARTTLS", "starttls"), option("TLS", "tls"), option("无加密", "none")),
			booleanField("skip_tls_verify", "忽略 TLS 证书错误", false),
			textField("username", "用户名", false),
			secretField("password", "密码", false),
			textField("from", "发件人", true),
			textField("to", "收件人（逗号分隔）", true),
			textField("cc", "抄送（逗号分隔）", false),
			textField("bcc", "密送（逗号分隔）", false),
			textField("subject", "邮件主题", false),
		}, r.sendSMTP),
		provider("turbosmtp", "TurboSMTP", categoryEmail, "", []Field{
			secretField("consumer_key", "Consumer Key", true),
			secretField("consumer_secret", "Consumer Secret", true),
			selectField("region", "区域", true, "global", option("全球", "global"), option("欧洲", "eu")),
			textField("from_email", "发件邮箱", true),
			textField("to", "收件邮箱（逗号分隔）", true),
			textField("cc", "抄送（逗号分隔）", false),
			textField("bcc", "密送（逗号分隔）", false),
			textField("subject", "邮件主题", false),
		}, r.emailAPISender("turbosmtp")),
	}
	for _, definition := range definitions {
		r.register(definition)
	}
}

func (r *Registry) emailAPISender(id string) SendFunc {
	return func(ctx context.Context, config map[string]any, message Message) error {
		subject := fallback(stringValue(config, "subject"), message.Title)
		if subject == "" {
			subject = "Marvo 活动"
		}
		text := message.Text()
		switch id {
		case "brevo":
			payload := map[string]any{
				"sender": map[string]any{
					"email": stringValue(config, "from_email"),
					"name":  fallback(stringValue(config, "from_name"), "Marvo"),
				},
				"to":          emailObjects(stringValue(config, "to")),
				"subject":     subject,
				"htmlContent": "<html><body><p>" + htmlEscapeWithBreaks(text) + "</p></body></html>",
			}
			if cc := emailObjects(stringValue(config, "cc")); len(cc) > 0 {
				payload["cc"] = cc
			}
			if bcc := emailObjects(stringValue(config, "bcc")); len(bcc) > 0 {
				payload["bcc"] = bcc
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api.brevo.com/v3/smtp/email", map[string]string{"api-key": stringValue(config, "api_key")}, payload)
		case "resend":
			from := stringValue(config, "from_email")
			if name := stringValue(config, "from_name"); name != "" {
				from = name + " <" + from + ">"
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api.resend.com/emails", bearerHeader(config, "api_key"), map[string]any{
				"from": from, "to": splitComma(stringValue(config, "to")), "subject": subject, "text": text,
			})
		case "send-grid":
			personalization := map[string]any{"to": emailObjects(stringValue(config, "to"))}
			if cc := emailObjects(stringValue(config, "cc")); len(cc) > 0 {
				personalization["cc"] = cc
			}
			if bcc := emailObjects(stringValue(config, "bcc")); len(bcc) > 0 {
				personalization["bcc"] = bcc
			}
			return r.sendJSON(ctx, http.MethodPost, "https://api.sendgrid.com/v3/mail/send", bearerHeader(config, "api_key"), map[string]any{
				"personalizations": []any{personalization},
				"from":             map[string]string{"email": stringValue(config, "from_email")},
				"subject":          subject,
				"content":          []any{map[string]string{"type": "text/plain", "value": text}},
			})
		case "turbosmtp":
			host := "api.turbo-smtp.com"
			if stringValue(config, "region") == "eu" {
				host = "api.eu.turbo-smtp.com"
			}
			payload := map[string]any{
				"from": stringValue(config, "from_email"), "to": strings.Join(splitComma(stringValue(config, "to")), ","),
				"subject": subject, "content": text,
			}
			if cc := splitComma(stringValue(config, "cc")); len(cc) > 0 {
				payload["cc"] = strings.Join(cc, ",")
			}
			if bcc := splitComma(stringValue(config, "bcc")); len(bcc) > 0 {
				payload["bcc"] = strings.Join(bcc, ",")
			}
			return r.sendJSON(ctx, http.MethodPost, "https://"+host+"/api/v2/mail/send", map[string]string{
				"consumerKey": stringValue(config, "consumer_key"), "consumerSecret": stringValue(config, "consumer_secret"),
			}, payload)
		default:
			return &DeliveryError{Err: fmt.Errorf("unknown email provider %s", id), Permanent: true}
		}
	}
}

func (r *Registry) sendSMTP(ctx context.Context, config map[string]any, message Message) error {
	host := stringValue(config, "host")
	port := intValue(config, "port", 587)
	if port < 1 || port > 65535 {
		return &DeliveryError{Err: errors.New("SMTP 端口无效"), Permanent: true}
	}
	from, err := parseMailbox(stringValue(config, "from"))
	if err != nil {
		return permanentSMTPError("发件人无效", err)
	}
	to, err := parseMailboxList(stringValue(config, "to"))
	if err != nil || len(to) == 0 {
		return permanentSMTPError("收件人无效", err)
	}
	cc, err := parseMailboxList(stringValue(config, "cc"))
	if err != nil {
		return permanentSMTPError("抄送地址无效", err)
	}
	bcc, err := parseMailboxList(stringValue(config, "bcc"))
	if err != nil {
		return permanentSMTPError("密送地址无效", err)
	}
	recipients := append(append(append([]string{}, to...), cc...), bcc...)

	address := net.JoinHostPort(host, strconv.Itoa(port))
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: boolValue(config, "skip_tls_verify")} //nolint:gosec -- explicit administrator setting
	var connection net.Conn
	security := stringValue(config, "security")
	if security == "tls" {
		connection, err = (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: tlsConfig}).DialContext(ctx, "tcp", address)
	} else {
		connection, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return classifySMTPError(err)
	}
	defer connection.Close()
	deadline := time.Now().Add(requestTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = connection.SetDeadline(deadline)

	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return classifySMTPError(err)
	}
	defer client.Close()
	if security == "starttls" {
		if supported, _ := client.Extension("STARTTLS"); !supported {
			return permanentSMTPError("SMTP 服务器不支持 STARTTLS", nil)
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return classifySMTPError(err)
		}
	}
	username := stringValue(config, "username")
	password := stringValue(config, "password")
	if username != "" || password != "" {
		if username == "" || password == "" {
			return permanentSMTPError("SMTP 用户名和密码必须同时填写", nil)
		}
		if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
			return classifySMTPError(err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return classifySMTPError(err)
	}
	for _, recipient := range recipients {
		if err := client.Rcpt(recipient); err != nil {
			return classifySMTPError(err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return classifySMTPError(err)
	}
	mailBody, err := buildSMTPMessage(from, to, cc, fallback(stringValue(config, "subject"), message.Title), message.Text(), message.DeliveryID)
	if err == nil {
		_, err = writer.Write(mailBody)
	}
	closeErr := writer.Close()
	if err != nil {
		return classifySMTPError(err)
	}
	if closeErr != nil {
		return classifySMTPError(closeErr)
	}
	// DATA has already been accepted at this point. A broken QUIT response must
	// not enqueue a duplicate copy of an otherwise successful email.
	_ = client.Quit()
	return nil
}

func parseMailbox(value string) (*mail.Address, error) {
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("mailbox contains a line break")
	}
	return mail.ParseAddress(value)
}

func parseMailboxList(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("mailbox list contains a line break")
	}
	addresses, err := mail.ParseAddressList(value)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.Address)
	}
	return result, nil
}

func buildSMTPMessage(from *mail.Address, to, cc []string, subject, body, deliveryID string) ([]byte, error) {
	if subject == "" {
		subject = "Marvo 活动"
	}
	if strings.ContainsAny(subject, "\r\n") {
		return nil, errors.New("subject contains a line break")
	}
	var buffer bytes.Buffer
	headers := textproto.MIMEHeader{}
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("From", from.String())
	headers.Set("To", strings.Join(to, ", "))
	if len(cc) > 0 {
		headers.Set("Cc", strings.Join(cc, ", "))
	}
	headers.Set("Subject", mime.QEncoding.Encode("UTF-8", subject))
	headers.Set("Message-ID", "<"+deliveryID+"@marvo.local>")
	headers.Set("MIME-Version", "1.0")
	headers.Set("Content-Type", "text/plain; charset=UTF-8")
	headers.Set("Content-Transfer-Encoding", "quoted-printable")
	for _, key := range []string{"Date", "From", "To", "Cc", "Subject", "Message-ID", "MIME-Version", "Content-Type", "Content-Transfer-Encoding"} {
		for _, value := range headers.Values(key) {
			fmt.Fprintf(&buffer, "%s: %s\r\n", key, value)
		}
	}
	buffer.WriteString("\r\n")
	quoted := quotedprintable.NewWriter(&buffer)
	_, err := quoted.Write([]byte(body))
	if closeErr := quoted.Close(); err == nil {
		err = closeErr
	}
	return buffer.Bytes(), err
}

func classifySMTPError(err error) error {
	if err == nil {
		return nil
	}
	var protocolError *textproto.Error
	permanent := errors.As(err, &protocolError) && protocolError.Code >= 500 && protocolError.Code < 600
	return &DeliveryError{Err: err, Permanent: permanent}
}

func permanentSMTPError(message string, err error) error {
	if err != nil {
		message += ": " + err.Error()
	}
	return &DeliveryError{Err: errors.New(message), Permanent: true}
}

func emailObjects(value string) []map[string]string {
	addresses := splitComma(value)
	result := make([]map[string]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, map[string]string{"email": address})
	}
	return result
}

func htmlEscapeWithBreaks(value string) string {
	var buffer bytes.Buffer
	writer := bufio.NewWriter(&buffer)
	for _, character := range value {
		switch character {
		case '&':
			_, _ = writer.WriteString("&amp;")
		case '<':
			_, _ = writer.WriteString("&lt;")
		case '>':
			_, _ = writer.WriteString("&gt;")
		case '"':
			_, _ = writer.WriteString("&quot;")
		case '\'':
			_, _ = writer.WriteString("&#39;")
		case '\n':
			_, _ = writer.WriteString("<br>")
		default:
			_, _ = writer.WriteRune(character)
		}
	}
	_ = writer.Flush()
	return buffer.String()
}
