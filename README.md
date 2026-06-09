# toolctl-sdk-go

`toolctl-sdk-go` is a Go translation of the Python `toolctl-sdk` server SDK for plain HTTP tool services.

It is designed for tool providers, not tool callers.

Features:

- `toolctl.Start()` to create a tool app quickly
- `toolctl.CreateApp()` as a direct constructor
- protocol-first JSON and SSE responses
- `RegisterTool()` for code-first tools
- `RegisterSSETool()` for code-first SSE tools
- built-in `/health`, `/tools`, `/openapi.json`, and `/docs`
- built-in `/tool-manifest.json` for agent/skill discovery
- optional resource heartbeat monitoring for scheduler integration
- optional Pub/Sub publisher and Vault credential helper for scheduler metrics

## Documentation

- [QUICK_TOOL_INTEGRATION.md](./QUICK_TOOL_INTEGRATION.md) - Chinese quick-start guide for exposing tools with the current SDK
- [TOOL_RESPONSE_PROTOCOL.md](./TOOL_RESPONSE_PROTOCOL.md) - protocol spec for JSON, SSE, and polling responses

## Quick start

```go
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
```

## Tool manifest

Every tool registered through the SDK is automatically added to the app registry.
Agents and skills should read this registry instead of inferring tool behavior from
OpenAPI, route names, or handwritten skill docs.

Configure a stable server name:

```go
app := toolctl.Start(toolctl.AppConfig{
	Title:      "Video Tools",
	ServerName: "video-tools",
	Version:    "0.1.0",
})
```

Read the manifest in process:

```go
manifest, ok := app.ToolManifest("ping")
if ok {
	fmt.Println(manifest.ServerName)
	fmt.Println(manifest.ResponseMode) // "json" or "sse"
	fmt.Println(manifest.IsSSE)
	fmt.Println(manifest.RequestSchema)
}
```

Or over HTTP:

```bash
curl http://127.0.0.1:8080/tool-manifest.json
```

The payload includes `server_name`, tool `description`, `request_schema`,
optional `response_schema`, `response_mode`, `is_sse`, `method`, `path`, tags,
timeout, and protocol mode. `/tools` returns the same per-tool metadata with
`server_name` for lightweight discovery.

Call it with:

```bash
curl -X POST "http://127.0.0.1:8080/tools/ping" \
  -H "Content-Type: application/json" \
  -d '{"message":"hello"}'
```

Response:

```json
{
  "type": "tool.completed",
  "tool": {
    "id": "task_xxx",
    "name": "ping",
    "status": "completed",
    "outputs": [],
    "metadata": {
      "result": {
        "ok": true,
        "payload": {
          "message": "hello"
        }
      }
    }
  }
}
```

For richer output items, return `toolctl.ToolResult`:

```go
Handler: func(_ context.Context, _ map[string]any) (any, error) {
	return toolctl.ToolResult{
		Outputs: []any{
			toolctl.FileOutput("video", "https://cdn.example.com/output.mp4", map[string]any{
				"content_type": "video/mp4",
				"duration_ms":  30000,
			}),
		},
		Usage:    map[string]any{"duration_ms": 6358},
		Metadata: map[string]any{"provider": "ffmpeg"},
	}, nil
},
```

## SSE tool

```go
app.MustRegisterSSETool(toolctl.RegisterSSEToolOptions{
	Name:        "stream_ping",
	Description: "Stream progress events.",
	RequestSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string"},
		},
		"required": []string{"message"},
	},
	Handler: func(_ context.Context, payload map[string]any, writer toolctl.StreamWriter) error {
		progress := 50
		if err := writer.Write(toolctl.InProgress(toolctl.InProgressOptions{
			ToolName: "stream_ping",
			TaskID:   "task_stream_ping",
			Progress: &progress,
			Message:  payload["message"].(string),
		})); err != nil {
		return err
	}
	return writer.Write(toolctl.Completed(toolctl.CompletedOptions{
		ToolName: "stream_ping",
		TaskID:   "task_stream_ping",
		Outputs:  []any{},
		Metadata: map[string]any{"result": "ok"},
	}))
	},
})
```

SSE streams are emitted with protocol events and end with `data: [DONE]`.

## Compatibility mode

Default behavior is `ProtocolMode: "strict"`.

If you still need raw legacy JSON temporarily:

```go
app.MustRegisterTool(toolctl.RegisterToolOptions{
	Name:          "legacy_ping",
	Description:   "Legacy behavior",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	ProtocolMode:  "passthrough",
	Handler: func(_ context.Context, _ map[string]any) (any, error) {
		return map[string]any{"ok": true}, nil
	},
})
```

## Resource monitoring

`toolctl-sdk-go` can publish comfy-agent style heartbeat payloads. The payload includes scheduling fields such as `id`, `ip`, `routes`, `send_time`, `machine_id`, `status`, `category`, `task_url`, `instance_group`, `express`, and `task_express_url`, plus CPU, memory, process, platform, and host metadata. The default interval is 5 seconds.

For an `App`, enable monitoring before `Run`:

```go
monitor, err := app.EnableResourceMonitoring(toolctl.EnableResourceMonitoringOptions{
	Publish: func(payload map[string]any) error {
		// Publish to Pub/Sub, Kafka, HTTP, logs, etc.
		return nil
	},
	PublishImmediately: true,
})
if err != nil {
	log.Fatal(err)
}
defer monitor.Stop(5 * time.Second)
```

When monitoring is enabled, app heartbeats report `MACHINE_STATUS_BUSY` while a tool request is active and `MACHINE_STATUS_IDLE` otherwise. Set `Enabled: toolctl.Bool(false)` to keep monitoring configured but inactive; disabled monitors do not start the heartbeat goroutine, publish metrics, or require a publisher.

You can also start a standalone monitor:

```go
monitor, err := toolctl.StartResourceMonitor(context.Background(), toolctl.StartResourceMonitorOptions{
	ServiceName:        "video-tools",
	Publish:            publishHeartbeat,
	PublishImmediately: true,
})
```

## Examples

- `examples/basic_app/main.go`
- `QUICK_TOOL_INTEGRATION.md`
