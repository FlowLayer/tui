package protocol

type ErrorCode string

const (
	ErrorCodeInvalidJSON            ErrorCode = "invalid_json"
	ErrorCodeMissingType            ErrorCode = "missing_type"
	ErrorCodeUnknownType            ErrorCode = "unknown_type"
	ErrorCodeMissingID              ErrorCode = "missing_id"
	ErrorCodeMissingName            ErrorCode = "missing_name"
	ErrorCodeInvalidPayload         ErrorCode = "invalid_payload"
	ErrorCodeUnsupportedMessageType ErrorCode = "unsupported_message_type"
	ErrorCodeUnknownService         ErrorCode = "unknown_service"
	ErrorCodeServiceBusy            ErrorCode = "service_busy"
	ErrorCodeUnknownCommand         ErrorCode = "unknown_command"
	ErrorCodeInternal               ErrorCode = "internal_error"
)

type ValidationError struct {
	Code    ErrorCode
	Message string
	ID      string
}

func (err *ValidationError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

func (err *ValidationError) ToMessage() ErrorMessage {
	if err == nil {
		return NewErrorMessage("", ErrorCodeInternal, "unknown validation error")
	}

	return NewErrorMessage(err.ID, err.Code, err.Message)
}

func NewErrorMessage(id string, code ErrorCode, message string) ErrorMessage {
	return ErrorMessage{
		Type: MessageTypeError,
		ID:   id,
		Payload: ErrorPayload{
			Code:    string(code),
			Message: message,
		},
	}
}
