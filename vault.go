package toolctl

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// VaultError is returned when vault credential retrieval fails.
type VaultError struct {
	msg string
}

func (e *VaultError) Error() string { return e.msg }

func vaultError(format string, args ...any) *VaultError {
	return &VaultError{msg: fmt.Sprintf(format, args...)}
}

var insecureHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, //nolint:gosec
	},
}

func vaultRequest(rawURL, token string) (map[string]any, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, vaultError("vault request build error: %v", err)
	}
	req.Header.Set("X-Vault-Token", token)
	resp, err := insecureHTTPClient.Do(req)
	if err != nil {
		return nil, vaultError("vault connection error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, vaultError("vault request failed [%d]: %s", resp.StatusCode, rawURL)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, vaultError("vault response is not valid JSON: %v", err)
	}
	return result, nil
}

func renewVaultToken(vaultURL, token string) {
	renewURL := strings.TrimRight(vaultURL, "/") + "/v1/auth/token/renew-self"
	req, err := http.NewRequest(http.MethodPost, renewURL, strings.NewReader("{}"))
	if err != nil {
		return
	}
	req.Header.Set("X-Vault-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := insecureHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// 403 means token has no renew permission — non-fatal
	}
}

// secretURL mirrors tool-scheduler's SecretURL logic:
// if VAULT_URL already has a path component, use it directly;
// otherwise append key_path from config.
func secretURL(vaultURL, keyPath string) (string, error) {
	trimmed := strings.TrimRight(vaultURL, "/")
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", vaultError("invalid VAULT_URL: %v", err)
	}
	if parsed.Path != "" && strings.Trim(parsed.Path, "/") != "" {
		return trimmed, nil
	}
	return trimmed + "/" + strings.TrimLeft(keyPath, "/"), nil
}

// GetCredentialsJSON fetches credentials_json from Vault using VAULT_URL and
// VAULT_TOKEN environment variables. Returns base64-encoded JSON suitable for
// passing to NewPubSubMetricsPublisher as CredentialsJSON.
func GetCredentialsJSON(keyPath string, renewPath string) (string, error) {
	vaultURL := strings.TrimSpace(os.Getenv("VAULT_URL"))
	vaultToken := strings.TrimSpace(os.Getenv("VAULT_TOKEN"))
	if vaultURL == "" || vaultToken == "" {
		return "", vaultError("VAULT_URL and VAULT_TOKEN environment variables are required")
	}

	if renewPath != "" {
		renewVaultToken(vaultURL, vaultToken)
	}

	rawURL, err := secretURL(vaultURL, keyPath)
	if err != nil {
		return "", err
	}

	data, err := vaultRequest(rawURL, vaultToken)
	if err != nil {
		return "", err
	}

	credJSON, ok := nestedString(data, "data", "data", "credentials_json")
	if !ok || credJSON == "" {
		return "", vaultError("credentials_json field not found in vault response")
	}

	return normalizeCredentialsJSON(credJSON)
}

// ProjectIDFromCredentialsJSON extracts the project_id from a credentials JSON
// string (raw JSON or base64-encoded).
func ProjectIDFromCredentialsJSON(value string) string {
	payload := strings.TrimSpace(value)
	if !strings.HasPrefix(payload, "{") {
		decoded, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return ""
		}
		payload = string(decoded)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return ""
	}
	if v, ok := m["project_id"].(string); ok {
		return v
	}
	return ""
}

func normalizeCredentialsJSON(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "{") {
		// raw JSON — base64-encode it
		return base64.StdEncoding.EncodeToString([]byte(trimmed)), nil
	}
	// already base64
	return trimmed, nil
}

func nestedString(m map[string]any, keys ...string) (string, bool) {
	var current any = m
	for _, key := range keys {
		mm, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		current = mm[key]
	}
	s, ok := current.(string)
	return s, ok
}
