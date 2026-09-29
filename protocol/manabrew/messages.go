package manabrew

import (
	"encoding/json"
	"fmt"
)

// Engine→client and client→engine envelopes
// (https://docs.manabrew.app/protocol/).
//
// The published spec leaves the outer envelope of `state` and `prompt`
// unspecified (its prompt examples show a bare AgentPrompt). INFERRED, per the
// scoping spec §1: engine→client messages are wrapped as
// {"kind":<kind>,…} and the prompt envelope flattens AgentPrompt into the
// prompt object.

// ErrorCode is the closed set of ProtocolError codes.
type ErrorCode string

const (
	CodeStalePrompt     ErrorCode = "stalePrompt"
	CodeWrongPlayer     ErrorCode = "wrongPlayer"
	CodeWrongPromptType ErrorCode = "wrongPromptType"
	CodeUnknownActionID ErrorCode = "unknownActionId"
	CodeInvalidShape    ErrorCode = "invalidShape"
)

// ProtocolError is the payload of an `error` message.
type ProtocolError struct {
	Code     ErrorCode `json:"code"`
	Message  string    `json:"message"`
	PromptID *int64    `json:"promptId,omitempty"`
}

// AgentPrompt is the prompt payload: the engine has paused and needs a
// decision from one player. Prompts carry no gameView.
type AgentPrompt struct {
	PromptID         int64       `json:"promptId"`
	DecidingPlayerID string      `json:"decidingPlayerId"`
	SourceCard       *CardDto    `json:"sourceCard,omitempty"`
	Input            PromptInput `json:"input"`
}

// EngineMessageValue is the closed set of engine→client payloads. Go cannot
// hang a method on an interface type, so the wire union is the EngineMessage
// struct carrier below; Value holds exactly one of the members after a decode
// (or whatever the caller stored).
type EngineMessageValue interface{ isEngineMessage() }

// EngineMessage is the engine→client message union, discriminated on kind.
type EngineMessage struct {
	Value EngineMessageValue `json:"-"`
}

// StateUpdate carries the full authoritative gameView.
type StateUpdate struct {
	Kind     string      `json:"kind"`
	GameView GameViewDto `json:"gameView"`
}

// StateDelta carries a patch plus base and fingerprint. gorge only *decodes*
// stateDelta (patches are optional for engines; MB-1 encodes none).
type StateDelta struct {
	Kind        string          `json:"kind"`
	Base        string          `json:"base"`
	Fingerprint string          `json:"fingerprint"`
	Patch       json.RawMessage `json:"patch"`
}

// DisplayMessage carries a DisplayEvent, which is UNDEFINED in the published
// spec and marked Work in Progress; the event stays opaque JSON.
type DisplayMessage struct {
	Kind  string          `json:"kind"`
	Event json.RawMessage `json:"event,omitempty"`
}

// PromptMessage wraps an AgentPrompt; the prompt fields are flattened into the
// envelope, so the wire shape is {"kind":"prompt","promptId":…,…}.
type PromptMessage struct {
	Kind        string `json:"kind"`
	AgentPrompt        // flattened
}

// ErrorMessage carries a ProtocolError.
type ErrorMessage struct {
	Kind  string        `json:"kind"`
	Error ProtocolError `json:"error"`
}

func (StateUpdate) isEngineMessage()    {}
func (StateDelta) isEngineMessage()     {}
func (DisplayMessage) isEngineMessage() {}
func (PromptMessage) isEngineMessage()  {}
func (ErrorMessage) isEngineMessage()   {}

// checkKind rejects a Kind that was explicitly set to something other than the
// type's own kind; an empty Kind is filled in.
func checkKind(kind, want string) error {
	if kind != "" && kind != want {
		return unknownDiscriminator("kind", kind)
	}
	return nil
}

func (s StateUpdate) MarshalJSON() ([]byte, error) {
	if err := checkKind(s.Kind, "state"); err != nil {
		return nil, err
	}
	s.Kind = "state"
	type plain StateUpdate
	return json.Marshal(plain(s))
}

func (s StateDelta) MarshalJSON() ([]byte, error) {
	if err := checkKind(s.Kind, "stateDelta"); err != nil {
		return nil, err
	}
	s.Kind = "stateDelta"
	type plain StateDelta
	return json.Marshal(plain(s))
}

func (d DisplayMessage) MarshalJSON() ([]byte, error) {
	if err := checkKind(d.Kind, "display"); err != nil {
		return nil, err
	}
	d.Kind = "display"
	type plain DisplayMessage
	return json.Marshal(plain(d))
}

func (p PromptMessage) MarshalJSON() ([]byte, error) {
	if err := checkKind(p.Kind, "prompt"); err != nil {
		return nil, err
	}
	p.Kind = "prompt"
	type plain PromptMessage
	return json.Marshal(plain(p))
}

func (e ErrorMessage) MarshalJSON() ([]byte, error) {
	if err := checkKind(e.Kind, "error"); err != nil {
		return nil, err
	}
	e.Kind = "error"
	type plain ErrorMessage
	return json.Marshal(plain(e))
}

func (m *EngineMessage) UnmarshalJSON(b []byte) error {
	var h struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	var v EngineMessageValue
	switch h.Kind {
	case "state":
		var x StateUpdate
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "stateDelta":
		var x StateDelta
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "display":
		var x DisplayMessage
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "prompt":
		var x PromptMessage
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "error":
		var x ErrorMessage
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	default:
		return unknownDiscriminator("kind", h.Kind)
	}
	*m = EngineMessage{Value: v}
	return nil
}

func (m EngineMessage) MarshalJSON() ([]byte, error) {
	if m.Value == nil {
		return nil, unknownDiscriminator("kind", "<nil>")
	}
	return json.Marshal(m.Value)
}

// ClientMessageValue is the closed set of client→engine payloads; see
// EngineMessageValue for why the union is a struct carrier.
type ClientMessageValue interface{ isClientMessage() }

// ClientMessage is the client→engine message union, discriminated on kind.
type ClientMessage struct {
	Value ClientMessageValue `json:"-"`
}

// ClientResponse answers a prompt. The prompt type rides in Action.Type; the
// kind-specific answer rides in Action.Output.
type ClientResponse struct {
	Kind     string       `json:"kind"`
	PromptID int64        `json:"promptId"`
	Action   PromptOutput `json:"action"`
}

// ClientDirective carries an out-of-band directive.
type ClientDirective struct {
	Kind      string         `json:"kind"`
	Directive DirectiveInput `json:"directive"`
}

func (ClientResponse) isClientMessage()  {}
func (ClientDirective) isClientMessage() {}

func (c ClientResponse) MarshalJSON() ([]byte, error) {
	if err := checkKind(c.Kind, "response"); err != nil {
		return nil, err
	}
	c.Kind = "response"
	type plain ClientResponse
	return json.Marshal(plain(c))
}

func (c ClientDirective) MarshalJSON() ([]byte, error) {
	if err := checkKind(c.Kind, "directive"); err != nil {
		return nil, err
	}
	c.Kind = "directive"
	type plain ClientDirective
	return json.Marshal(plain(c))
}

func (m *ClientMessage) UnmarshalJSON(b []byte) error {
	var h struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &h); err != nil {
		return err
	}
	var v ClientMessageValue
	switch h.Kind {
	case "response":
		var x ClientResponse
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	case "directive":
		var x ClientDirective
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		v = x
	default:
		return unknownDiscriminator("kind", h.Kind)
	}
	*m = ClientMessage{Value: v}
	return nil
}

func (m ClientMessage) MarshalJSON() ([]byte, error) {
	if m.Value == nil {
		return nil, unknownDiscriminator("kind", "<nil>")
	}
	return json.Marshal(m.Value)
}

// DirectiveInput is an out-of-band client directive. The published spec shows
// only {type:"concede"}; anything else is rejected as an unknown discriminator.
type DirectiveInput struct {
	Type string `json:"type"`
}

func (d DirectiveInput) MarshalJSON() ([]byte, error) {
	if d.Type != "concede" {
		return nil, unknownDiscriminator("type", d.Type)
	}
	type plain DirectiveInput
	return json.Marshal(plain(d))
}

func (d *DirectiveInput) UnmarshalJSON(b []byte) error {
	type plain DirectiveInput
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.Type != "concede" {
		return unknownDiscriminator("type", v.Type)
	}
	*d = DirectiveInput(v)
	return nil
}

// unknownDiscriminator is the single error shape every union codec uses.
func unknownDiscriminator(field, value string) error {
	return fmt.Errorf("manabrew: unknown %s discriminator %q", field, value)
}
