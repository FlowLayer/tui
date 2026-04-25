package protocol

import (
	"encoding/json"
	"strings"
)

func ParseEnvelope(data []byte) (Envelope, *ValidationError) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return Envelope{}, &ValidationError{
			Code:    ErrorCodeInvalidJSON,
			Message: "invalid json message",
		}
	}

	id := extractUsableID(raw)

	rawType, ok := raw["type"]
	if !ok {
		return Envelope{}, &ValidationError{
			Code:    ErrorCodeMissingType,
			Message: "type is required",
			ID:      id,
		}
	}

	var typeValue string
	if err := json.Unmarshal(rawType, &typeValue); err != nil || strings.TrimSpace(typeValue) == "" {
		return Envelope{}, &ValidationError{
			Code:    ErrorCodeMissingType,
			Message: "type is required",
			ID:      id,
		}
	}

	envelope := Envelope{
		Type: MessageType(strings.TrimSpace(typeValue)),
		ID:   id,
	}

	rawName, hasName := raw["name"]
	if hasName {
		var nameValue string
		if err := json.Unmarshal(rawName, &nameValue); err == nil {
			envelope.Name = strings.TrimSpace(nameValue)
		}
	}

	rawPayload, hasPayload := raw["payload"]
	if hasPayload {
		envelope.Payload = rawPayload
	}

	if validationErr := ValidateEnvelope(envelope); validationErr != nil {
		if validationErr.ID == "" {
			validationErr.ID = id
		}
		return Envelope{}, validationErr
	}

	return envelope, nil
}

func ValidateEnvelope(envelope Envelope) *ValidationError {
	messageType := MessageType(strings.TrimSpace(string(envelope.Type)))
	id := strings.TrimSpace(envelope.ID)
	name := strings.TrimSpace(envelope.Name)

	if messageType == "" {
		return &ValidationError{
			Code:    ErrorCodeMissingType,
			Message: "type is required",
			ID:      id,
		}
	}

	switch messageType {
	case MessageTypeCommand:
		if id == "" {
			return &ValidationError{
				Code:    ErrorCodeMissingID,
				Message: "command.id is required",
			}
		}
		if name == "" {
			return &ValidationError{
				Code:    ErrorCodeMissingName,
				Message: "command.name is required",
				ID:      id,
			}
		}
	case MessageTypeAck:
		if id == "" {
			return &ValidationError{
				Code:    ErrorCodeMissingID,
				Message: "ack.id is required",
			}
		}
		if !isValidAckPayload(envelope.Payload) {
			return &ValidationError{
				Code:    ErrorCodeInvalidPayload,
				Message: "ack.payload.accepted is required",
				ID:      id,
			}
		}
	case MessageTypeResult:
		if id == "" {
			return &ValidationError{
				Code:    ErrorCodeMissingID,
				Message: "result.id is required",
			}
		}
		if !isValidResultPayload(envelope.Payload) {
			return &ValidationError{
				Code:    ErrorCodeInvalidPayload,
				Message: "result.payload.ok is required",
				ID:      id,
			}
		}
	case MessageTypeEvent:
		if name == "" {
			return &ValidationError{
				Code:    ErrorCodeMissingName,
				Message: "event.name is required",
				ID:      id,
			}
		}
	case MessageTypeError:
		if !isValidErrorPayload(envelope.Payload) {
			return &ValidationError{
				Code:    ErrorCodeInvalidPayload,
				Message: "error.payload.code and error.payload.message are required",
				ID:      id,
			}
		}
	default:
		return &ValidationError{
			Code:    ErrorCodeUnknownType,
			Message: "unknown message type",
			ID:      id,
		}
	}

	return nil
}

func extractUsableID(raw map[string]json.RawMessage) string {
	rawID, ok := raw["id"]
	if !ok {
		return ""
	}

	var id string
	if err := json.Unmarshal(rawID, &id); err != nil {
		return ""
	}

	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}

	return id
}

func isValidAckPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var payload struct {
		Accepted *bool `json:"accepted"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}

	return payload.Accepted != nil
}

func isValidResultPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var payload struct {
		OK *bool `json:"ok"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}

	return payload.OK != nil
}

func isValidErrorPayload(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var payload struct {
		Code    *string `json:"code"`
		Message *string `json:"message"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return false
	}

	if payload.Code == nil || strings.TrimSpace(*payload.Code) == "" {
		return false
	}
	if payload.Message == nil || strings.TrimSpace(*payload.Message) == "" {
		return false
	}

	return true
}
