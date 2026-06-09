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
				Outputs:  []any{},
				Metadata: map[string]any{"result": "ok"},
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
	if !strings.Contains(body, `"type":"tool.created"`) {
		t.Fatalf("missing created event: %s", body)
	}
	if !strings.Contains(body, `"type":"tool.in_progress"`) {
		t.Fatalf("missing in_progress event: %s", body)
	}
	if !strings.Contains(body, `"type":"tool.completed"`) {
		t.Fatalf("missing completed event: %s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing done marker: %s", body)
	}
}

func TestToolManifestIncludesServerAndProtocolMetadata(t *testing.T) {
	app := Start(AppConfig{Title: "display-name", ServerName: "video-tools", Version: "0.1.0"})
	app.MustRegisterTool(RegisterToolOptions{
		Name:        "json_ping",
		Description: "JSON ping",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string", "description": "Message to echo"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return payload, nil
		},
	})
	app.MustRegisterSSETool(RegisterSSEToolOptions{
		Name:          "stream_ping",
		Description:   "Stream ping",
		RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handler: func(_ context.Context, _ map[string]any, writer StreamWriter) error {
			return writer.Write(Completed(CompletedOptions{ToolName: "stream_ping", TaskID: "task_1"}))
		},
	})

	manifest, ok := app.ToolManifest("stream_ping")
	if !ok {
		t.Fatal("expected stream_ping manifest")
	}
	if manifest.ServerName != "video-tools" {
		t.Fatalf("unexpected server_name: %q", manifest.ServerName)
	}
	if manifest.ResponseMode != "sse" || !manifest.IsSSE {
		t.Fatalf("unexpected SSE metadata: %#v", manifest)
	}
	if manifest.RequestSchema["type"] != "object" {
		t.Fatalf("unexpected request schema: %#v", manifest.RequestSchema)
	}

	req := httptest.NewRequest(http.MethodGet, "/tool-manifest.json", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["server_name"] != "video-tools" {
		t.Fatalf("unexpected server_name payload: %#v", body["server_name"])
	}
	tools := body["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	first := tools[0].(map[string]any)
	if first["name"] != "json_ping" {
		t.Fatalf("expected sorted tools, got %#v", first["name"])
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
