package toolctl

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type CallUpstreamToolOptions struct {
	BaseURL    string
	Path       string
	Method     string
	Payload    map[string]any
	Headers    map[string]string
	TimeoutMS  int
	Auth       AuthConfig
	RetryCount int
	RetryDelay float64
	VerifyTLS  bool
}

type StreamUpstreamToolOptions struct {
	BaseURL    string
	Path       string
	Method     string
	Payload    map[string]any
	Headers    map[string]string
	TimeoutMS  int
	Auth       AuthConfig
	RetryCount int
	RetryDelay float64
	VerifyTLS  bool
}

func CallUpstreamTool(ctx context.Context, opts CallUpstreamToolOptions) (any, error) {
	client := buildHTTPClient(opts.TimeoutMS, opts.VerifyTLS)
	method := strings.ToUpper(opts.Method)
	if method == "" {
		method = http.MethodPost
	}
	attempts := opts.RetryCount + 1
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := buildUpstreamRequest(ctx, buildUpstreamRequestOptions{
			BaseURL: opts.BaseURL,
			Path:    opts.Path,
			Method:  method,
			Payload: opts.Payload,
			Headers: opts.Headers,
			Auth:    opts.Auth,
		})
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			body, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				return nil, &UpstreamNetworkError{Message: fmt.Sprintf("upstream request failed: %v", readErr)}
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return nil, &UpstreamHTTPError{Message: fmt.Sprintf("upstream request failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))}
			}
			if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
				var payload any
				if err := json.Unmarshal(body, &payload); err == nil {
					return payload, nil
				}
			}
			return string(body), nil
		}
		if !isRetryable(err) || attempt == attempts-1 {
			return nil, wrapUpstreamError(err)
		}
		if err := sleepWithContext(ctx, opts.RetryDelay); err != nil {
			return nil, err
		}
	}
	return nil, &UpstreamRequestError{Message: "upstream request failed"}
}

func StreamUpstreamTool(ctx context.Context, opts StreamUpstreamToolOptions, emit func(string) error) error {
	client := buildHTTPClient(opts.TimeoutMS, opts.VerifyTLS)
	method := strings.ToUpper(opts.Method)
	if method == "" {
		method = http.MethodPost
	}
	attempts := opts.RetryCount + 1
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := buildUpstreamRequest(ctx, buildUpstreamRequestOptions{
			BaseURL: opts.BaseURL,
			Path:    opts.Path,
			Method:  method,
			Payload: opts.Payload,
			Headers: mergeStringMaps(opts.Headers, map[string]string{"Accept": "text/event-stream"}),
			Auth:    opts.Auth,
		})
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				body, _ := io.ReadAll(resp.Body)
				return &UpstreamHTTPError{Message: fmt.Sprintf("upstream request failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))}
			}
			buffer := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buffer)
				if n > 0 {
					if emitErr := emit(string(buffer[:n])); emitErr != nil {
						return emitErr
					}
				}
				if errors.Is(readErr, io.EOF) {
					return nil
				}
				if readErr != nil {
					return &UpstreamNetworkError{Message: fmt.Sprintf("upstream request failed: %v", readErr)}
				}
			}
		}
		if !isRetryable(err) || attempt == attempts-1 {
			return wrapUpstreamError(err)
		}
		if err := sleepWithContext(ctx, opts.RetryDelay); err != nil {
			return err
		}
	}
	return &UpstreamRequestError{Message: "upstream request failed"}
}

type buildUpstreamRequestOptions struct {
	BaseURL string
	Path    string
	Method  string
	Payload map[string]any
	Headers map[string]string
	Auth    AuthConfig
}

func buildUpstreamRequest(ctx context.Context, opts buildUpstreamRequestOptions) (*http.Request, error) {
	base, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, &UpstreamRequestError{Message: err.Error()}
	}
	ref, err := url.Parse(opts.Path)
	if err != nil {
		return nil, &UpstreamRequestError{Message: err.Error()}
	}
	fullURL := base.ResolveReference(ref)
	query := fullURL.Query()
	for key, value := range authQueryParams(opts.Auth.withDefaults()) {
		query.Set(key, value)
	}
	if strings.EqualFold(opts.Method, http.MethodGet) {
		for key, value := range opts.Payload {
			switch typed := value.(type) {
			case string:
				query.Set(key, typed)
			default:
				query.Set(key, fmt.Sprint(typed))
			}
		}
		fullURL.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, opts.Method, fullURL.String(), nil)
		if err != nil {
			return nil, &UpstreamRequestError{Message: err.Error()}
		}
		for key, value := range applyAuthHeaders(opts.Headers, opts.Auth.withDefaults()) {
			req.Header.Set(key, value)
		}
		return req, nil
	}
	fullURL.RawQuery = query.Encode()
	body, err := json.Marshal(opts.Payload)
	if err != nil {
		return nil, &UpstreamRequestError{Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, opts.Method, fullURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, &UpstreamRequestError{Message: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range applyAuthHeaders(opts.Headers, opts.Auth.withDefaults()) {
		req.Header.Set(key, value)
	}
	return req, nil
}

func buildHTTPClient(timeoutMS int, verifyTLS bool) *http.Client {
	if timeoutMS == 0 {
		timeoutMS = 30000
	}
	if !verifyTLS {
		return &http.Client{
			Timeout: time.Duration(timeoutMS) * time.Millisecond,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, //nolint:gosec
			},
		}
	}
	return &http.Client{Timeout: time.Duration(timeoutMS) * time.Millisecond}
}

func wrapUpstreamError(err error) error {
	if isTimeoutError(err) {
		return &UpstreamTimeoutError{Message: fmt.Sprintf("upstream request timed out: %v", err)}
	}
	return &UpstreamNetworkError{Message: fmt.Sprintf("upstream request failed: %v", err)}
}

func isRetryable(err error) bool {
	return isTimeoutError(err) || isNetError(err)
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isNetError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

func applyAuthHeaders(headers map[string]string, auth AuthConfig) map[string]string {
	merged := cloneStringMap(headers)
	auth = auth.withDefaults()
	switch auth.Type {
	case "bearer":
		if auth.Token != "" {
			header := auth.HeaderName
			if header == "" {
				header = "Authorization"
			}
			merged[header] = auth.Prefix + " " + auth.Token
		}
	case "api_key":
		if auth.Location == "header" && auth.Key != "" {
			header := auth.HeaderName
			if header == "" {
				header = "X-API-Key"
			}
			merged[header] = auth.Key
		}
	case "headers", "custom":
		for key, value := range auth.Headers {
			merged[key] = value
		}
	}
	return merged
}

func authQueryParams(auth AuthConfig) map[string]string {
	auth = auth.withDefaults()
	if auth.Type == "api_key" && auth.Location == "query" && auth.Key != "" {
		key := auth.QueryParam
		if key == "" {
			key = "api_key"
		}
		return map[string]string{key: auth.Key}
	}
	return map[string]string{}
}

func mergeStringMaps(left, right map[string]string) map[string]string {
	result := cloneStringMap(left)
	for key, value := range right {
		result[key] = value
	}
	return result
}

func sleepWithContext(ctx context.Context, seconds float64) error {
	if seconds <= 0 {
		return nil
	}
	timer := time.NewTimer(time.Duration(seconds * float64(time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
