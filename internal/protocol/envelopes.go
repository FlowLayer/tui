// Derived from the FlowLayer server protocol v1.
// This file contains the client-side subset required by the TUI.
// Maintained locally in the FlowLayer TUI repository.
package protocol

import "encoding/json"

func NewAckEnvelope(id string, accepted bool, payloadError *ErrorPayload) Envelope {
	payload := AckPayload{Accepted: accepted}
	if !accepted && payloadError != nil {
		payload.Error = payloadError
	}

	return Envelope{
		Type:    MessageTypeAck,
		ID:      id,
		Payload: mustMarshal(payload),
	}
}

func NewResultEnvelope(id string, ok bool, payloadError *ErrorPayload, failedServices []string, data json.RawMessage) Envelope {
	payload := ResultPayload{OK: ok}
	if !ok && payloadError != nil {
		payload.Error = payloadError
	}
	if len(failedServices) > 0 {
		payload.FailedServices = append([]string(nil), failedServices...)
	}
	if len(data) > 0 {
		payload.Data = append(json.RawMessage(nil), data...)
	}

	return Envelope{
		Type:    MessageTypeResult,
		ID:      id,
		Payload: mustMarshal(payload),
	}
}

func NewErrorEnvelope(message ErrorMessage) Envelope {
	return Envelope{
		Type:    MessageTypeError,
		ID:      message.ID,
		Payload: mustMarshal(message.Payload),
	}
}

func mustMarshal(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	return encoded
}
