package main

import (
	"context"
	"log"

	toolctl "toolctl-sdk-go"
)

func main() {
	app := toolctl.Start(toolctl.AppConfig{
		Title:   "video-tools",
		Version: "0.1.0",
	})

	app.MustRegisterTool(toolctl.RegisterToolOptions{
		Name:        "ping",
		Description: "Return the submitted payload.",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return map[string]any{"ok": true, "payload": payload}, nil
		},
	})

	log.Fatal(app.Run("127.0.0.1", 8080))
}
