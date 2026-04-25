package protocol

import (
	"encoding/json"
	"strings"
)

type CommandHandler interface {
	HandleCommand(command CommandMessage, sender Sender)
}

type HandlerFunc func(command CommandMessage, sender Sender)

func (handler HandlerFunc) HandleCommand(command CommandMessage, sender Sender) {
	if handler == nil {
		return
	}

	handler(command, sender)
}

type Sender interface {
	Send(envelope Envelope)
}

type SenderFunc func(envelope Envelope)

func (sender SenderFunc) Send(envelope Envelope) {
	if sender == nil {
		return
	}

	sender(envelope)
}

type Router struct {
	handler CommandHandler
}

func NewRouter(handler CommandHandler) *Router {
	return &Router{handler: handler}
}

func (router *Router) Route(data []byte, sender Sender) {
	if sender == nil {
		return
	}

	envelope, validationErr := ParseEnvelope(data)
	if validationErr != nil {
		sender.Send(NewErrorEnvelope(validationErr.ToMessage()))
		return
	}

	router.RouteEnvelope(envelope, sender)
}

func (router *Router) RouteEnvelope(envelope Envelope, sender Sender) {
	if sender == nil {
		return
	}

	if validationErr := ValidateEnvelope(envelope); validationErr != nil {
		sender.Send(NewErrorEnvelope(validationErr.ToMessage()))
		return
	}

	messageType := MessageType(strings.TrimSpace(string(envelope.Type)))
	id := strings.TrimSpace(envelope.ID)
	name := strings.TrimSpace(envelope.Name)

	if messageType != MessageTypeCommand {
		sender.Send(NewErrorEnvelope(NewErrorMessage(id, ErrorCodeUnsupportedMessageType, "only command messages can be routed")))
		return
	}

	command := CommandMessage{
		Type:    MessageTypeCommand,
		ID:      id,
		Name:    name,
		Payload: envelope.Payload,
	}

	if router.handler == nil {
		sender.Send(NewErrorEnvelope(NewErrorMessage(command.ID, ErrorCodeInternal, "command handler is not configured")))
		return
	}

	router.handler.HandleCommand(command, sender)
}

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
