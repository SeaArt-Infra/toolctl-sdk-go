# Tool Response Protocol

统一的远程工具响应协议，参考 OpenAI Responses API 设计模式。

## 设计原则

1. **类型明确** - 每个响应必须有 `type` 字段标识事件类型
2. **状态清晰** - 终态事件（completed/failed）包含完整结果
3. **输出统一** - 所有产出物（图片/视频/音频）使用统一的 `outputs` 结构
4. **向后兼容** - Fabric 层做适配，支持老接口平滑迁移

## 协议概览

```text
┌─────────────────────────────────────────────────────────────────┐
│                    Tool Response Protocol                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Event Types:                                                    │
│  ├── tool.created        任务创建                                │
│  ├── tool.in_progress    任务处理中（可选，用于进度更新）          │
│  ├── tool.completed      任务完成（包含完整结果）                  │
│  ├── tool.failed         任务失败（包含错误信息）                  │
│  └── tool.cancelled      任务取消                                │
│                                                                  │
│  Response Modes:                                                 │
│  ├── JSON      单次请求，直接返回结果                             │
│  ├── SSE       流式返回，多个事件                                 │
│  └── Polling   异步任务，轮询获取结果                             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## 事件类型定义

### 1. tool.created

任务创建成功，返回任务 ID。

```json
{
  "type": "tool.created",
  "tool": {
    "id": "task_abc123",
    "name": "compose_video",
    "status": "created",
    "created_at": 1715000000
  }
}
```

### 2. tool.in_progress

任务处理中，可包含进度信息（可选事件）。

```json
{
  "type": "tool.in_progress",
  "tool": {
    "id": "task_abc123",
    "name": "compose_video",
    "status": "in_progress",
    "progress": 50,
    "message": "Processing video clips..."
  }
}
```

### 3. tool.completed

任务完成，**必须包含完整的输出结果**。

```json
{
  "type": "tool.completed",
  "tool": {
    "id": "task_abc123",
    "name": "compose_video",
    "status": "completed",
    "outputs": [
      {
        "type": "video",
        "url": "https://cdn.example.com/output.mp4",
        "content_type": "video/mp4",
        "size_bytes": 1234567,
        "duration_ms": 30000
      }
    ],
    "usage": {
      "duration_ms": 6358,
      "credits": 10
    },
    "metadata": {
      "request_id": "req_xyz",
      "provider": "ffmpeg"
    }
  }
}
```

### 4. tool.failed

任务失败，包含错误信息。

```json
{
  "type": "tool.failed",
  "tool": {
    "id": "task_abc123",
    "name": "compose_video",
    "status": "failed",
    "error": {
      "code": "INVALID_INPUT",
      "message": "Video URL is not accessible",
      "details": {
        "url": "https://...",
        "http_status": 404
      }
    }
  }
}
```

### 5. tool.cancelled

任务取消。

```json
{
  "type": "tool.cancelled",
  "tool": {
    "id": "task_abc123",
    "name": "compose_video",
    "status": "cancelled",
    "reason": "user_cancelled"
  }
}
```

## Output 结构定义

所有产出物使用统一的 Output 结构：

```json
{
  "type": "video | image | audio | 3d | file | text",
  "url": "https://...",
  "content_type": "video/mp4",
  "size_bytes": 1234567,
  "duration_ms": 30000,
  "width": 1920,
  "height": 1080,
  "format": "glb"
}
```

### Output Types

| Type | 描述 | 特定字段 |
|------|------|----------|
| `image` | 图片 | `width`, `height` |
| `video` | 视频 | `width`, `height`, `duration_ms`, `fps` |
| `audio` | 音频 | `duration_ms`, `sample_rate` |
| `3d` | 3D 模型 | `format` (`glb` / `obj` / `fbx`) |
| `file` | 通用文件 | `filename` |
| `text` | 文本内容 | `content`（内联文本） |

## SSE 流式协议

### 事件格式

```text
event: tool.created
data: {"type": "tool.created", "tool": {...}}

event: tool.in_progress
data: {"type": "tool.in_progress", "tool": {...}}

event: tool.completed
data: {"type": "tool.completed", "tool": {...}}

data: [DONE]
```

### 关键规则

1. **type 字段必须存在** - 每个事件必须有 `type` 字段
2. **终态事件包含完整数据** - `tool.completed` 必须包含所有 outputs
3. **不依赖事件顺序** - 客户端应以终态事件为准
4. **[DONE] 标记结束** - SSE 流以 `[DONE]` 结束

## JSON 单次响应

对于非流式接口，直接返回终态格式：

```json
{
  "type": "tool.completed",
  "tool": {
    "id": "task_abc123",
    "name": "generate_image",
    "status": "completed",
    "outputs": [...]
  }
}
```

## Polling 异步任务

### 创建任务

```json
POST /v1/generation
Response:
{
  "type": "tool.created",
  "tool": {
    "id": "task_abc123",
    "status": "created"
  }
}
```

### 查询状态

```json
GET /v1/generation/task/{task_id}
Response:
{
  "type": "tool.in_progress",
  "tool": {
    "id": "task_abc123",
    "status": "in_progress",
    "progress": 75
  }
}
```

也可能返回 `tool.completed` 或 `tool.failed`。

## 实现要求

### 远程服务端

所有远程工具必须返回符合协议的响应格式，包括：

1. **JSON 模式** - 直接返回终态事件
2. **SSE 模式** - 流式返回事件，最后一个事件必须是终态

### Fabric 端 (`http.py`)

SSE 处理逻辑：

```python
TOOL_TERMINAL_EVENTS = frozenset({
    "tool.completed",
    "tool.failed",
    "tool.cancelled",
})

final_result = None

for event in sse_stream:
    event_type = event.get("type")

    if event_type in TOOL_TERMINAL_EVENTS:
        final_result = event
        break

    if event_type == "tool.in_progress":
        continue

return final_result
```

### 内置工具 (`seaart.py`)

将 SDK 返回的 Task 对象转换为协议格式：

```python
def _task_to_response(task: Task, tool_name: str) -> dict:
    if task.status == "completed":
        return {
            "type": "tool.completed",
            "tool": {
                "id": task.id,
                "name": tool_name,
                "status": "completed",
                "outputs": [
                    {"type": _infer_output_type(url), "url": url}
                    for url in task.urls()
                ],
                "metadata": {"model": task.model}
            }
        }
    else:
        return {
            "type": "tool.failed",
            "tool": {
                "id": task.id,
                "name": tool_name,
                "status": "failed",
                "error": {"message": str(task.error)}
            }
        }
```

## 与 Agent 事件协议的关系

| 层级 | 协议 | 用途 |
|------|------|------|
| Agent 层 | OpenAI Responses API | Agent 与 Gateway 之间的通信 |
| Tool 层 | Tool Response Protocol | 远程工具与 Fabric 之间的通信 |

Tool 调用结果会被包装进 Agent 事件：

```json
{
  "type": "response.output_item.done",
  "item": {
    "type": "function_call",
    "name": "compose_video",
    "output": "{\"type\":\"tool.completed\",\"tool\":{...}}"
  }
}
```

## 示例：compose_video

### 请求

```json
POST /tools/compose_video
Content-Type: application/json

{
  "video_urls": ["https://..."],
  "audio_url": "https://...",
  "replace_audio": true
}
```

### SSE 响应（新协议）

```text
event: tool.created
data: {"type":"tool.created","tool":{"id":"task_123","name":"compose_video","status":"created"}}

event: tool.in_progress
data: {"type":"tool.in_progress","tool":{"id":"task_123","status":"in_progress","progress":50,"message":"Merging clips..."}}

event: tool.completed
data: {"type":"tool.completed","tool":{"id":"task_123","name":"compose_video","status":"completed","outputs":[{"type":"video","url":"https://cdn.example.com/output.mp4","content_type":"video/mp4"}],"usage":{"duration_ms":6358}}}

data: [DONE]
```

### JSON 响应（新协议）

```json
{
  "type": "tool.completed",
  "tool": {
    "id": "task_123",
    "name": "compose_video",
    "status": "completed",
    "outputs": [
      {
        "type": "video",
        "url": "https://cdn.example.com/output.mp4",
        "content_type": "video/mp4"
      }
    ]
  }
}
```
