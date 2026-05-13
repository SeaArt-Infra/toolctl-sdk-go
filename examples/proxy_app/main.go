package main

import (
	"log"

	toolctl "toolctl-sdk-go"
)

func main() {
	app := toolctl.Start(toolctl.AppConfig{
		Title:   "proxy-tools",
		Version: "0.1.0",
	})

	app.MustRegisterProxyTool(toolctl.RegisterProxyToolOptions{
		Name:         "video_metadata",
		Description:  "Proxy video metadata requests to an upstream HTTP API.",
		BaseURL:      "http://127.0.0.1:9000",
		Path:         "/tools/video_metadata",
		UpstreamPath: "/metadata",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"video_url": map[string]any{"type": "string"},
			},
			"required": []string{"video_url"},
		},
	})

	log.Fatal(app.Run("127.0.0.1", 8080))
}
