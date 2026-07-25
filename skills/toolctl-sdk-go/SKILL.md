---
name: toolctl-sdk-go
description: Build and extend Go HTTP tool services with toolctl-sdk-go. Use when creating a toolctl App, registering JSON or SSE tools, returning structured tool outputs, exposing tool manifests, configuring protocol compatibility, or adding resource monitoring to a Go tool service.
---

# Toolctl Go SDK

Use `toolctl-sdk-go` to expose Go handlers as standard HTTP tool services. Provide a JSON Schema request body for every tool and use a stable `ServerName` for services discovered by schedulers, agents, and skills.

## Install

Install the current GitHub `main` revision in the consuming module:

```bash
go get github.com/SeaArt-Infra/toolctl-sdk-go@main
```

## Create A Tool Service

Create an application with `toolctl.Start` or `toolctl.CreateApp`, then register tools before calling `Run`.

```go
package main

import (
	"context"
	"log"

	toolctl "github.com/SeaArt-Infra/toolctl-sdk-go"
)

func main() {
	app := toolctl.Start(toolctl.AppConfig{
		Title:      "Video Tools",
		ServerName: "video-tools",
		Version:    "0.1.0",
	})

	app.MustRegisterTool(toolctl.RegisterToolOptions{
		Name:        "ping",
		Description: "Return the submitted message.",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{"type": "string"},
			},
			"required": []string{"message"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return map[string]any{"message": payload["message"]}, nil
		},
	})

	log.Fatal(app.Run("0.0.0.0", 8080))
}
```

Use `RegisterTool` when registration errors must be handled explicitly; use `MustRegisterTool` only when invalid configuration should stop startup. The default response is a strict `tool.completed` protocol event. Set `ProtocolMode: "passthrough"` only for temporary compatibility with callers that require raw JSON.

## Stream Progress Or Return Files

Use `MustRegisterSSETool` or `RegisterSSETool` for progress streams. Write protocol events with the supplied `StreamWriter`; the SDK sends the creation event and ends the stream with `data: [DONE]`.

```go
app.MustRegisterSSETool(toolctl.RegisterSSEToolOptions{
	Name:          "render",
	Description:   "Render a video with progress.",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	Handler: func(_ context.Context, _ map[string]any, writer toolctl.StreamWriter) error {
		progress := 50
		taskID := toolctl.NewTaskID()
		if err := writer.Write(toolctl.InProgress(toolctl.InProgressOptions{
			ToolName: "render",
			TaskID:   taskID,
			Progress: &progress,
		})); err != nil {
			return err
		}
		return writer.Write(toolctl.Completed(toolctl.CompletedOptions{
			ToolName: "render",
			TaskID:   taskID,
			Outputs:  []any{},
		}))
	},
})
```

Use one task ID consistently throughout a stream. For image, video, audio, or file results, return `toolctl.ToolResult` with `toolctl.FileOutput(...)` rather than putting media URLs in an unstructured map.

## Discover And Verify

Use `/health` to check process health, `/tools` for a lightweight tool list, and `/tool-manifest.json` for complete discovery metadata. In process, use `app.ToolManifest(name)`, `app.ToolsManifest()`, or `app.ServerManifest()`.

Run `go test ./...` before delivery. Start the service, POST a valid JSON body to `/tools/<name>`, verify JSON or SSE behavior as registered, and retrieve `/tool-manifest.json` to confirm that the schema, route, and protocol mode are discoverable.

## Monitor A Tool Service

Call `app.EnableResourceMonitoring(...)` before `Run` to bind a monitor to the app. Provide either `Publish` or a `MetricsPublisher`; stop it with `monitor.Stop(timeout)` during shutdown. Set `Enabled: toolctl.Bool(false)` to keep the monitor configured without starting its heartbeat goroutine or requiring a publisher.

Do not put real credentials, API tokens, or production-only endpoints in Go source code, tool descriptions, or skill examples.
