package protocol

type ErrorCode string

const (
	ErrorCodeUnknownService         ErrorCode = "unknown_service"
	ErrorCodeInternal               ErrorCode = "internal_error"
)

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
