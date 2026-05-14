# Tool 快速接入说明（Go）

这份文档面向想用当前 `toolctl-sdk-go` 快速暴露一个 HTTP tool 的服务方。

当前 SDK 有 3 种常见接入方式：

1. 代码直注册：你自己写 handler，用 `MustRegisterTool()` / `RegisterTool()`
2. 代理已有 HTTP 服务：用 `MustRegisterProxyTool()` / `RegisterProxyTool()`
3. 从 OpenAPI 导入：用 `MustRegisterToolFromOpenAPI()` / `RegisterToolFromOpenAPI()`

如果你只是要最快跑起来，优先用第 1 种。

## 1. 安装

```bash
go get toolctl-sdk-go
```

如果你在本地仓库里联调，也可以直接：

```bash
go run ./examples/basic_app
```

## 2. 最短路径：注册一个本地 tool

```go
package main

import (
	"context"
	"log"

	toolctl "toolctl-sdk-go"
)

func main() {
	app := toolctl.Start(toolctl.AppConfig{
		Title:       "demo-tools",
		Version:     "0.1.0",
		Description: "Demo tool service",
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
		Tags: []string{"demo"},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return toolctl.ToolResult{
				Outputs:  []any{},
				Metadata: map[string]any{"echo": payload["message"]},
			}, nil
		},
	})

	log.Fatal(app.Run("127.0.0.1", 8080))
}
```

启动后可直接调用：

```bash
curl -X POST "http://127.0.0.1:8080/tools/ping" \
  -H "Content-Type: application/json" \
  -d '{"message":"hello"}'
```

返回值默认会被 SDK 包装成统一协议：

```json
{
  "type": "tool.completed",
  "tool": {
    "id": "task_xxx",
    "name": "ping",
    "status": "completed",
    "outputs": [],
    "metadata": {
      "echo": "hello"
    }
  }
}
```

## 3. handler 应该返回什么

最常见有 3 种返回方式：

### 3.1 返回普通 `map[string]any`

```go
app.MustRegisterTool(toolctl.RegisterToolOptions{
	Name:          "echo",
	Description:   "Echo payload",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	Handler: func(_ context.Context, payload map[string]any) (any, error) {
		return map[string]any{"ok": true, "payload": payload}, nil
	},
})
```

SDK 会自动包装成 `tool.completed`，并把原始结果放到 `tool.metadata.result`。

### 3.2 返回 `toolctl.ToolResult`

如果你的工具会产出图片、视频、音频、文件等，优先返回 `ToolResult`：

```go
app.MustRegisterTool(toolctl.RegisterToolOptions{
	Name:          "compose_video",
	Description:   "Compose a video.",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
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
})
```

### 3.3 直接返回协议事件

如果你已经自己构造好了协议，也可以直接返回：

```go
app.MustRegisterTool(toolctl.RegisterToolOptions{
	Name:          "ready_made_result",
	Description:   "Return a protocol event directly.",
	RequestSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	Handler: func(_ context.Context, _ map[string]any) (any, error) {
		return toolctl.Completed(toolctl.CompletedOptions{
			ToolName: "ready_made_result",
			TaskID:   "task_fixed",
			Outputs:  []any{},
			Metadata: map[string]any{"result": "ok"},
		}), nil
	},
})
```

## 4. 什么时候用 SSE

如果你的 tool 需要进度流，使用 `MustRegisterSSETool()` 或 `RegisterSSETool()`：

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

SSE 模式下，SDK 会：

1. 自动先发一个 `tool.created`
2. 透传你产出的中间事件
3. 在流结尾补 `data: [DONE]`

## 5. 代理一个已有 HTTP 服务

如果你已经有上游服务，不想重写 handler，可以直接代理：

```go
app.MustRegisterProxyTool(toolctl.RegisterProxyToolOptions{
	Name:         "get_video_metadata",
	Description:  "Proxy video metadata requests.",
	BaseURL:      "https://video.example.com",
	Path:         "/tools/get_video_metadata",
	UpstreamPath: "/metadata",
	RequestSchema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"video_url":  map[string]any{"type": "string"},
			"video_path": map[string]any{"type": "string"},
		},
	},
	Auth: &toolctl.AuthConfig{
		Type:  "bearer",
		Token: "YOUR_TOKEN",
	},
	RetryCount: 2,
	RetryDelay: 0.2,
})
```

说明：

1. 对外暴露路径就是 `Path`
2. 如果上游路径不同，可额外传 `UpstreamPath`
3. `Auth` 同时支持 `bearer`、`api_key`、自定义 header
4. `ResponseMode: "sse"` 时会代理上游 SSE

## 6. 从 OpenAPI 快速导入

如果上游已经有 OpenAPI，可以直接导一个 operation：

```go
app.MustRegisterToolFromOpenAPI(toolctl.RegisterToolFromOpenAPIOptions{
	Name:        "weather_lookup",
	BaseURL:     "https://api.example.com",
	SpecURL:     "https://api.example.com/openapi.json",
	OperationID: "weatherLookup",
})
```

也可以改用：

1. `Spec: ...` 直接传字典
2. `SpecPath: "openapi.json"` 从本地文件读取
3. `Path: "/weather", Method: "POST"` 按路径和方法定位 operation

导入后：

1. SDK 会提取该 operation 的 JSON request schema
2. 对外默认注册成 `/tools/<name>`
3. 实际请求会被转发到上游原始 path

## 7. 注册到 gateway

本地注册完 tool 之后，可以直接导出或提交注册载荷。

### 7.1 只导出 payload

```go
payloads := app.ExportGatewayPayloads(toolctl.ExportGatewayPayloadOptions{
	Provider: "demo",
	BaseURL:  "http://tools.example.com",
	Version:  "v1",
	Category: "general",
})
```

### 7.2 直接提交到 gateway

```go
verifyTLS := true
results, err := app.RegisterToGateway(context.Background(), toolctl.RegisterToGatewayOptions{
	GatewayURL: "https://gateway.example.com/v1/tools/register",
	Provider:   "demo",
	BaseURL:    "http://tools.example.com",
	GatewayAuth: &toolctl.AuthConfig{
		Type:  "bearer",
		Token: "GATEWAY_TOKEN",
	},
	VerifyTLS: &verifyTLS,
	RetryCount: 1,
})
if err != nil {
	log.Fatal(err)
}
_ = results
```

生成的 tool id 规则是：

```text
<provider>:<tool_name>:<version>
```

例如：

```text
demo:ping:v1
```

## 8. 默认暴露的系统接口

每个 `App` 默认会带这些接口：

1. `GET /health`
2. `GET /tools`
3. `GET /openapi.json`
4. `GET /docs`

其中：

1. `/tools` 用于查看当前已注册工具列表
2. `/openapi.json` 和 `/docs` 方便联调和自查

## 9. 当前 SDK 的几个接入约束

这是按当前实现整理的，不是泛化建议。

1. 运行时入参只支持 JSON object；请求体如果不是对象会返回 400 或协议失败
2. 当前运行时校验主要检查 `required` 字段是否缺失，不会完整执行 JSON Schema 校验
3. 默认 `ProtocolMode: "strict"`，返回会被包装成统一 tool 协议
4. 如果你还要兼容旧接口，可临时用 `ProtocolMode: "passthrough"`
5. `ResponseMode` 只支持 `"json"` 和 `"sse"`
6. `GET` 工具会从 query string 取参数；其他方法从 JSON body 取参数

## 10. 推荐接入顺序

如果你要尽快把一个 tool 接进来，建议按这个顺序：

1. 先用 `MustRegisterTool()` 写一个最小可跑通版本
2. 用 `curl` 验证 `/tools/<name>` 的协议响应
3. 确认是否需要 `ToolResult.Outputs`
4. 需要进度再切换成 `MustRegisterSSETool()`
5. 工具稳定后再调用 `RegisterToGateway()`

## 11. 一个完整的最小模板

```go
package main

import (
	"context"
	"log"

	toolctl "toolctl-sdk-go"
)

func main() {
	app := toolctl.Start(toolctl.AppConfig{
		Title:   "my-tools",
		Version: "0.1.0",
	})

	app.MustRegisterTool(toolctl.RegisterToolOptions{
		Name:        "my_tool",
		Description: "Describe what the tool does.",
		RequestSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{"type": "string"},
			},
			"required": []string{"input"},
		},
		Handler: func(_ context.Context, payload map[string]any) (any, error) {
			return toolctl.ToolResult{
				Outputs:  []any{},
				Metadata: map[string]any{"result": payload["input"]},
			}, nil
		},
	})

	log.Fatal(app.Run("0.0.0.0", 8080))
}
```
