package toolctl

import (
	"context"
	"log"
	"time"
)

type HandlerFunc func(context.Context, map[string]any) (any, error)

type StreamHandlerFunc func(context.Context, map[string]any, StreamWriter) error

type StreamWriter interface {
	Write(any) error
}

type AuthConfig struct {
	Type       string            `json:"type"`
	Token      string            `json:"token,omitempty"`
	Key        string            `json:"key,omitempty"`
	HeaderName string            `json:"header_name,omitempty"`
	Prefix     string            `json:"prefix,omitempty"`
	Location   string            `json:"location,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	QueryParam string            `json:"query_param,omitempty"`
}

func (a AuthConfig) withDefaults() AuthConfig {
	if a.Type == "" {
		a.Type = "none"
	}
	if a.Prefix == "" {
		a.Prefix = "Bearer"
	}
	if a.Location == "" {
		a.Location = "header"
	}
	if a.Headers == nil {
		a.Headers = map[string]string{}
	}
	return a
}

type ToolSpec struct {
	Name           string
	Description    string
	RequestSchema  map[string]any
	Handler        HandlerFunc
	StreamHandler  StreamHandlerFunc
	Path           string
	Method         string
	Tags           []string
	ResponseSchema map[string]any
	Headers        map[string]string
	TimeoutMS      int
	Auth           AuthConfig
	VerifyTLS      bool
	ResponseMode   string
	ProtocolMode   string
}

func (s ToolSpec) Manifest(serverName string) ToolManifest {
	return ToolManifest{
		ServerName:     serverName,
		Name:           s.Name,
		Description:    s.Description,
		RequestSchema:  cloneMap(s.RequestSchema),
		ResponseSchema: cloneMap(s.ResponseSchema),
		Method:         s.Method,
		Path:           s.Path,
		Tags:           append([]string(nil), s.Tags...),
		TimeoutMS:      s.TimeoutMS,
		ResponseMode:   s.ResponseMode,
		IsSSE:          s.IsSSE(),
		ProtocolMode:   s.ProtocolMode,
	}
}

func (s ToolSpec) IsSSE() bool {
	return s.ResponseMode == "sse"
}

type ToolManifest struct {
	ServerName     string         `json:"server_name"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	RequestSchema  map[string]any `json:"request_schema"`
	ResponseSchema map[string]any `json:"response_schema,omitempty"`
	Method         string         `json:"method"`
	Path           string         `json:"path"`
	Tags           []string       `json:"tags,omitempty"`
	TimeoutMS      int            `json:"timeout_ms"`
	ResponseMode   string         `json:"response_mode"`
	IsSSE          bool           `json:"is_sse"`
	ProtocolMode   string         `json:"protocol_mode"`
}

type ServerManifest struct {
	ServerName  string         `json:"server_name"`
	Title       string         `json:"title"`
	Version     string         `json:"version"`
	Description string         `json:"description,omitempty"`
	BasePath    string         `json:"base_path,omitempty"`
	Tools       []ToolManifest `json:"tools"`
}

type ToolResult struct {
	Outputs  []any          `json:"outputs,omitempty"`
	Usage    map[string]any `json:"usage,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ToolOutput struct {
	Type        string         `json:"type"`
	URL         string         `json:"url,omitempty"`
	ContentType string         `json:"content_type,omitempty"`
	SizeBytes   *int64         `json:"size_bytes,omitempty"`
	DurationMS  *int           `json:"duration_ms,omitempty"`
	Width       *int           `json:"width,omitempty"`
	Height      *int           `json:"height,omitempty"`
	FPS         *float64       `json:"fps,omitempty"`
	SampleRate  *int           `json:"sample_rate,omitempty"`
	Format      string         `json:"format,omitempty"`
	Filename    string         `json:"filename,omitempty"`
	Content     string         `json:"content,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type AppConfig struct {
	Title       string
	ServerName  string
	Version     string
	Description string
	BasePath    string
	DocsURL     string
	OpenAPIURL  string
}

type RegisterToolOptions struct {
	Name           string
	Description    string
	RequestSchema  map[string]any
	Handler        HandlerFunc
	Method         string
	Path           string
	Tags           []string
	ResponseSchema map[string]any
	TimeoutMS      int
	Auth           *AuthConfig
	ResponseMode   string
	ProtocolMode   string
}

type RegisterSSEToolOptions struct {
	Name          string
	Description   string
	RequestSchema map[string]any
	Handler       StreamHandlerFunc
	Method        string
	Path          string
	Tags          []string
	TimeoutMS     int
	Auth          *AuthConfig
	ProtocolMode  string
}

type EnableResourceMonitoringOptions struct {
	Publisher          MetricsPublisher
	Publish            PublishFn
	Enabled            *bool
	Interval           time.Duration
	InstanceID         string
	Labels             map[string]string
	PublishImmediately bool
	Logger             *log.Logger
}

type CreatedOptions struct {
	ToolName  string
	TaskID    string
	CreatedAt int64
	Metadata  map[string]any
}

type InProgressOptions struct {
	ToolName string
	TaskID   string
	Progress *int
	Message  string
	Metadata map[string]any
}

type CompletedOptions struct {
	ToolName string
	TaskID   string
	Outputs  []any
	Usage    map[string]any
	Metadata map[string]any
}

type FailedOptions struct {
	ToolName string
	TaskID   string
	Code     string
	Message  string
	Details  map[string]any
	Metadata map[string]any
	Usage    map[string]any
}

type CancelledOptions struct {
	ToolName string
	TaskID   string
	Reason   string
}
