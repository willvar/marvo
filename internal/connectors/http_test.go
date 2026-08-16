package connectors

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPFailureClassificationAndRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		retryAfter string
		permanent  bool
		minimum    time.Duration
	}{
		{name: "invalid request", status: http.StatusBadRequest, permanent: true},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "120", minimum: 2 * time.Minute},
		{name: "server failure", status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: test.status,
					Status:     http.StatusText(test.status),
					Header:     http.Header{"Retry-After": []string{test.retryAfter}},
					Body:       io.NopCloser(strings.NewReader("rejected")),
					Request:    request,
				}, nil
			})}
			registry := NewRegistry(client)
			err := registry.Send(context.Background(), "webhook", map[string]any{
				"url": "https://example.test/hook", "method": "POST", "content_type": "json",
			}, Message{Title: "测试"})
			if err == nil || IsPermanent(err) != test.permanent {
				t.Fatalf("Send() error = %v, permanent = %t", err, IsPermanent(err))
			}
			if delay := RetryAfter(err); delay < test.minimum {
				t.Fatalf("RetryAfter() = %s, want at least %s", delay, test.minimum)
			}
		})
	}
}
