// Derived from the FlowLayer server protocol v1.
// This file contains the client-side subset required by the TUI.
// Maintained locally in the FlowLayer TUI repository.
package protocol

type ErrorCode string

const (
	ErrorCodeUnknownService ErrorCode = "unknown_service"
	ErrorCodeInternal       ErrorCode = "internal_error"
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
