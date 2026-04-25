// Vendored from FlowLayer server protocol v1 for the embedded TUI client.
// Keep in sync manually when the server protocol evolves.
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

const (
	EventNameServiceStatus = "service_status"
	EventNameLog           = "log"
)

type Envelope struct {
	Type    MessageType     `json:"type"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type CommandMessage struct {
	Type    MessageType     `json:"type"`
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type AckPayload struct {
	Accepted bool          `json:"accepted"`
	Error    *ErrorPayload `json:"error,omitempty"`
}

type AckMessage struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id"`
	Payload AckPayload  `json:"payload"`
}

type ResultPayload struct {
	OK             bool            `json:"ok"`
	Error          *ErrorPayload   `json:"error,omitempty"`
	FailedServices []string        `json:"failed_services,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
}

type ResultMessage struct {
	Type    MessageType   `json:"type"`
	ID      string        `json:"id"`
	Payload ResultPayload `json:"payload"`
}

type EventMessage struct {
	Type    MessageType     `json:"type"`
	Name    string          `json:"name"`
	Payload json.RawMessage `json:"payload,omitempty"`
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
