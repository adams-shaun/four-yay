package v2agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Agent error codes (spec 10.5).
const (
	ErrMalformedJSON     = "malformed_json"
	ErrMalformedRequest  = "malformed_request"
	ErrProtocolMismatch  = "protocol_mismatch"
	ErrUnknownGame       = "unknown_game"
	ErrGameAlreadyActive = "game_already_active"
	ErrDecisionPending   = "decision_pending"
	ErrInternal          = "internal_error"
)

// Options configures an Agent.
type Options struct {
	// Name and Version are hello_ok.bot (spec 10.1); both must be nonempty.
	Name    string
	Version string
	// NoEcho drops the optional seat_step and semantic_echo from each
	// choice (spec 10.3). Echoing is the default.
	NoEcho bool
	// Log receives diagnostics (unknown or mistyped fields, policy
	// failures); nil discards them. Never part of the protocol (spec 2).
	Log io.Writer
	// MaxLogLines bounds the diagnostics written per process (0 = 64).
	MaxLogLines int
	// SkipStrictCheck turns off the second, strict decode that reports
	// fields the typed structs do not know. Play is unaffected either way.
	SkipStrictCheck bool
	// BeliefLog, when non-nil, receives one JSON line per choose: the
	// seat's reconstruction (Belief) keyed by game_id, seat and seat_step,
	// for the reverse-adapter shadow check. Diagnostics only; never part of
	// the protocol.
	BeliefLog io.Writer
}

// Stats counts what the agent answered. Every protocol-visible failure is
// here: errors it sent (by code), and the choose requests it answered with
// a fallback instead of an error because the policy failed (a choose error
// is a forfeit, spec 10.5, and never loses a legal action on our side).
type Stats struct {
	Requests        int
	Chooses         int
	Errors          map[string]int
	PolicyFallbacks int
}

// Agent serves the agent role of spec Section 10 for one policy. It is not
// safe for concurrent use: the protocol never pipelines (spec 2).
type Agent struct {
	policy Policy
	opts   Options

	gameID string
	active bool

	// Game is the active (or last) game's game_start, typed.
	Game *GameStart
	// Observation is the latest decision's observation, typed; nil before
	// the first choose.
	Observation *Observation
	// Decision is the latest decision as the policy read it.
	Decision *Decision

	logged   map[string]bool
	logLines int

	belief *Belief
	// Stats counts the agent's answers (see Stats).
	Stats Stats
}

// New returns an agent that answers with policy.
func New(policy Policy, opts Options) (*Agent, error) {
	if policy == nil {
		return nil, errors.New("v2agent: nil policy")
	}
	if opts.Name == "" || opts.Version == "" {
		return nil, errors.New("v2agent: bot name and version must be nonempty (spec 10.1)")
	}
	if opts.MaxLogLines == 0 {
		opts.MaxLogLines = 64
	}
	return &Agent{policy: policy, opts: opts, logged: map[string]bool{}, Stats: Stats{Errors: map[string]int{}}}, nil
}

// Serve answers request lines from r on w until r reaches EOF (closing stdin
// ends the process, spec 2). It returns nil at a clean EOF.
func (a *Agent) Serve(r io.Reader, w io.Writer) error {
	lr := NewLineReader(r)
	bw := bufio.NewWriterSize(w, 64<<10)
	for {
		line, err := lr.ReadLine()
		var out []byte
		switch {
		case err == nil:
			out = a.HandleLine(line)
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, ErrLineTooLong), errors.Is(err, ErrUnterminated):
			a.logf("framing: %v", err)
			out = errorLine("", ErrMalformedJSON, err.Error())
		default:
			return err
		}
		if _, err := bw.Write(out); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
	}
}

// HandleLine answers one request line (terminator optional) with one
// canonical response line, "\n" included.
func (a *Agent) HandleLine(line []byte) []byte {
	a.Stats.Requests++
	out := a.handleLine(line)
	if bytes.HasPrefix(out, []byte(`{"error":{"code":"`)) {
		code := out[len(`{"error":{"code":"`):]
		if i := bytes.IndexByte(code, '"'); i >= 0 {
			a.Stats.Errors[string(code[:i])]++
		}
	}
	return out
}

func (a *Agent) handleLine(line []byte) []byte {
	line = bytes.TrimRight(line, "\r\n")
	if !json.Valid(line) {
		return errorLine("", ErrMalformedJSON, "line is not valid JSON")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(line, &top); err != nil || top == nil {
		return errorLine("", ErrMalformedRequest, "top-level JSON value is not an object")
	}
	requestID, ok := rawString(top["request_id"])
	if !ok || requestID == "" {
		return errorLine("", ErrMalformedRequest, "request_id must be a nonempty string")
	}
	protocol, ok := rawString(top["protocol"])
	if !ok {
		return errorLine(requestID, ErrMalformedRequest, "protocol must be a string")
	}
	if protocol != Protocol {
		return errorLine(requestID, ErrProtocolMismatch, `protocol must be "`+Protocol+`"`)
	}
	requestType, ok := rawString(top["request_type"])
	if !ok {
		return errorLine(requestID, ErrMalformedRequest, "request_type must be a string")
	}
	switch requestType {
	case "hello":
		return a.hello(requestID)
	case "game_start":
		return a.gameStart(requestID, line, top)
	case "choose":
		return a.choose(requestID, top)
	case "game_over":
		return a.gameOver(requestID, line, top)
	}
	return errorLine(requestID, ErrMalformedRequest, "unknown request_type")
}

func (a *Agent) hello(requestID string) []byte {
	return responseLine("hello_ok", requestID, map[string]any{
		"bot":                 map[string]any{"name": a.opts.Name, "version": a.opts.Version},
		"requires":            map[string]any{"observation": []any{}, "extensions": []any{}},
		"extensions_accepted": []any{},
	})
}

func (a *Agent) gameStart(requestID string, line []byte, top map[string]json.RawMessage) []byte {
	if a.active {
		return errorLine(requestID, ErrGameAlreadyActive, "a game is already active")
	}
	gameID, ok := rawString(top["game_id"])
	if !ok {
		return errorLine(requestID, ErrMalformedRequest, "game_id must be a string")
	}
	gs := &GameStart{}
	if err := json.Unmarshal(line, gs); err != nil {
		a.logf("game_start: lenient decode: %v", err)
	}
	gs.GameID = gameID
	_, gs.seatIsString = rawString(top["seat"])
	if !a.opts.SkipStrictCheck {
		a.strictCheck("game_start", line, &struct {
			envelope
			GameStart
		}{})
	}
	if err := a.call(func() error { a.policy.GameStart(gs); return nil }); err != nil {
		// The game did not start, so a later game_start is still welcome.
		return errorLine(requestID, ErrInternal, "game_start failed: "+err.Error())
	}
	a.Game, a.gameID, a.active = gs, gameID, true
	a.Observation, a.Decision = nil, nil
	if a.opts.BeliefLog != nil {
		a.belief = NewBelief(gs)
	}
	return responseLine("ack", requestID, nil)
}

func (a *Agent) choose(requestID string, top map[string]json.RawMessage) []byte {
	if refusal := a.refuseUnlessActive(requestID, top); refusal != nil {
		return refusal
	}
	d, err := a.readDecision(top)
	if err != nil {
		return errorLine(requestID, ErrMalformedRequest, err.Error())
	}
	a.Stats.Chooses++
	if a.belief != nil {
		beliefWriter{a.opts.BeliefLog}.write(a.belief.Record(a.gameID, d))
	}
	index := -1
	err = a.call(func() error {
		var e error
		index, e = a.policy.Choose(d)
		return e
	})
	if err == nil && (index < 0 || index >= len(d.Candidates)) {
		err = fmt.Errorf("policy chose index %d of %d candidates", index, len(d.Candidates))
	}
	if err != nil {
		// An error answering choose is a forfeit (spec 10.5): answer the
		// first candidate instead -- pass whenever passing is legal (spec
		// 7.1) -- and count it.
		a.Stats.PolicyFallbacks++
		a.logf("choose: policy failed (%v); answering candidate 0", err)
		index = 0
	}
	a.Decision, a.Observation = d, d.Observation()
	chosen := &d.Candidates[index]
	selection := map[string]any{"candidate_id": chosen.ID}
	if !a.opts.NoEcho {
		if d.SeatStep != nil {
			selection["seat_step"] = *d.SeatStep
		}
		if len(chosen.Raw) > 0 {
			selection["semantic_echo"] = chosen.Raw
		}
	}
	return responseLine("choice", requestID, map[string]any{"selection": selection})
}

func (a *Agent) gameOver(requestID string, line []byte, top map[string]json.RawMessage) []byte {
	if refusal := a.refuseUnlessActive(requestID, top); refusal != nil {
		return refusal
	}
	g := &GameOver{}
	if err := json.Unmarshal(line, g); err != nil {
		a.logf("game_over: lenient decode: %v", err)
	}
	a.active = false // the game is over even if the hook fails
	if err := a.call(func() error { a.policy.GameOver(g); return nil }); err != nil {
		return errorLine(requestID, ErrInternal, "game_over failed: "+err.Error())
	}
	return responseLine("ack", requestID, nil)
}

// refuseUnlessActive is the error for a request that does not name the
// active game (spec 10.5), else nil.
func (a *Agent) refuseUnlessActive(requestID string, top map[string]json.RawMessage) []byte {
	gameID, ok := rawString(top["game_id"])
	if !ok {
		return errorLine(requestID, ErrMalformedRequest, "game_id must be a string")
	}
	if !a.active || gameID != a.gameID {
		return errorLine(requestID, ErrUnknownGame, "game_id names no active game")
	}
	return nil
}

// readDecision reads a choose request. It fails only when the candidates
// are unusable: not a nonempty array of objects with integer candidate_id
// (python's Decision.from_request). Everything else is best effort.
func (a *Agent) readDecision(top map[string]json.RawMessage) (*Decision, error) {
	d := &Decision{Seat: &SeatDecision{}}
	d.GameID, _ = rawString(top["game_id"])
	var decision map[string]json.RawMessage
	if raw := trimJSON(top["decision"]); len(raw) > 0 && raw[0] == '{' {
		if err := json.Unmarshal(raw, &decision); err != nil {
			return nil, fmt.Errorf("decision: %v", err)
		}
	}
	var candidates []json.RawMessage
	if raw := trimJSON(decision["candidates"]); len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &candidates)
	}
	if len(candidates) == 0 {
		return nil, errors.New("decision.candidates must be a nonempty list")
	}
	d.Candidates = make([]Candidate, len(candidates))
	for i, raw := range candidates {
		var fields map[string]json.RawMessage
		if r := trimJSON(raw); len(r) == 0 || r[0] != '{' || json.Unmarshal(r, &fields) != nil {
			return nil, fmt.Errorf("decision.candidates[%d] has no integer candidate_id", i)
		}
		id, ok := rawInt(fields["candidate_id"])
		if !ok {
			return nil, fmt.Errorf("decision.candidates[%d] has no integer candidate_id", i)
		}
		c := &d.Candidates[i]
		c.ID = id
		c.Raw = trimJSON(fields["semantic"])
		if len(c.Raw) > 0 && c.Raw[0] == '{' {
			if err := json.Unmarshal(c.Raw, &c.Fields); err != nil {
				c.Fields = nil
			}
			if err := json.Unmarshal(c.Raw, &c.Semantic); err != nil {
				a.logf("candidate semantic: lenient decode: %v", err)
			}
			if !a.opts.SkipStrictCheck {
				a.strictCheck("semantic "+c.Kind(), c.Raw, &Semantic{})
			}
		}
		if s, ok := rawString(fields["display_text"]); ok {
			c.DisplayText = &s
		}
	}
	if seatStep, ok := rawInt(decision["seat_step"]); ok {
		d.SeatStep = &seatStep
	}
	if raw := top["decision"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, d.Seat); err != nil {
			a.logf("decision: lenient decode: %v", err)
		}
		if !a.opts.SkipStrictCheck {
			a.strictCheck("decision", raw, &SeatDecision{})
		}
	}
	if raw := top["clock"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &d.Clock); err != nil {
			a.logf("clock: lenient decode: %v", err)
		}
	}
	return d, nil
}

// call runs f, turning a panic into an error (answered internal_error).
func (a *Agent) call(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
			a.logf("policy panic: %v", r)
		}
	}()
	return f()
}

// envelope holds the request envelope fields for the strict check.
type envelope struct {
	RequestType string `json:"request_type"`
	Protocol    string `json:"protocol"`
	RequestID   string `json:"request_id"`
}

// strictCheck logs every field of raw that target's type does not model
// (UnknownFields). Mistyped fields are logged by the lenient decode. It
// never affects play.
func (a *Agent) strictCheck(what string, raw []byte, target any) {
	for _, path := range UnknownFields(raw, target) {
		a.logf("%s: unknown field %s", what, path)
	}
}

// logf writes one diagnostics line, each distinct line once, at most
// MaxLogLines per process.
func (a *Agent) logf(format string, args ...any) {
	if a.opts.Log == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if a.logged[msg] || a.logLines > a.opts.MaxLogLines {
		return
	}
	a.logged[msg] = true
	a.logLines++
	if a.logLines > a.opts.MaxLogLines {
		msg = "further diagnostics suppressed"
	}
	fmt.Fprintf(a.opts.Log, "sbagent: %s\n", msg)
}

// rawString reads a JSON string; ok is false for anything else (absent,
// null, number, ...).
func rawString(raw json.RawMessage) (string, bool) {
	raw = trimJSON(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// rawInt reads a JSON integer literal (no fraction, no exponent) that fits
// an int64.
func rawInt(raw json.RawMessage) (int64, bool) {
	text := string(trimJSON(raw))
	if text == "" || strings.ContainsAny(text, ".eE\"") {
		return 0, false
	}
	n, err := strconv.ParseInt(text, 10, 64)
	return n, err == nil
}

// responseLine builds one canonical response line.
func responseLine(responseType, requestID string, fields map[string]any) []byte {
	msg := map[string]any{"response_type": responseType, "protocol": Protocol, "request_id": requestID}
	for k, v := range fields {
		msg[k] = v
	}
	out, err := CanonicalLine(msg)
	if err != nil {
		// Only a semantic echo can fail to re-encode; answer without it
		// rather than with nothing (the host reads leniently).
		if sel, ok := fields["selection"].(map[string]any); ok {
			delete(sel, "semantic_echo")
			if out, err = CanonicalLine(msg); err == nil {
				return out
			}
		}
		return errorLine(requestID, ErrInternal, "response encoding failed")
	}
	return out
}

// maxMessageChars bounds error messages (human-facing only, spec 10.5).
const maxMessageChars = 240

func errorLine(requestID, code, message string) []byte {
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > maxMessageChars {
		message = message[:maxMessageChars]
	}
	out, _ := CanonicalLine(map[string]any{
		"response_type": "error", "protocol": Protocol, "request_id": requestID,
		"error": map[string]any{"code": code, "message": message},
	})
	return out
}
