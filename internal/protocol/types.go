// Derived from the FlowLayer server protocol v1.
// This file contains the client-side subset required by the TUI.
// Maintained locally in the FlowLayer TUI repository.
package protocol

import "encoding/json"

type MessageType string

const (
	MessageTypeCommand MessageType = "command"
	MessageTypeAck     MessageType = "ack"
	MessageTypeResult  MessageType = "result"
	MessageTypeEvent   MessageType = "event"
	MessageTypeError   MessageType = "error"
)

type Envelope struct {
	Type    MessageType     `json:"type"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type AckPayload struct {
	Accepted bool          `json:"accepted"`
	Error    *ErrorPayload `json:"error,omitempty"`
}

type ResultPayload struct {
	OK             bool            `json:"ok"`
	Error          *ErrorPayload   `json:"error,omitempty"`
	FailedServices []string        `json:"failed_services,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorMessage struct {
	Type    MessageType  `json:"type"`
	ID      string       `json:"id,omitempty"`
	Payload ErrorPayload `json:"payload"`
}
