package toolctl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"time"
)

var ToolTerminalEvents = map[string]struct{}{
	"tool.completed": {},
	"tool.failed":    {},
	"tool.cancelled": {},
}

var ToolEventTypes = map[string]struct{}{
	"tool.created":     {},
	"tool.in_progress": {},
	"tool.completed":   {},
	"tool.failed":      {},
	"tool.cancelled":   {},
}

var toolEventStatus = map[string]string{
	"tool.created":     "pending",
	"tool.in_progress": "in_progress",
	"tool.completed":   "completed",
	"tool.failed":      "failed",
	"tool.cancelled":   "cancelled",
}

func NewTaskID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("task_%d", time.Now().UnixNano())
	}
	return "task_" + hex.EncodeToString(raw[:])
}

func TextOutput(content string) map[string]any {
	return FileOutput("file", "data:text/plain;charset=utf-8,"+url.QueryEscape(content), map[string]any{
		"content_type": "text/plain",
		"filename":     "output.txt",
	})
}

func FileOutput(outputType, resourceURL string, extra map[string]any) map[string]any {
	result := map[string]any{
		"type": outputType,
		"url":  resourceURL,
	}
	for key, value := range extra {
		if value != nil {
			result[key] = value
		}
	}
	return result
}

func Created(opts CreatedOptions) map[string]any {
	tool := cleanMap(map[string]any{
		"id":       opts.TaskID,
		"name":     opts.ToolName,
		"status":   "pending",
		"metadata": opts.Metadata,
	})
	return map[string]any{"type": "tool.created", "tool": tool}
}

func InProgress(opts InProgressOptions) map[string]any {
	tool := cleanMap(map[string]any{
		"id":       opts.TaskID,
		"name":     opts.ToolName,
		"status":   "in_progress",
		"progress": opts.Progress,
		"message":  blankToNil(opts.Message),
		"metadata": opts.Metadata,
	})
	return map[string]any{"type": "tool.in_progress", "tool": tool}
}

func Completed(opts CompletedOptions) map[string]any {
	outputs := make([]any, 0, len(opts.Outputs))
	for _, output := range opts.Outputs {
		outputs = append(outputs, normalizeOutput(output))
	}
	tool := cleanMap(map[string]any{
		"id":       opts.TaskID,
		"name":     opts.ToolName,
		"status":   "completed",
		"outputs":  outputs,
		"usage":    opts.Usage,
		"metadata": opts.Metadata,
	})
	return map[string]any{"type": "tool.completed", "tool": tool}
}

func Failed(opts FailedOptions) map[string]any {
	return map[string]any{
		"type": "tool.failed",
		"tool": cleanMap(map[string]any{
			"id":     opts.TaskID,
			"name":   opts.ToolName,
			"status": "failed",
			"error": cleanMap(map[string]any{
				"code":    opts.Code,
				"message": opts.Message,
				"details": opts.Details,
			}),
			"metadata": opts.Metadata,
			"usage":    opts.Usage,
		}),
	}
}

func Cancelled(opts CancelledOptions) map[string]any {
	return map[string]any{
		"type": "tool.cancelled",
		"tool": cleanMap(map[string]any{
			"id":     opts.TaskID,
			"name":   opts.ToolName,
			"status": "cancelled",
			"reason": blankToNil(opts.Reason),
		}),
	}
}

func IsToolEvent(payload any) bool {
	event, ok := payload.(map[string]any)
	if !ok {
		return false
	}
	eventType, ok := event["type"].(string)
	if !ok {
		return false
	}
	if _, ok := ToolEventTypes[eventType]; !ok {
		return false
	}
	_, ok = event["tool"].(map[string]any)
	return ok
}

func EnsureToolEvent(payload map[string]any, toolName, taskID string) map[string]any {
	tool, _ := payload["tool"].(map[string]any)
	if tool == nil {
		tool = map[string]any{}
	}
	cloned := cloneMap(payload)
	clonedTool := cloneMap(tool)
	if _, ok := clonedTool["id"]; !ok {
		clonedTool["id"] = taskID
	}
	if _, ok := clonedTool["name"]; !ok {
		clonedTool["name"] = toolName
	}
	if eventType, ok := cloned["type"].(string); ok {
		if status, ok := toolEventStatus[eventType]; ok {
			clonedTool["status"] = status
		}
		if eventType == "tool.completed" {
			var normalizedOutputs []any
			switch outputs := clonedTool["outputs"].(type) {
			case []any:
				normalizedOutputs = make([]any, 0, len(outputs))
				for _, output := range outputs {
					normalizedOutputs = append(normalizedOutputs, normalizeOutput(output))
				}
			case nil:
				normalizedOutputs = []any{}
			}
			clonedTool["outputs"] = normalizedOutputs
		}
	}
	cloned["tool"] = clonedTool
	return cloned
}

func NormalizeJSONResult(result any, toolName, taskID string) map[string]any {
	if payload, ok := result.(map[string]any); ok && IsToolEvent(payload) {
		return EnsureToolEvent(payload, toolName, taskID)
	}
	if result == nil {
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  []any{},
			Metadata: map[string]any{"result": nil},
		})
	}
	if toolResult, ok := result.(ToolResult); ok {
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  toolResult.Outputs,
			Usage:    toolResult.Usage,
			Metadata: toolResult.Metadata,
		})
	}
	if toolResult, ok := result.(*ToolResult); ok && toolResult != nil {
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  toolResult.Outputs,
			Usage:    toolResult.Usage,
			Metadata: toolResult.Metadata,
		})
	}
	if text, ok := result.(string); ok {
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  []any{},
			Metadata: map[string]any{"result": text},
		})
	}
	if payload, ok := result.(map[string]any); ok {
		if _, hasOutputs := payload["outputs"]; hasOutputs {
			metadata := map[string]any{}
			if existing, ok := payload["metadata"].(map[string]any); ok {
				metadata = cloneMap(existing)
			}
			extra := map[string]any{}
			for key, value := range payload {
				if key == "outputs" || key == "usage" || key == "metadata" {
					continue
				}
				extra[key] = value
			}
			if len(extra) > 0 {
				metadata["result"] = extra
			}
			var usage map[string]any
			if rawUsage, ok := payload["usage"].(map[string]any); ok {
				usage = rawUsage
			}
			var outputs []any
			if rawOutputs, ok := payload["outputs"].([]any); ok {
				outputs = rawOutputs
			}
			if len(metadata) == 0 {
				metadata = nil
			}
			return Completed(CompletedOptions{
				ToolName: toolName,
				TaskID:   taskID,
				Outputs:  outputs,
				Usage:    usage,
				Metadata: metadata,
			})
		}
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  []any{},
			Metadata: map[string]any{"result": payload},
		})
	}
	if list, ok := toAnySlice(result); ok {
		return Completed(CompletedOptions{
			ToolName: toolName,
			TaskID:   taskID,
			Outputs:  []any{},
			Metadata: map[string]any{"result": list},
		})
	}
	return Completed(CompletedOptions{
		ToolName: toolName,
		TaskID:   taskID,
		Outputs:  []any{},
		Metadata: map[string]any{"result": result},
	})
}

func ProtocolResponseSchema() map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"type", "tool"},
		"properties": map[string]any{
			"type": map[string]any{
				"type": "string",
				"enum": []string{"tool.cancelled", "tool.completed", "tool.created", "tool.failed", "tool.in_progress"},
			},
			"tool": map[string]any{
				"type":     "object",
				"required": []string{"id", "name", "status"},
				"properties": map[string]any{
					"id":       map[string]any{"type": "string"},
					"name":     map[string]any{"type": "string"},
					"status":   map[string]any{"type": "string"},
					"progress": map[string]any{"type": "number"},
					"message":  map[string]any{"type": "string"},
					"outputs":  map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
					"usage":    map[string]any{"type": "object"},
					"metadata": map[string]any{"type": "object"},
					"error":    map[string]any{"type": "object"},
				},
			},
		},
	}
}

func normalizeOutput(output any) any {
	switch value := output.(type) {
	case ToolOutput:
		return structToMap(value)
	case *ToolOutput:
		if value == nil {
			return nil
		}
		return structToMap(*value)
	case map[string]any:
		if outputType, ok := value["type"].(string); ok && outputType == "text" {
			content, _ := value["content"].(string)
			extra := cloneMap(value)
			delete(extra, "type")
			delete(extra, "content")
			contentType, _ := extra["content_type"].(string)
			if contentType == "" {
				extra["content_type"] = "text/plain"
			}
			filename, _ := extra["filename"].(string)
			if filename == "" {
				extra["filename"] = "output.txt"
			}
			return FileOutput("file", "data:text/plain;charset=utf-8,"+url.QueryEscape(content), extra)
		}
		return cleanMap(value)
	default:
		return output
	}
}

func blankToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func trimEventPrefix(value string) string {
	const prefix = "tool."
	if len(value) > len(prefix) && value[:len(prefix)] == prefix {
		return value[len(prefix):]
	}
	return value
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}

func toAnySlice(value any) ([]any, bool) {
	if list, ok := value.([]any); ok {
		return list, true
	}
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() || (reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array) {
		return nil, false
	}
	result := make([]any, reflected.Len())
	for i := 0; i < reflected.Len(); i++ {
		result[i] = reflected.Index(i).Interface()
	}
	return result, true
}
