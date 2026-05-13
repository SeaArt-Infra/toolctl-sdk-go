package toolctl

type ToolRegistrationError struct {
	Message string
}

func (e *ToolRegistrationError) Error() string {
	return e.Message
}

type ToolValidationError struct {
	Message string
}

func (e *ToolValidationError) Error() string {
	return e.Message
}

type OpenAPIImportError struct {
	Message string
}

func (e *OpenAPIImportError) Error() string {
	return e.Message
}

type UpstreamRequestError struct {
	Message string
}

func (e *UpstreamRequestError) Error() string {
	return e.Message
}

type UpstreamHTTPError struct {
	Message string
}

func (e *UpstreamHTTPError) Error() string {
	return e.Message
}

type UpstreamNetworkError struct {
	Message string
}

func (e *UpstreamNetworkError) Error() string {
	return e.Message
}

type UpstreamTimeoutError struct {
	Message string
}

func (e *UpstreamTimeoutError) Error() string {
	return e.Message
}

type GatewayRegistrationError struct {
	Message string
}

func (e *GatewayRegistrationError) Error() string {
	return e.Message
}
