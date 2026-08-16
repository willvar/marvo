package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/willvar/notix"
	"github.com/willvar/notix/providers"
)

type FieldType = notix.FieldType

const (
	FieldText     = notix.FieldText
	FieldSecret   = notix.FieldSecret
	FieldURL      = notix.FieldURL
	FieldNumber   = notix.FieldNumber
	FieldBoolean  = notix.FieldBoolean
	FieldSelect   = notix.FieldSelect
	FieldTextarea = notix.FieldTextarea
)

type FieldOption = notix.FieldOption
type Field = notix.Field
type Provider = notix.Provider
type DeliveryError = notix.DeliveryError

// Message is Marvo's Activity projection at the Notix boundary. Keeping this
// adapter in Marvo prevents Activity-specific terminology from entering the
// reusable connector SDK.
type Message struct {
	DeliveryID string
	ActivityID string
	Kind       string
	Title      string
	Content    string
	URL        string
	CreatedAt  time.Time
}

func (m Message) Text() string {
	parts := []string{strings.TrimSpace(m.Title), strings.TrimSpace(m.Content), strings.TrimSpace(m.URL)}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return strings.Join(result, "\n\n")
}

type Registry struct {
	core *notix.Registry
}

func NewRegistry(client *http.Client) *Registry {
	definitions := providers.DefinitionsWithOptions(providers.Options{
		Client: client, SourceName: "Marvo", EventName: "活动", EventKey: "activity",
		MessageIDDomain: "marvo.local", LegacyWebhookPayload: true,
	})
	selected := make([]notix.Provider, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Category != providers.CategoryIncident {
			selected = append(selected, definition)
		}
	}
	return &Registry{core: notix.MustRegistry(selected...)}
}

func (r *Registry) Catalog() []Provider {
	return r.core.Catalog()
}

func (r *Registry) Provider(id string) (Provider, bool) {
	return r.core.Provider(id)
}

func (r *Registry) Validate(providerID string, config map[string]any) (map[string]any, error) {
	normalized, err := r.core.Validate(providerID, config)
	if err == nil {
		return normalized, nil
	}
	return nil, r.localizeValidationError(providerID, err)
}

func (r *Registry) Send(ctx context.Context, providerID string, config map[string]any, message Message) error {
	normalized, err := r.Validate(providerID, config)
	if err != nil {
		return &notix.DeliveryError{Err: err, Permanent: true}
	}
	return r.core.Send(ctx, providerID, normalized, notix.Delivery{
		ID: message.DeliveryID,
		Event: notix.Event{
			ID: message.ActivityID, Type: message.Kind, Title: message.Title,
			Body: message.Content, Link: message.URL, CreatedAt: message.CreatedAt,
		},
	})
}

func (r *Registry) localizeValidationError(providerID string, err error) error {
	var validationError *notix.ValidationError
	if !errors.As(err, &validationError) {
		return err
	}
	provider, providerExists := r.core.Provider(providerID)
	if validationError.Code == notix.ValidationUnknownProvider || !providerExists {
		return fmt.Errorf("unknown connector provider %q", providerID)
	}
	if validationError.Code == notix.ValidationUnknownField {
		return fmt.Errorf("unsupported %s setting %q", provider.Name, validationError.FieldKey)
	}
	var target Field
	for _, field := range provider.Fields {
		if field.Key == validationError.FieldKey {
			target = field
			break
		}
	}
	label := target.Label
	if label == "" {
		label = validationError.FieldKey
	}
	switch validationError.Code {
	case notix.ValidationRequired:
		return fmt.Errorf("%s不能为空", label)
	case notix.ValidationInvalidChoice:
		return fmt.Errorf("%s选项无效", label)
	case notix.ValidationInvalidURL:
		return fmt.Errorf("%s必须是有效的 HTTP 或 HTTPS 地址", label)
	case notix.ValidationTooLong:
		return fmt.Errorf("%s过长", label)
	case notix.ValidationOutOfRange:
		return fmt.Errorf("%s超出允许范围", label)
	case notix.ValidationInvalidType:
		switch target.Type {
		case FieldBoolean:
			return fmt.Errorf("%s必须是开关值", label)
		case FieldNumber:
			return fmt.Errorf("%s必须是数字", label)
		default:
			return fmt.Errorf("%s必须是文本", label)
		}
	default:
		return err
	}
}

func (r *Registry) RedactError(providerID string, config map[string]any, message string) string {
	return strings.ReplaceAll(r.core.RedactError(providerID, config, message), "[redacted]", "[已隐藏]")
}

func IsPermanent(err error) bool { return notix.IsPermanent(err) }

func RetryAfter(err error) time.Duration { return notix.RetryAfter(err) }
