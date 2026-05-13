package toolctl

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BuildGatewayRegistrationPayloadOptions struct {
	Provider  string
	BaseURL   string
	Version   string
	Category  string
	Auth      map[string]any
	Enabled   bool
	OwnerID   string
	CreatedBy string
	TimeoutMS *int
}

type RegisterToolsToGatewayOptions struct {
	GatewayURL     string
	Payloads       []map[string]any
	Auth           AuthConfig
	VerifyTLS      bool
	TimeoutSeconds float64
	RetryCount     int
	RetryDelay     float64
}

func BuildGatewayRegistrationPayload(spec ToolSpec, opts BuildGatewayRegistrationPayloadOptions) map[string]any {
	base, _ := url.Parse(opts.BaseURL)
	ref, _ := url.Parse(spec.Path)
	endpoint := ""
	if base != nil && ref != nil {
		endpoint = base.ResolveReference(ref).String()
	}
	version := opts.Version
	if version == "" {
		version = "v1"
	}
	category := opts.Category
	if category == "" {
		category = "general"
	}
	timeoutMS := spec.TimeoutMS
	if opts.TimeoutMS != nil {
		timeoutMS = *opts.TimeoutMS
	}
	ownerID := opts.OwnerID
	if ownerID == "" {
		ownerID = opts.Provider
	}
	createdBy := opts.CreatedBy
	if createdBy == "" {
		createdBy = opts.Provider
	}
	auth := opts.Auth
	if auth == nil {
		auth = map[string]any{"type": "none"}
	}
	return map[string]any{
		"id":          fmt.Sprintf("%s:%s:%s", opts.Provider, spec.Name, version),
		"provider":    opts.Provider,
		"name":        spec.Name,
		"version":     version,
		"category":    category,
		"transport":   "http",
		"description": spec.Description,
		"endpoint":    endpoint,
		"method":      spec.Method,
		"parameters":  spec.RequestSchema,
		"auth":        auth,
		"config":      map[string]any{"timeout_ms": timeoutMS},
		"tags":        spec.Tags,
		"enabled":     opts.Enabled,
		"owner_id":    ownerID,
		"created_by":  createdBy,
	}
}

func RegisterToolsToGateway(ctx context.Context, opts RegisterToolsToGatewayOptions) ([]GatewayRegistrationResult, error) {
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 30
	}
	client := &http.Client{Timeout: time.Duration(timeoutSeconds * float64(time.Second))}
	if !opts.VerifyTLS {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, //nolint:gosec
		}
	}
	headers := applyAuthHeaders(map[string]string{"Content-Type": "application/json"}, opts.Auth.withDefaults())
	queryParams := authQueryParams(opts.Auth.withDefaults())

	results := make([]GatewayRegistrationResult, 0, len(opts.Payloads))
	for _, payload := range opts.Payloads {
		attempts := opts.RetryCount + 1
		var lastErr error
		for attempt := 0; attempt < attempts; attempt++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return nil, err
			}
			endpoint, err := addQueryParams(opts.GatewayURL, queryParams)
			if err != nil {
				return nil, err
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
			if err != nil {
				return nil, err
			}
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			resp, err := client.Do(req)
			if err == nil {
				responseBody, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					return nil, readErr
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					return nil, &GatewayRegistrationError{
						Message: fmt.Sprintf("gateway registration failed for %v: HTTP %d %s", payload["name"], resp.StatusCode, strings.TrimSpace(string(responseBody))),
					}
				}
				var parsed any
				if json.Unmarshal(responseBody, &parsed) != nil {
					parsed = strings.TrimSpace(string(responseBody))
				}
				results = append(results, GatewayRegistrationResult{
					Name:   fmt.Sprint(payload["name"]),
					Status: resp.StatusCode,
					Body:   parsed,
				})
				lastErr = nil
				break
			}
			lastErr = &GatewayRegistrationError{
				Message: fmt.Sprintf("gateway registration network failure for %v: %v", payload["name"], err),
			}
			if attempt < attempts-1 {
				if err := sleepWithContext(ctx, opts.RetryDelay); err != nil {
					return nil, err
				}
			}
		}
		if lastErr != nil {
			return nil, lastErr
		}
	}
	return results, nil
}

func addQueryParams(rawURL string, params map[string]string) (string, error) {
	if len(params) == 0 {
		return rawURL, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
