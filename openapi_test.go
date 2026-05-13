package toolctl

import "testing"

func TestRegisterToolFromOpenAPI(t *testing.T) {
	app := Start(AppConfig{Title: "import-tools", Version: "0.1.0"})
	tool := app.MustRegisterToolFromOpenAPI(RegisterToolFromOpenAPIOptions{
		Name:    "weather_lookup",
		BaseURL: "https://api.example.com",
		Spec: map[string]any{
			"openapi": "3.0.0",
			"paths": map[string]any{
				"/weather": map[string]any{
					"post": map[string]any{
						"operationId": "weatherLookup",
						"summary":     "Lookup weather",
						"requestBody": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"city": map[string]any{"type": "string"},
										},
										"required": []string{"city"},
									},
								},
							},
						},
					},
				},
			},
		},
		OperationID: "weatherLookup",
	})

	if tool.Name != "weather_lookup" {
		t.Fatalf("unexpected tool name: %q", tool.Name)
	}
	if tool.Path != "/tools/weather_lookup" {
		t.Fatalf("unexpected path: %q", tool.Path)
	}
	if tool.UpstreamPath != "/weather" {
		t.Fatalf("unexpected upstream path: %q", tool.UpstreamPath)
	}
	required := tool.RequestSchema["required"].([]string)
	if len(required) != 1 || required[0] != "city" {
		t.Fatalf("unexpected required schema: %#v", required)
	}
}
