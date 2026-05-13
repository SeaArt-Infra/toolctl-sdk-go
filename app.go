package toolctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
)

type App struct {
	title       string
	version     string
	description string
	basePath    string
	docsURL     string
	openAPIURL  string

	mu    sync.RWMutex
	tools map[string]*ToolSpec
}

func Start(config AppConfig) *App {
	version := config.Version
	if version == "" {
		version = "0.1.0"
	}
	docsURL := config.DocsURL
	if docsURL == "" {
		docsURL = "/docs"
	}
	openAPIURL := config.OpenAPIURL
	if openAPIURL == "" {
		openAPIURL = "/openapi.json"
	}
	return &App{
		title:       config.Title,
		version:     version,
		description: config.Description,
		basePath:    config.BasePath,
		docsURL:     docsURL,
		openAPIURL:  openAPIURL,
		tools:       map[string]*ToolSpec{},
	}
}

func CreateApp(config AppConfig) *App {
	return Start(config)
}

func (a *App) Title() string {
	return a.title
}

func (a *App) Version() string {
	return a.version
}

func (a *App) Description() string {
	return a.description
}

func (a *App) RegisterTool(opts RegisterToolOptions) (*ToolSpec, error) {
	if opts.Name == "" {
		return nil, &ToolRegistrationError{Message: "tool name is required"}
	}
	if opts.Handler == nil {
		return nil, &ToolRegistrationError{Message: "tool handler must be provided"}
	}
	responseMode := opts.ResponseMode
	if responseMode == "" {
		responseMode = "json"
	}
	if responseMode != "json" && responseMode != "sse" {
		return nil, &ToolRegistrationError{Message: "response mode must be either 'json' or 'sse'"}
	}
	protocolMode := opts.ProtocolMode
	if protocolMode == "" {
		protocolMode = "strict"
	}
	if protocolMode != "strict" && protocolMode != "passthrough" {
		return nil, &ToolRegistrationError{Message: "protocol mode must be either 'strict' or 'passthrough'"}
	}
	method := strings.ToUpper(opts.Method)
	if method == "" {
		method = http.MethodPost
	}
	path := opts.Path
	if path == "" {
		path = "/tools/" + opts.Name
	}
	timeoutMS := opts.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = 30000
	}
	auth := AuthConfig{}.withDefaults()
	if opts.Auth != nil {
		auth = opts.Auth.withDefaults()
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.tools[opts.Name]; exists {
		return nil, &ToolRegistrationError{Message: fmt.Sprintf("tool %q is already registered", opts.Name)}
	}

	spec := &ToolSpec{
		Name:           opts.Name,
		Description:    opts.Description,
		RequestSchema:  opts.RequestSchema,
		Handler:        opts.Handler,
		Path:           path,
		Method:         method,
		Tags:           append([]string(nil), opts.Tags...),
		ResponseSchema: opts.ResponseSchema,
		Headers:        map[string]string{},
		TimeoutMS:      timeoutMS,
		Auth:           auth,
		VerifyTLS:      true,
		ResponseMode:   responseMode,
		ProtocolMode:   protocolMode,
	}
	a.tools[opts.Name] = spec
	return spec, nil
}

func (a *App) MustRegisterTool(opts RegisterToolOptions) *ToolSpec {
	spec, err := a.RegisterTool(opts)
	if err != nil {
		panic(err)
	}
	return spec
}

func (a *App) RegisterSSETool(opts RegisterSSEToolOptions) (*ToolSpec, error) {
	if opts.Handler == nil {
		return nil, &ToolRegistrationError{Message: "stream handler must be provided"}
	}
	protocolMode := opts.ProtocolMode
	if protocolMode == "" {
		protocolMode = "strict"
	}
	spec, err := a.RegisterTool(RegisterToolOptions{
		Name:          opts.Name,
		Description:   opts.Description,
		RequestSchema: opts.RequestSchema,
		Handler: func(context.Context, map[string]any) (any, error) {
			return nil, nil
		},
		Method:       opts.Method,
		Path:         opts.Path,
		Tags:         opts.Tags,
		TimeoutMS:    opts.TimeoutMS,
		Auth:         opts.Auth,
		ResponseMode: "sse",
		ProtocolMode: protocolMode,
	})
	if err != nil {
		return nil, err
	}
	spec.StreamHandler = opts.Handler
	return spec, nil
}

func (a *App) MustRegisterSSETool(opts RegisterSSEToolOptions) *ToolSpec {
	spec, err := a.RegisterSSETool(opts)
	if err != nil {
		panic(err)
	}
	return spec
}

func (a *App) RegisterProxyTool(opts RegisterProxyToolOptions) (*ToolSpec, error) {
	responseMode := opts.ResponseMode
	if responseMode == "" {
		responseMode = "json"
	}
	protocolMode := opts.ProtocolMode
	if protocolMode == "" {
		protocolMode = "strict"
	}
	timeoutMS := opts.TimeoutMS
	if timeoutMS == 0 {
		timeoutMS = 30000
	}
	verifyTLS := true
	if opts.VerifyTLS != nil {
		verifyTLS = *opts.VerifyTLS
	}
	auth := AuthConfig{}.withDefaults()
	if opts.Auth != nil {
		auth = opts.Auth.withDefaults()
	}
	path := opts.Path
	if path == "" {
		path = "/tools/" + opts.Name
	}
	upstreamPath := opts.UpstreamPath
	if upstreamPath == "" {
		upstreamPath = path
	}

	if responseMode == "sse" {
		spec, err := a.RegisterSSETool(RegisterSSEToolOptions{
			Name:          opts.Name,
			Description:   opts.Description,
			RequestSchema: opts.RequestSchema,
			Method:        opts.Method,
			Path:          path,
			Tags:          opts.Tags,
			TimeoutMS:     timeoutMS,
			Auth:          &auth,
			ProtocolMode:  protocolMode,
			Handler: func(ctx context.Context, payload map[string]any, writer StreamWriter) error {
				return StreamUpstreamTool(ctx, StreamUpstreamToolOptions{
					BaseURL:    opts.BaseURL,
					Path:       upstreamPath,
					Method:     opts.Method,
					Payload:    payload,
					Headers:    opts.Headers,
					TimeoutMS:  timeoutMS,
					Auth:       auth,
					RetryCount: opts.RetryCount,
					RetryDelay: opts.RetryDelay,
					VerifyTLS:  verifyTLS,
				}, func(chunk string) error {
					return writer.Write(chunk)
				})
			},
		})
		if err != nil {
			return nil, err
		}
		spec.Headers = cloneStringMap(opts.Headers)
		spec.UpstreamBaseURL = opts.BaseURL
		spec.UpstreamPath = upstreamPath
		spec.RetryCount = opts.RetryCount
		spec.RetryDelay = opts.RetryDelay
		spec.VerifyTLS = verifyTLS
		return spec, nil
	}

	spec, err := a.RegisterTool(RegisterToolOptions{
		Name:           opts.Name,
		Description:    opts.Description,
		RequestSchema:  opts.RequestSchema,
		Method:         opts.Method,
		Path:           path,
		Tags:           opts.Tags,
		ResponseSchema: opts.ResponseSchema,
		TimeoutMS:      timeoutMS,
		Auth:           &auth,
		ResponseMode:   responseMode,
		ProtocolMode:   protocolMode,
		Handler: func(ctx context.Context, payload map[string]any) (any, error) {
			return CallUpstreamTool(ctx, CallUpstreamToolOptions{
				BaseURL:    opts.BaseURL,
				Path:       upstreamPath,
				Method:     opts.Method,
				Payload:    payload,
				Headers:    opts.Headers,
				TimeoutMS:  timeoutMS,
				Auth:       auth,
				RetryCount: opts.RetryCount,
				RetryDelay: opts.RetryDelay,
				VerifyTLS:  verifyTLS,
			})
		},
	})
	if err != nil {
		return nil, err
	}
	spec.Headers = cloneStringMap(opts.Headers)
	spec.UpstreamBaseURL = opts.BaseURL
	spec.UpstreamPath = upstreamPath
	spec.RetryCount = opts.RetryCount
	spec.RetryDelay = opts.RetryDelay
	spec.VerifyTLS = verifyTLS
	return spec, nil
}

func (a *App) MustRegisterProxyTool(opts RegisterProxyToolOptions) *ToolSpec {
	spec, err := a.RegisterProxyTool(opts)
	if err != nil {
		panic(err)
	}
	return spec
}

func (a *App) RegisterToolFromOpenAPI(opts RegisterToolFromOpenAPIOptions) (*ToolSpec, error) {
	openAPISpec, err := LoadOpenAPISpec(LoadOpenAPIOptions{
		Spec:      opts.Spec,
		SpecPath:  opts.SpecPath,
		SpecURL:   opts.SpecURL,
		VerifyTLS: opts.VerifyTLS,
	})
	if err != nil {
		return nil, err
	}
	matchedPath, matchedMethod, operation, err := FindOpenAPIOperation(FindOpenAPIOperationOptions{
		Spec:        openAPISpec,
		OperationID: opts.OperationID,
		Path:        opts.Path,
		Method:      opts.Method,
	})
	if err != nil {
		return nil, err
	}
	requestSchema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	if requestBody, ok := operation["requestBody"].(map[string]any); ok {
		if content, ok := requestBody["content"].(map[string]any); ok {
			if appJSON, ok := content["application/json"].(map[string]any); ok {
				if schema, ok := appJSON["schema"].(map[string]any); ok {
					requestSchema = schema
				}
			}
		}
	}
	description := opts.Description
	if description == "" {
		if value, ok := operation["description"].(string); ok && value != "" {
			description = value
		} else if value, ok := operation["summary"].(string); ok && value != "" {
			description = value
		} else {
			description = opts.Name
		}
	}
	tags := opts.Tags
	if len(tags) == 0 {
		if rawTags, ok := operation["tags"].([]any); ok {
			tags = make([]string, 0, len(rawTags))
			for _, item := range rawTags {
				if value, ok := item.(string); ok {
					tags = append(tags, value)
				}
			}
		}
	}
	return a.RegisterProxyTool(RegisterProxyToolOptions{
		Name:          opts.Name,
		Description:   description,
		BaseURL:       opts.BaseURL,
		Path:          "/tools/" + opts.Name,
		RequestSchema: requestSchema,
		Method:        matchedMethod,
		UpstreamPath:  matchedPath,
		Tags:          tags,
		Headers:       opts.Headers,
		TimeoutMS:     opts.TimeoutMS,
		Auth:          opts.Auth,
		RetryCount:    opts.RetryCount,
		RetryDelay:    opts.RetryDelay,
		VerifyTLS:     opts.VerifyTLS,
		ResponseMode:  opts.ResponseMode,
		ProtocolMode:  opts.ProtocolMode,
	})
}

func (a *App) MustRegisterToolFromOpenAPI(opts RegisterToolFromOpenAPIOptions) *ToolSpec {
	spec, err := a.RegisterToolFromOpenAPI(opts)
	if err != nil {
		panic(err)
	}
	return spec
}

func (a *App) ExportGatewayPayloads(opts ExportGatewayPayloadOptions) []map[string]any {
	version := opts.Version
	if version == "" {
		version = "v1"
	}
	category := opts.Category
	if category == "" {
		category = "general"
	}
	enabled := true
	if opts.Enabled != nil {
		enabled = *opts.Enabled
	}
	filter := map[string]struct{}{}
	if len(opts.ToolNames) > 0 {
		for _, name := range opts.ToolNames {
			filter[name] = struct{}{}
		}
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	payloads := make([]map[string]any, 0, len(a.tools))
	for _, spec := range a.tools {
		if len(filter) > 0 {
			if _, ok := filter[spec.Name]; !ok {
				continue
			}
		}
		payloads = append(payloads, BuildGatewayRegistrationPayload(*spec, BuildGatewayRegistrationPayloadOptions{
			Provider:  opts.Provider,
			BaseURL:   opts.BaseURL,
			Version:   version,
			Category:  category,
			Auth:      opts.Auth,
			Enabled:   enabled,
			OwnerID:   opts.OwnerID,
			CreatedBy: opts.CreatedBy,
			TimeoutMS: opts.TimeoutMS,
		}))
	}
	return payloads
}

func (a *App) RegisterToGateway(ctx context.Context, opts RegisterToGatewayOptions) ([]GatewayRegistrationResult, error) {
	version := opts.Version
	if version == "" {
		version = "v1"
	}
	category := opts.Category
	if category == "" {
		category = "general"
	}
	authPayload := map[string]any{"type": "none"}
	if opts.Auth != nil {
		authPayload = structToMap(opts.Auth.withDefaults())
	}
	payloads := a.ExportGatewayPayloads(ExportGatewayPayloadOptions{
		Provider:  opts.Provider,
		BaseURL:   opts.BaseURL,
		Version:   version,
		Category:  category,
		Auth:      authPayload,
		Enabled:   opts.Enabled,
		OwnerID:   opts.OwnerID,
		CreatedBy: opts.CreatedBy,
		TimeoutMS: opts.TimeoutMS,
		ToolNames: opts.ToolNames,
	})
	verifyTLS := true
	if opts.VerifyTLS != nil {
		verifyTLS = *opts.VerifyTLS
	}
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds == 0 {
		timeoutSeconds = 30
	}
	gatewayAuth := AuthConfig{}.withDefaults()
	if opts.GatewayAuth != nil {
		gatewayAuth = opts.GatewayAuth.withDefaults()
	}
	return RegisterToolsToGateway(ctx, RegisterToolsToGatewayOptions{
		GatewayURL:     opts.GatewayURL,
		Payloads:       payloads,
		Auth:           gatewayAuth,
		VerifyTLS:      verifyTLS,
		TimeoutSeconds: timeoutSeconds,
		RetryCount:     opts.RetryCount,
		RetryDelay:     opts.RetryDelay,
	})
}

func (a *App) Run(host string, port int) error {
	if host == "" {
		host = "0.0.0.0"
	}
	if port == 0 {
		port = 8080
	}
	return http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), a)
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		a.handleHealth(w, r)
		return
	case "/tools":
		a.handleListTools(w, r)
		return
	case a.openAPIURL:
		a.handleOpenAPI(w, r)
		return
	case a.docsURL:
		a.handleDocs(w, r)
		return
	}

	a.mu.RLock()
	var matched *ToolSpec
	for _, spec := range a.tools {
		if spec.Path == r.URL.Path && spec.Method == r.Method {
			matched = spec
			break
		}
	}
	a.mu.RUnlock()
	if matched == nil {
		http.NotFound(w, r)
		return
	}
	a.handleTool(w, r, matched)
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleListTools(w http.ResponseWriter, _ *http.Request) {
	a.mu.RLock()
	tools := make([]map[string]any, 0, len(a.tools))
	for _, spec := range a.tools {
		tools = append(tools, map[string]any{
			"name":        spec.Name,
			"method":      spec.Method,
			"path":        spec.Path,
			"description": spec.Description,
			"tags":        spec.Tags,
		})
	}
	a.mu.RUnlock()
	slices.SortFunc(tools, func(a, b map[string]any) int {
		return strings.Compare(a["name"].(string), b["name"].(string))
	})
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (a *App) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.buildOpenAPI())
}

func (a *App) handleDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, fmt.Sprintf(
		"<html><body><h1>%s</h1><p>%s</p><p><a href=\"%s\">OpenAPI schema</a></p></body></html>",
		a.title,
		a.description,
		a.openAPIURL,
	))
}

func (a *App) handleTool(w http.ResponseWriter, r *http.Request, spec *ToolSpec) {
	taskID := r.Header.Get("x-tool-task-id")
	if taskID == "" {
		taskID = r.Header.Get("x-request-id")
	}
	if taskID == "" {
		taskID = NewTaskID()
	}

	payload, err := parsePayload(r, spec.Method)
	if err != nil {
		http.Error(w, "Invalid JSON request body.", http.StatusBadRequest)
		return
	}
	if err := validatePayload(spec.RequestSchema, payload); err != nil {
		if spec.ProtocolMode == "passthrough" {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, Failed(FailedOptions{
			ToolName: spec.Name,
			TaskID:   taskID,
			Code:     "INVALID_INPUT",
			Message:  err.Error(),
		}))
		return
	}

	if spec.ResponseMode == "sse" {
		a.handleSSETool(w, r, spec, payload, taskID)
		return
	}

	result, handlerErr := spec.Handler(r.Context(), payload)
	if handlerErr != nil {
		a.handleToolError(w, spec, taskID, handlerErr)
		return
	}
	if spec.ProtocolMode == "passthrough" {
		writeValue(w, http.StatusOK, result)
		return
	}
	writeJSON(w, http.StatusOK, NormalizeJSONResult(result, spec.Name, taskID))
}

func (a *App) handleSSETool(w http.ResponseWriter, r *http.Request, spec *ToolSpec, payload map[string]any, taskID string) {
	headers := w.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	writer := &httpStreamWriter{w: w, flusher: flusher, spec: spec, taskID: taskID}

	var err error
	if spec.StreamHandler != nil {
		err = spec.StreamHandler(r.Context(), payload, writer)
	} else {
		var result any
		result, err = spec.Handler(r.Context(), payload)
		if err == nil {
			err = writer.writeValue(result)
		}
	}
	if err != nil {
		if spec.ProtocolMode == "passthrough" {
			_ = writer.Write(map[string]any{"event": "error", "data": err.Error()})
			if !writer.sawDone {
				_ = writer.Write("data: [DONE]\n\n")
			}
			return
		}
		_ = writer.Write(Failed(FailedOptions{
			ToolName: spec.Name,
			TaskID:   taskID,
			Code:     errorCodeFor(err),
			Message:  err.Error(),
		}))
	}
	if !writer.sawDone {
		_ = writer.Write("data: [DONE]\n\n")
	}
}

func (a *App) handleToolError(w http.ResponseWriter, spec *ToolSpec, taskID string, err error) {
	if spec.ProtocolMode == "passthrough" {
		status := http.StatusInternalServerError
		switch err.(type) {
		case *UpstreamHTTPError, *UpstreamNetworkError, *UpstreamTimeoutError, *UpstreamRequestError:
			status = http.StatusBadGateway
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, Failed(FailedOptions{
		ToolName: spec.Name,
		TaskID:   taskID,
		Code:     errorCodeFor(err),
		Message:  err.Error(),
	}))
}

func (a *App) buildOpenAPI() map[string]any {
	paths := map[string]any{
		"/health": map[string]any{
			"get": map[string]any{
				"tags":        []string{"system"},
				"summary":     "Health check",
				"description": "Health check",
				"responses": map[string]any{
					"200": map[string]any{"description": "Successful response"},
				},
			},
		},
		"/tools": map[string]any{
			"get": map[string]any{
				"tags":        []string{"system"},
				"summary":     "List tools",
				"description": "List tools",
				"responses": map[string]any{
					"200": map[string]any{"description": "Successful response"},
				},
			},
		},
	}

	a.mu.RLock()
	for _, spec := range a.tools {
		pathItem, _ := paths[spec.Path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[spec.Path] = pathItem
		}
		responseSchema := spec.ResponseSchema
		if responseSchema == nil {
			responseSchema = ProtocolResponseSchema()
		}
		pathItem[strings.ToLower(spec.Method)] = map[string]any{
			"summary":     spec.Description,
			"description": spec.Description,
			"operationId": spec.Name,
			"tags":        spec.Tags,
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{
						"schema": spec.RequestSchema,
					},
				},
			},
			"responses": map[string]any{
				"200": map[string]any{
					"description": "Successful response",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": responseSchema,
						},
					},
				},
				"400": map[string]any{"description": "Invalid request body"},
			},
		}
	}
	a.mu.RUnlock()

	return map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":       a.title,
			"version":     a.version,
			"description": a.description,
		},
		"paths": paths,
	}
}

type httpStreamWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	spec    *ToolSpec
	taskID  string
	sawDone bool
}

func (w *httpStreamWriter) Write(item any) error {
	formatted, err := formatSSEEvent(item, w.spec, w.taskID)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w.w, formatted); err != nil {
		return err
	}
	w.flusher.Flush()
	w.sawDone = w.sawDone || isDoneMarker(formatted)
	return nil
}

func (w *httpStreamWriter) writeValue(value any) error {
	if value == nil {
		return nil
	}
	if items, ok := toAnySlice(value); ok {
		for _, item := range items {
			if err := w.Write(item); err != nil {
				return err
			}
		}
		return nil
	}
	return w.Write(value)
}

func formatSSEEvent(item any, spec *ToolSpec, taskID string) (string, error) {
	switch value := item.(type) {
	case nil:
		return "", nil
	case []byte:
		return string(value), nil
	case string:
		if strings.HasSuffix(value, "\n\n") || strings.HasPrefix(value, "data:") || strings.HasPrefix(value, "event:") {
			return value, nil
		}
		if spec.ProtocolMode == "passthrough" {
			return "data: " + value + "\n\n", nil
		}
		payload := Completed(CompletedOptions{
			ToolName: spec.Name,
			TaskID:   taskID,
			Outputs:  []any{TextOutput(value)},
		})
		return "event: " + payload["type"].(string) + "\ndata: " + mustJSON(payload) + "\n\n", nil
	case map[string]any:
		if spec.ProtocolMode != "passthrough" && IsToolEvent(value) {
			payload := EnsureToolEvent(value, spec.Name, taskID)
			return "event: " + payload["type"].(string) + "\ndata: " + mustJSON(payload) + "\n\n", nil
		}
		if hasSSEKeys(value) {
			var parts []string
			if event, ok := value["event"]; ok {
				parts = append(parts, "event: "+fmt.Sprint(event))
			}
			if id, ok := value["id"]; ok {
				parts = append(parts, "id: "+fmt.Sprint(id))
			}
			if retry, ok := value["retry"]; ok {
				parts = append(parts, "retry: "+fmt.Sprint(retry))
			}
			data := value["data"]
			switch dataValue := data.(type) {
			case nil:
				parts = append(parts, "data: ")
			case string:
				for _, line := range strings.Split(dataValue, "\n") {
					parts = append(parts, "data: "+line)
				}
			default:
				for _, line := range strings.Split(mustJSON(dataValue), "\n") {
					parts = append(parts, "data: "+line)
				}
			}
			return strings.Join(parts, "\n") + "\n\n", nil
		}
		if spec.ProtocolMode == "passthrough" {
			return "data: " + mustJSON(value) + "\n\n", nil
		}
		payload := NormalizeJSONResult(value, spec.Name, taskID)
		return "event: " + payload["type"].(string) + "\ndata: " + mustJSON(payload) + "\n\n", nil
	default:
		if spec.ProtocolMode == "passthrough" {
			return "data: " + mustJSON(value) + "\n\n", nil
		}
		payload := NormalizeJSONResult(value, spec.Name, taskID)
		return "event: " + payload["type"].(string) + "\ndata: " + mustJSON(payload) + "\n\n", nil
	}
}

func hasSSEKeys(value map[string]any) bool {
	for _, key := range []string{"event", "id", "retry", "data"} {
		if _, ok := value[key]; ok {
			return true
		}
	}
	return false
}

func isDoneMarker(value string) bool {
	return strings.TrimSpace(value) == "data: [DONE]"
}

func validatePayload(schema map[string]any, payload map[string]any) error {
	if schema == nil {
		return nil
	}
	if schemaType, ok := schema["type"].(string); ok && schemaType != "" && schemaType != "object" {
		return &ToolValidationError{Message: "Only object payload schemas are supported."}
	}
	var required []string
	switch raw := schema["required"].(type) {
	case []string:
		required = raw
	case []any:
		for _, item := range raw {
			if value, ok := item.(string); ok {
				required = append(required, value)
			}
		}
	}
	var missing []string
	for _, field := range required {
		if _, ok := payload[field]; !ok {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return &ToolValidationError{Message: "Missing required fields: " + strings.Join(missing, ", ")}
	}
	return nil
}

func parsePayload(r *http.Request, method string) (map[string]any, error) {
	if strings.EqualFold(method, http.MethodGet) {
		payload := map[string]any{}
		for key, values := range r.URL.Query() {
			if len(values) == 1 {
				payload[key] = values[0]
			} else {
				converted := make([]any, len(values))
				for i, value := range values {
					converted[i] = value
				}
				payload[key] = converted
			}
		}
		return payload, nil
	}

	if r.Body == nil {
		return map[string]any{}, nil
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	return payload, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeValue(w http.ResponseWriter, status int, value any) {
	switch typed := value.(type) {
	case nil:
		w.WriteHeader(status)
	case string:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, typed)
	default:
		writeJSON(w, status, typed)
	}
}

func errorCodeFor(err error) string {
	switch err.(type) {
	case *ToolValidationError:
		return "INVALID_INPUT"
	case *UpstreamHTTPError, *UpstreamNetworkError, *UpstreamTimeoutError, *UpstreamRequestError:
		return "UPSTREAM_REQUEST_FAILED"
	default:
		return "INTERNAL_ERROR"
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
