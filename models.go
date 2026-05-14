package toolctl

import (
	"context"
	"log"
	"net/url"
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

type GatewayRegistrationResult struct {
	Name   string
	Status int
	Body   any
}

type ToolSpec struct {
	Name            string
	Description     string
	RequestSchema   map[string]any
	Handler         HandlerFunc
	StreamHandler   StreamHandlerFunc
	Path            string
	Method          string
	Tags            []string
	ResponseSchema  map[string]any
	Headers         map[string]string
	TimeoutMS       int
	UpstreamBaseURL string
	UpstreamPath    string
	Auth            AuthConfig
	RetryCount      int
	RetryDelay      float64
	VerifyTLS       bool
	ResponseMode    string
	ProtocolMode    string
}

func (s ToolSpec) Endpoint() string {
	if s.UpstreamBaseURL == "" || s.UpstreamPath == "" {
		return ""
	}
	base, err := url.Parse(s.UpstreamBaseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(s.UpstreamPath)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
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

type RegisterProxyToolOptions struct {
	Name           string
	Description    string
	BaseURL        string
	Path           string
	RequestSchema  map[string]any
	Method         string
	UpstreamPath   string
	Tags           []string
	Headers        map[string]string
	TimeoutMS      int
	Auth           *AuthConfig
	RetryCount     int
	RetryDelay     float64
	VerifyTLS      *bool
	ResponseMode   string
	ProtocolMode   string
	ResponseSchema map[string]any
}

type RegisterToolFromOpenAPIOptions struct {
	Name         string
	BaseURL      string
	Spec         map[string]any
	SpecPath     string
	SpecURL      string
	OperationID  string
	Path         string
	Method       string
	Description  string
	Tags         []string
	Headers      map[string]string
	TimeoutMS    int
	VerifyTLS    *bool
	Auth         *AuthConfig
	RetryCount   int
	RetryDelay   float64
	ResponseMode string
	ProtocolMode string
}

type ExportGatewayPayloadOptions struct {
	Provider  string
	BaseURL   string
	Version   string
	Category  string
	Auth      map[string]any
	Enabled   *bool
	OwnerID   string
	CreatedBy string
	TimeoutMS *int
	ToolNames []string
}

type RegisterToGatewayOptions struct {
	GatewayURL     string
	Provider       string
	BaseURL        string
	Version        string
	Category       string
	Auth           *AuthConfig
	Enabled        *bool
	OwnerID        string
	CreatedBy      string
	TimeoutMS      *int
	ToolNames      []string
	GatewayAuth    *AuthConfig
	VerifyTLS      *bool
	TimeoutSeconds float64
	RetryCount     int
	RetryDelay     float64
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

type LoadOpenAPIOptions struct {
	Spec      map[string]any
	SpecPath  string
	SpecURL   string
	VerifyTLS *bool
}

type FindOpenAPIOperationOptions struct {
	Spec        map[string]any
	OperationID string
	Path        string
	Method      string
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
