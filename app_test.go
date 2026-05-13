package toolctl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterToolAndCallRoute(t *testing.T) {
	app := Start(AppConfig{Title: "test-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:        "ping",
		Description: "Ping",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Tags: []string{"demo"},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return map[string]any{"echo": payload["message"]}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/ping", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["type"] != "tool.completed" {
		t.Fatalf("unexpected event type: %#v", body["type"])
	}
	tool := body["tool"].(map[string]any)
	if tool["status"] != "completed" {
		t.Fatalf("unexpected status: %#v", tool["status"])
	}
	if tool["name"] != "ping" {
		t.Fatalf("unexpected name: %#v", tool["name"])
	}
	outputs := tool["outputs"].([]any)
	first := outputs[0].(map[string]any)
	if first["type"] != "text" {
		t.Fatalf("unexpected output type: %#v", first["type"])
	}
	metadata := tool["metadata"].(map[string]any)
	result := metadata["result"].(map[string]any)
	if result["echo"] != "hello" {
		t.Fatalf("unexpected echoed value: %#v", result["echo"])
	}
}

func TestMissingRequiredFieldReturnsProtocolFailure(t *testing.T) {
	app := Start(AppConfig{Title: "test-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:        "ping",
		Description: "Ping",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/ping", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "tool.failed" {
		t.Fatalf("unexpected event type: %#v", body["type"])
	}
	errBody := body["tool"].(map[string]any)["error"].(map[string]any)
	if errBody["code"] != "INVALID_INPUT" {
		t.Fatalf("unexpected error code: %#v", errBody["code"])
	}
	if !strings.Contains(errBody["message"].(string), "Missing required fields") {
		t.Fatalf("unexpected error message: %#v", errBody["message"])
	}
}

func TestRegisterProxyTool(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer upstream.Close()

	app := Start(AppConfig{Title: "proxy-tools", Version: "0.1.0"})
	app.MustRegisterProxyTool(RegisterProxyToolOptions{
		Name:        "video_metadata",
		Description: "Proxy metadata",
		BaseURL:     upstream.URL,
		Path:        "/tools/video_metadata",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"video_url": map[string]any{"type": "string"},
			},
		},
		Auth:       &AuthConfig{Type: "bearer", Token: "token-1"},
		RetryCount: 2,
		RetryDelay: 0.01,
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/video_metadata", strings.NewReader(`{"video_url":"https://example.com/test.mp4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if receivedAuth != "Bearer token-1" {
		t.Fatalf("unexpected auth header: %q", receivedAuth)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	result := body["tool"].(map[string]any)["metadata"].(map[string]any)["result"].(map[string]any)
	if result["success"] != true {
		t.Fatalf("unexpected proxy result: %#v", result["success"])
	}
}

func TestRegisterSSETool(t *testing.T) {
	app := Start(AppConfig{Title: "sse-tools", Version: "0.1.0"})
	app.MustRegisterSSETool(RegisterSSEToolOptions{
		Name:        "stream_ping",
		Description: "Stream ping events",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any, writer StreamWriter) error {
			progress := 50
			if err := writer.Write(InProgress(InProgressOptions{
				ToolName: "stream_ping",
				TaskID:   "task_stream_ping",
				Progress: &progress,
				Message:  payload["message"].(string),
			})); err != nil {
				return err
			}
			return writer.Write(Completed(CompletedOptions{
				ToolName: "stream_ping",
				TaskID:   "task_stream_ping",
				Outputs:  []any{TextOutput("ok")},
			}))
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/stream_ping", strings.NewReader(`{"message":"hello"}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/event-stream") {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: tool.in_progress") {
		t.Fatalf("missing in_progress event: %s", body)
	}
	if !strings.Contains(body, "event: tool.completed") {
		t.Fatalf("missing completed event: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing done marker: %s", body)
	}
}

func TestRegisterProxySSETool(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: tool.in_progress\ndata: {\"type\":\"tool.in_progress\",\"tool\":{\"id\":\"task_1\",\"name\":\"stream_video_status\",\"status\":\"in_progress\",\"progress\":50}}\n\n"))
		_, _ = w.Write([]byte("event: tool.completed\ndata: {\"type\":\"tool.completed\",\"tool\":{\"id\":\"task_1\",\"name\":\"stream_video_status\",\"status\":\"completed\",\"outputs\":[{\"type\":\"text\",\"content\":\"ok\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	app := Start(AppConfig{Title: "proxy-sse-tools", Version: "0.1.0"})
	app.MustRegisterProxyTool(RegisterProxyToolOptions{
		Name:          "stream_video_status",
		Description:   "Proxy video status stream",
		BaseURL:       upstream.URL,
		Path:          "/tools/stream_video_status",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		ResponseMode:  "sse",
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/stream_video_status", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: tool.in_progress") {
		t.Fatalf("missing in_progress event: %s", body)
	}
	if !strings.Contains(body, "event: tool.completed") {
		t.Fatalf("missing completed event: %s", body)
	}
}

func TestProxyUpstreamErrorMapsToProtocolFailure(t *testing.T) {
	app := Start(AppConfig{Title: "proxy-tools", Version: "0.1.0"})
	app.MustRegisterProxyTool(RegisterProxyToolOptions{
		Name:          "video_metadata",
		Description:   "Proxy metadata",
		BaseURL:       "http://127.0.0.1:1",
		Path:          "/tools/video_metadata",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/video_metadata", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	errBody := body["tool"].(map[string]any)["error"].(map[string]any)
	if errBody["code"] != "UPSTREAM_REQUEST_FAILED" {
		t.Fatalf("unexpected error code: %#v", errBody["code"])
	}
}

func TestRegisterToolSupportsExplicitProtocolResult(t *testing.T) {
	app := Start(AppConfig{Title: "protocol-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:          "compose_video",
		Description:   "Compose video",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return ToolResult{
				Outputs: []any{
					FileOutput("video", "https://cdn.example.com/output.mp4", map[string]any{
						"content_type": "video/mp4",
						"duration_ms":  30000,
					}),
				},
				Usage:    map[string]any{"duration_ms": 6358},
				Metadata: map[string]any{"provider": "ffmpeg"},
			}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/compose_video", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	tool := body["tool"].(map[string]any)
	outputs := tool["outputs"].([]any)
	first := outputs[0].(map[string]any)
	if first["type"] != "video" {
		t.Fatalf("unexpected output type: %#v", first["type"])
	}
	usage := tool["usage"].(map[string]any)
	if usage["duration_ms"].(float64) != 6358 {
		t.Fatalf("unexpected usage: %#v", usage["duration_ms"])
	}
}

func TestRegisterToolPassthroughModeKeepsLegacyResponse(t *testing.T) {
	app := Start(AppConfig{Title: "legacy-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:          "legacy_ping",
		Description:   "Legacy ping",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		ProtocolMode:  "passthrough",
		Handler: func(_ context.Context, _ map[string]any) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/legacy_ping", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"ok":true}` {
		t.Fatalf("unexpected passthrough body: %s", rec.Body.String())
	}
}

func TestOpenAPIContainsRegisteredToolSchema(t *testing.T) {
	app := Start(AppConfig{Title: "openapi-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:        "ping",
		Description: "Ping",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	paths := body["paths"].(map[string]any)
	route := paths["/tools/ping"].(map[string]any)["post"].(map[string]any)
	if route["operationId"] != "ping" {
		t.Fatalf("unexpected operation id: %#v", route["operationId"])
	}
	requestSchema := route["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	required := requestSchema["required"].([]any)
	if required[0].(string) != "message" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
	responseSchema := route["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	requiredResponse := responseSchema["required"].([]any)
	if len(requiredResponse) != 2 || requiredResponse[0].(string) != "type" || requiredResponse[1].(string) != "tool" {
		t.Fatalf("unexpected response schema required fields: %#v", requiredResponse)
	}
}

func TestExportGatewayPayloads(t *testing.T) {
	app := Start(AppConfig{Title: "gateway-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:          "ping",
		Description:   "Ping",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Tags:          []string{"demo"},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
	})

	payloads := app.ExportGatewayPayloads(ExportGatewayPayloadOptions{
		Provider: "demo",
		BaseURL:  "http://tools.example.com",
		Version:  "v1",
		Category: "general",
	})

	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	if payloads[0]["id"] != "demo:ping:v1" {
		t.Fatalf("unexpected payload id: %#v", payloads[0]["id"])
	}
	if payloads[0]["endpoint"] != "http://tools.example.com/tools/ping" {
		t.Fatalf("unexpected endpoint: %#v", payloads[0]["endpoint"])
	}
}

func TestRegisterToGateway(t *testing.T) {
	var receivedAuth string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"name":"ping"}}`))
	}))
	defer server.Close()

	app := Start(AppConfig{Title: "gateway-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:          "ping",
		Description:   "Ping",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
	})

	verifyTLS := false
	results, err := app.RegisterToGateway(context.Background(), RegisterToGatewayOptions{
		GatewayURL:  server.URL,
		Provider:    "demo",
		BaseURL:     "http://tools.example.com",
		GatewayAuth: &AuthConfig{Type: "bearer", Token: "abc"},
		VerifyTLS:   &verifyTLS,
		RetryCount:  1,
	})
	if err != nil {
		t.Fatalf("register to gateway: %v", err)
	}
	if results[0].Status != http.StatusOK {
		t.Fatalf("unexpected status: %d", results[0].Status)
	}
	if receivedAuth != "Bearer abc" {
		t.Fatalf("unexpected auth header: %q", receivedAuth)
	}
}
