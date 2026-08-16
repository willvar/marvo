package connectors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type FieldType string

const (
	FieldText     FieldType = "text"
	FieldSecret   FieldType = "secret"
	FieldURL      FieldType = "url"
	FieldNumber   FieldType = "number"
	FieldBoolean  FieldType = "boolean"
	FieldSelect   FieldType = "select"
	FieldTextarea FieldType = "textarea"
)

type FieldOption struct {
	Label string `json:"label"`
	Value any    `json:"value"`
}

type Field struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        FieldType     `json:"type"`
	Required    bool          `json:"required"`
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Default     any           `json:"default,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
	Sensitive   bool          `json:"sensitive,omitempty"`
}

type Provider struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	Documentation string  `json:"documentation,omitempty"`
	Fields        []Field `json:"fields"`
	send          SendFunc
}

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
	parts := []string{strings.TrimSpace(m.Title), strings.TrimSpace(m.Content)}
	if strings.TrimSpace(m.URL) != "" {
		parts = append(parts, strings.TrimSpace(m.URL))
	}
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return strings.Join(result, "\n\n")
}

type SendFunc func(context.Context, map[string]any, Message) error

type DeliveryError struct {
	Err        error
	Permanent  bool
	RetryAfter time.Duration
}

func (e *DeliveryError) Error() string {
	if e == nil || e.Err == nil {
		return "connector delivery failed"
	}
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error { return e.Err }

func IsPermanent(err error) bool {
	var deliveryError *DeliveryError
	return errors.As(err, &deliveryError) && deliveryError.Permanent
}

func RetryAfter(err error) time.Duration {
	var deliveryError *DeliveryError
	if errors.As(err, &deliveryError) {
		return deliveryError.RetryAfter
	}
	return 0
}

type Registry struct {
	providers map[string]Provider
	ordered   []Provider
	client    *http.Client
}

func NewRegistry(client *http.Client) *Registry {
	if client == nil {
		client = defaultHTTPClient()
	}
	registry := &Registry{providers: make(map[string]Provider), client: client}
	registerProviders(registry)
	sort.Slice(registry.ordered, func(i, j int) bool {
		if registry.ordered[i].Category == registry.ordered[j].Category {
			return registry.ordered[i].Name < registry.ordered[j].Name
		}
		return registry.ordered[i].Category < registry.ordered[j].Category
	})
	return registry
}

func (r *Registry) Catalog() []Provider {
	result := make([]Provider, len(r.ordered))
	copy(result, r.ordered)
	for index := range result {
		result[index].send = nil
	}
	return result
}

func (r *Registry) Provider(id string) (Provider, bool) {
	provider, ok := r.providers[id]
	return provider, ok
}

func (r *Registry) Validate(providerID string, config map[string]any) (map[string]any, error) {
	provider, ok := r.providers[providerID]
	if !ok {
		return nil, fmt.Errorf("unknown connector provider %q", providerID)
	}
	if config == nil {
		config = map[string]any{}
	}
	allowed := make(map[string]Field, len(provider.Fields))
	for _, field := range provider.Fields {
		allowed[field.Key] = field
	}
	normalized := make(map[string]any, len(config))
	for key, value := range config {
		field, exists := allowed[key]
		if !exists {
			return nil, fmt.Errorf("unsupported %s setting %q", provider.Name, key)
		}
		normalizedValue, err := normalizeFieldValue(field, value)
		if err != nil {
			return nil, err
		}
		if normalizedValue != nil && normalizedValue != "" {
			normalized[key] = normalizedValue
		}
	}
	for _, field := range provider.Fields {
		if _, exists := normalized[field.Key]; !exists && field.Default != nil {
			normalized[field.Key] = field.Default
		}
		if field.Required && emptyValue(normalized[field.Key]) {
			return nil, fmt.Errorf("%s不能为空", field.Label)
		}
	}
	return normalized, nil
}

func (r *Registry) Send(ctx context.Context, providerID string, config map[string]any, message Message) error {
	provider, ok := r.providers[providerID]
	if !ok || provider.send == nil {
		return &DeliveryError{Err: errors.New("connector provider is unavailable"), Permanent: true}
	}
	normalized, err := r.Validate(providerID, config)
	if err != nil {
		return &DeliveryError{Err: err, Permanent: true}
	}
	return provider.send(ctx, normalized, message)
}

// RedactError removes configured credentials from errors before they are
// persisted or returned to a browser. Some remote services echo request data
// in an error response, and URL-based webhooks often carry credentials in the
// path or query string.
func (r *Registry) RedactError(providerID string, config map[string]any, message string) string {
	provider, ok := r.providers[providerID]
	if !ok || message == "" {
		return message
	}
	values := make([]string, 0)
	for _, field := range provider.Fields {
		if field.Type != FieldSecret && !field.Sensitive {
			continue
		}
		value, _ := config[field.Key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(left, right int) bool { return len(values[left]) > len(values[right]) })
	for _, value := range values {
		message = strings.ReplaceAll(message, value, "[已隐藏]")
	}
	return message
}

func (r *Registry) register(provider Provider) {
	if provider.ID == "" || provider.Name == "" || provider.send == nil {
		panic("invalid connector provider")
	}
	if _, exists := r.providers[provider.ID]; exists {
		panic("duplicate connector provider: " + provider.ID)
	}
	r.providers[provider.ID] = provider
	r.ordered = append(r.ordered, provider)
}

func normalizeFieldValue(field Field, value any) (any, error) {
	switch field.Type {
	case FieldBoolean:
		if boolean, ok := value.(bool); ok {
			return boolean, nil
		}
		return nil, fmt.Errorf("%s必须是开关值", field.Label)
	case FieldNumber:
		switch number := value.(type) {
		case float64:
			return number, nil
		case int:
			return float64(number), nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
			if err == nil {
				return parsed, nil
			}
		}
		return nil, fmt.Errorf("%s必须是数字", field.Label)
	default:
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s必须是文本", field.Label)
		}
		text = strings.TrimSpace(text)
		if len(text) > 64<<10 {
			return nil, fmt.Errorf("%s过长", field.Label)
		}
		if field.Type == FieldSelect && text != "" && len(field.Options) > 0 {
			valid := false
			for _, option := range field.Options {
				if fmt.Sprint(option.Value) == text {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("%s选项无效", field.Label)
			}
		}
		if field.Type == FieldURL && text != "" {
			parsed, err := url.Parse(text)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return nil, fmt.Errorf("%s必须是有效的 HTTP 或 HTTPS 地址", field.Label)
			}
		}
		return text, nil
	}
}

func emptyValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func stringValue(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return strings.TrimSpace(value)
}

func boolValue(config map[string]any, key string) bool {
	value, _ := config[key].(bool)
	return value
}

func intValue(config map[string]any, key string, fallback int) int {
	value, ok := config[key].(float64)
	if !ok {
		return fallback
	}
	return int(value)
}

func textField(key, label string, required bool) Field {
	return Field{Key: key, Label: label, Type: FieldText, Required: required}
}

func sensitiveTextField(key, label string, required bool) Field {
	return Field{Key: key, Label: label, Type: FieldText, Required: required, Sensitive: true}
}

func secretField(key, label string, required bool) Field {
	return Field{Key: key, Label: label, Type: FieldSecret, Required: required}
}

func urlField(key, label string, required bool) Field {
	return Field{Key: key, Label: label, Type: FieldURL, Required: required, Placeholder: "https://"}
}

func sensitiveURLField(key, label string, required bool) Field {
	return Field{
		Key: key, Label: label, Type: FieldURL, Required: required, Placeholder: "https://", Sensitive: true,
	}
}

func numberField(key, label string, required bool, fallback any) Field {
	return Field{Key: key, Label: label, Type: FieldNumber, Required: required, Default: fallback}
}

func booleanField(key, label string, fallback bool) Field {
	return Field{Key: key, Label: label, Type: FieldBoolean, Default: fallback}
}

func selectField(key, label string, required bool, fallback any, options ...FieldOption) Field {
	return Field{Key: key, Label: label, Type: FieldSelect, Required: required, Default: fallback, Options: options}
}

func option(label string, value any) FieldOption { return FieldOption{Label: label, Value: value} }
