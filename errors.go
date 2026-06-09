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
