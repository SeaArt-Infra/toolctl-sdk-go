# Tool SDK 快速接入

这个 SDK 的职责是帮助 tool service 统一暴露工具能力和调度心跳：

1. 注册 JSON tool：`RegisterTool()` / `MustRegisterTool()`
2. 注册 SSE tool：`RegisterSSETool()` / `MustRegisterSSETool()`
3. 自动维护 tool registry，并通过 `/tool-manifest.json` 暴露
4. 统一 JSON/SSE tool 响应协议
5. 可选开启资源心跳，上报给调度系统

SDK 不负责注册 gateway，也不负责代理已有 HTTP 服务。平台侧应通过
`/tool-manifest.json` 主动发现 tool。

## 创建服务

```go
app := toolctl.Start(toolctl.AppConfig{
	Title:      "Video Tools",
	ServerName: "video-tools",
	Version:    "0.1.0",
})
```

`ServerName` 是调度、skill、agent 识别服务的稳定名称。未配置时默认使用
`Title`。

## 注册 JSON Tool

```go
app.MustRegisterTool(toolctl.RegisterToolOptions{
	Name:        "ping",
	Description: "Return the submitted payload.",
	RequestSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{
				"type":        "string",
				"description": "Message to echo.",
			},
		},
		"required": []string{"message"},
	},
	Handler: func(ctx context.Context, payload map[string]any) (any, error) {
		return map[string]any{"ok": true, "payload": payload}, nil
	},
})
```

字段描述放在 `request_schema.properties.<field>.description`，SDK 会原样暴露给
skill/agent。

## 注册 SSE Tool

```go
app.MustRegisterSSETool(toolctl.RegisterSSEToolOptions{
	Name:          "stream_ping",
	Description:   "Stream progress events.",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	Handler: func(ctx context.Context, payload map[string]any, writer toolctl.StreamWriter) error {
		progress := 50
		if err := writer.Write(toolctl.InProgress(toolctl.InProgressOptions{
			ToolName: "stream_ping",
			TaskID:   toolctl.NewTaskID(),
			Progress: &progress,
			Message:  "working",
		})); err != nil {
			return err
		}
		return writer.Write(toolctl.Completed(toolctl.CompletedOptions{
			ToolName: "stream_ping",
			TaskID:   toolctl.NewTaskID(),
			Outputs:  []any{},
			Metadata: map[string]any{"result": "ok"},
		}))
	},
})
```

## 发现 Tool

```bash
curl http://127.0.0.1:8080/tool-manifest.json
```

manifest 包含：

- `server_name`
- tool `name`
- `description`
- `request_schema`
- `response_schema`
- `response_mode`
- `is_sse`
- `method`
- `path`
- `timeout_ms`
- `protocol_mode`

## 调用 Tool

JSON tool：

```bash
curl -X POST http://127.0.0.1:8080/tools/ping \
  -H 'Content-Type: application/json' \
  -d '{"message":"hello"}'
```

SSE tool：

```bash
curl -N -X POST http://127.0.0.1:8080/tools/stream_ping \
  -H 'Content-Type: application/json' \
  -d '{}'
```

## 接入调度心跳

```go
monitor, err := app.EnableResourceMonitoring(toolctl.EnableResourceMonitoringOptions{
	Publish: func(payload map[string]any) error {
		// 发送到调度系统、Pub/Sub、Kafka、HTTP 等。
		return nil
	},
	PublishImmediately: true,
})
if err != nil {
	log.Fatal(err)
}
defer monitor.Stop(5 * time.Second)
```

开启后，SDK 会在 tool 请求执行期间上报 busy，空闲时上报 idle。调度系统可以基于
心跳进行实例选择和任务分发。
