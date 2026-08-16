package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxResponseBody = 64 << 10
	requestTimeout  = 20 * time.Second
)

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			ResponseHeaderTimeout: 15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
		},
	}
}

func (r *Registry) sendJSON(ctx context.Context, method, target string, headers map[string]string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return &DeliveryError{Err: err, Permanent: true}
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if headers["Content-Type"] == "" {
		headers["Content-Type"] = "application/json"
	}
	return r.sendRequest(ctx, method, target, headers, bytes.NewReader(body))
}

func (r *Registry) sendForm(ctx context.Context, method, target string, headers map[string]string, values url.Values) error {
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	return r.sendRequest(ctx, method, target, headers, strings.NewReader(values.Encode()))
}

func (r *Registry) sendRequest(ctx context.Context, method, target string, headers map[string]string, body io.Reader) error {
	_, err := r.doRequest(ctx, method, target, headers, body)
	return err
}

func (r *Registry) requestJSON(ctx context.Context, method, target string, headers map[string]string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, &DeliveryError{Err: err, Permanent: true}
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if headers["Content-Type"] == "" {
		headers["Content-Type"] = "application/json"
	}
	return r.doRequest(ctx, method, target, headers, bytes.NewReader(body))
}

func (r *Registry) doRequest(ctx context.Context, method, target string, headers map[string]string, body io.Reader) ([]byte, error) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, &DeliveryError{Err: errors.New("invalid connector URL"), Permanent: true}
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, &DeliveryError{Err: err, Permanent: true}
	}
	request.Header.Set("User-Agent", "Marvo-Connector/1")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := r.client.Do(request)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) && requestError.Err != nil {
			err = requestError.Err
		}
		return nil, &DeliveryError{Err: fmt.Errorf("connector request failed: %w", err)}
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBody))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return responseBody, nil
	}
	message := strings.TrimSpace(string(responseBody))
	if message == "" {
		message = response.Status
	} else {
		message = fmt.Sprintf("%s: %s", response.Status, message)
	}
	permanent := response.StatusCode >= 400 && response.StatusCode < 500 &&
		response.StatusCode != http.StatusRequestTimeout && response.StatusCode != http.StatusTooManyRequests
	return nil, &DeliveryError{
		Err: errors.New(message), Permanent: permanent,
		RetryAfter: parseRetryAfter(response.Header.Get("Retry-After"), time.Now()),
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if retryAt, err := http.ParseTime(value); err == nil && retryAt.After(now) {
		return retryAt.Sub(now)
	}
	return 0
}

func basicAuth(username, password string) string {
	request, _ := http.NewRequest(http.MethodGet, "http://localhost", nil)
	request.SetBasicAuth(username, password)
	return request.Header.Get("Authorization")
}
