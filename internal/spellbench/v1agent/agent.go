package v1agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
)

// Protocol is the v1 protocol string (spec 4.1).
const Protocol = "spellbench/v1"

// Agent error codes (spec 10.5).
const (
	ErrMalformedJSON    = "malformed_json"
	ErrMalformedRequest = "malformed_request"
	ErrProtocolMismatch = "protocol_mismatch"
	ErrRequestIDReuse   = "request_id_reuse_mismatch"
	ErrUnknownGame      = "unknown_game"
	ErrDecisionPending  = "decision_pending"
	ErrGameActive       = "game_already_active"
	ErrInternal         = "internal_error"
)

// GameStart is the game_start request (spec 10.2).
type GameStart struct {
	GameID string
	Seat   string
	Format string
	// Decks holds each seat's deck spec by seat order; CatalogIDs is the
	// catalog_id of each ("" for a decklist deck).
	Decks      []json.RawMessage
	CatalogIDs []string
	Engine     json.RawMessage
}

// Terminal is the game_over terminal (spec 10.4).
type Terminal struct {
	Outcome        string  `json:"outcome"`
	Classification string  `json:"classification"`
	Winner         *string `json:"winner"`
	Reason         string  `json:"reason"`
	StepCount      int64   `json:"step_count"`
	DecisionCount  int64   `json:"decision_count"`
}

// Policy is the plug point: any Go policy answers a v1 decision with a
// candidate index. Choose may panic or return an out-of-range index; the
// agent then answers with the fallback policy and counts it.
type Policy interface {
	GameStart(g *GameStart)
	Choose(d *Decision) int
	GameOver(t *Terminal)
}

// Stats counts what the agent had to absorb. None of these reach the wire
// as an error.
type Stats struct {
	Decisions     int
	Fallbacks     int // policy panicked or answered out of range
	KernelMissing int // decisions without a decodable x_kernel_v5
	WireErrors    int // requests answered with an error response
	RetriesServed int // identical retransmits answered from the cache
}

// Options configures an Agent.
type Options struct {
	Name, Version string
	// ExtensionsAccepted is informative (hello_ok.extensions_accepted).
	ExtensionsAccepted []string
	// Log receives diagnostics (never stdout); nil drops them.
	Log io.Writer
	// Fallback answers when the policy fails; nil means Heuristic.
	Fallback Policy
}

// Agent serves the v1 agent role for one policy.
type Agent struct {
	policy   Policy
	fallback Policy
	opts     Options
	gameID   string
	active   bool
	pending  bool
	lastID   string
	lastReq  []byte
	lastResp []byte
	Stats    Stats
}

// New builds an agent.
func New(p Policy, opts Options) *Agent {
	fb := opts.Fallback
	if fb == nil {
		fb = &Heuristic{}
	}
	if opts.ExtensionsAccepted == nil {
		opts.ExtensionsAccepted = []string{}
	}
	return &Agent{policy: p, fallback: fb, opts: opts}
}

// Serve answers request lines from r on w until EOF.
func (a *Agent) Serve(r io.Reader, w io.Writer) error {
	lr := v2agent.NewLineReader(r)
	for {
		line, err := lr.ReadLine()
		var out []byte
		switch {
		case errors.Is(err, io.EOF):
			a.logf("stats decisions=%d fallbacks=%d kernel_missing=%d wire_errors=%d retries=%d",
				a.Stats.Decisions, a.Stats.Fallbacks, a.Stats.KernelMissing, a.Stats.WireErrors, a.Stats.RetriesServed)
			return nil
		case errors.Is(err, v2agent.ErrLineTooLong), errors.Is(err, v2agent.ErrUnterminated):
			out = a.errorLine("", ErrMalformedJSON, err.Error())
		case err != nil:
			return err
		default:
			out = a.HandleLine(line)
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
		if f, ok := w.(interface{ Flush() error }); ok {
			if err := f.Flush(); err != nil {
				return err
			}
		}
	}
}

// HandleLine answers one request line with one response line ("\n"
// included).
func (a *Agent) HandleLine(line []byte) []byte {
	line = bytes.TrimRight(line, "\r\n")
	if !json.Valid(line) {
		return a.errorLine("", ErrMalformedJSON, "line is not valid JSON")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(line, &top); err != nil || top == nil {
		return a.errorLine("", ErrMalformedJSON, "top-level JSON value is not an object")
	}
	var requestID, protocol, requestType string
	if json.Unmarshal(top["request_id"], &requestID) != nil || requestID == "" {
		return a.errorLine("", ErrMalformedRequest, "request_id must be a nonempty string")
	}
	if json.Unmarshal(top["protocol"], &protocol) != nil {
		return a.errorLine(requestID, ErrMalformedRequest, "protocol must be a string")
	}
	if protocol != Protocol {
		return a.errorLine(requestID, ErrProtocolMismatch, `protocol must be "`+Protocol+`"`)
	}
	if a.lastReq != nil && a.lastID == requestID {
		if bytes.Equal(a.lastReq, line) {
			a.Stats.RetriesServed++
			return a.lastResp
		}
		return a.errorLine(requestID, ErrRequestIDReuse, "request_id reused with a different payload")
	}
	var resp []byte
	if json.Unmarshal(top["request_type"], &requestType) != nil {
		resp = a.errorLine(requestID, ErrMalformedRequest, "request_type must be a string")
	} else {
		switch requestType {
		case "hello":
			resp = a.hello(requestID, top)
		case "game_start":
			resp = a.gameStart(requestID, top)
		case "choose":
			resp = a.choose(requestID, top)
		case "game_over":
			resp = a.gameOver(requestID, top)
		default:
			resp = a.errorLine(requestID, ErrMalformedRequest, fmt.Sprintf("unknown request_type %q", requestType))
		}
	}
	a.lastID, a.lastReq, a.lastResp = requestID, append([]byte(nil), line...), resp
	return resp
}

func exactKeys(top map[string]json.RawMessage, keys ...string) error {
	if len(top) != len(keys) {
		return fmt.Errorf("expected exactly the fields %s", strings.Join(keys, ", "))
	}
	for _, k := range keys {
		if _, ok := top[k]; !ok {
			return fmt.Errorf("missing field %q", k)
		}
	}
	return nil
}

func (a *Agent) hello(id string, top map[string]json.RawMessage) []byte {
	if err := exactKeys(top, "request_type", "protocol", "request_id"); err != nil {
		return a.errorLine(id, ErrMalformedRequest, err.Error())
	}
	if a.pending {
		return a.errorLine(id, ErrDecisionPending, "a decision is pending")
	}
	return a.response("hello_ok", id, map[string]any{
		"bot":                 map[string]any{"name": a.opts.Name, "version": a.opts.Version},
		"extensions_accepted": a.opts.ExtensionsAccepted,
	})
}

func (a *Agent) gameStart(id string, top map[string]json.RawMessage) []byte {
	if a.pending {
		return a.errorLine(id, ErrDecisionPending, "a decision is pending")
	}
	if a.active {
		return a.errorLine(id, ErrGameActive, "a game is already active on this agent")
	}
	if err := exactKeys(top, "request_type", "protocol", "request_id", "game_id", "seat", "format", "decks", "engine"); err != nil {
		return a.errorLine(id, ErrMalformedRequest, err.Error())
	}
	g := &GameStart{Engine: top["engine"]}
	if json.Unmarshal(top["game_id"], &g.GameID) != nil || g.GameID == "" {
		return a.errorLine(id, ErrMalformedRequest, "game_id must be a nonempty string")
	}
	if json.Unmarshal(top["seat"], &g.Seat) != nil || (g.Seat != "p0" && g.Seat != "p1") {
		return a.errorLine(id, ErrMalformedRequest, `seat must be "p0" or "p1"`)
	}
	if json.Unmarshal(top["format"], &g.Format) != nil {
		return a.errorLine(id, ErrMalformedRequest, "format must be a string")
	}
	if json.Unmarshal(top["decks"], &g.Decks) != nil || len(g.Decks) != 2 {
		return a.errorLine(id, ErrMalformedRequest, "decks must be a 2-entry array")
	}
	for _, d := range g.Decks {
		var spec struct {
			CatalogID string `json:"catalog_id"`
		}
		_ = json.Unmarshal(d, &spec)
		g.CatalogIDs = append(g.CatalogIDs, spec.CatalogID)
	}
	if err := a.call(func() { a.policy.GameStart(g) }); err != nil {
		// A policy that cannot start still plays: every choose falls back.
		a.logf("game_start: policy failed: %v", err)
	}
	_ = a.call(func() { a.fallback.GameStart(g) })
	a.gameID, a.active = g.GameID, true
	return a.response("ack", id, nil)
}

func (a *Agent) choose(id string, top map[string]json.RawMessage) []byte {
	if !a.active {
		return a.errorLine(id, ErrUnknownGame, "no active game")
	}
	if a.pending {
		return a.errorLine(id, ErrDecisionPending, "a decision is pending")
	}
	if err := exactKeys(top, "request_type", "protocol", "request_id", "game_id", "decision"); err != nil {
		return a.errorLine(id, ErrMalformedRequest, err.Error())
	}
	var gameID string
	if json.Unmarshal(top["game_id"], &gameID) != nil || gameID != a.gameID {
		return a.errorLine(id, ErrUnknownGame, fmt.Sprintf("unknown game_id %s", top["game_id"]))
	}
	d, err := ParseDecision(top["decision"])
	if err != nil {
		return a.errorLine(id, ErrMalformedRequest, err.Error())
	}
	a.Stats.Decisions++
	if d.Kernel == nil {
		a.Stats.KernelMissing++
		if d.KernelErr != nil {
			a.logf("x_kernel_v5 undecodable: %v", d.KernelErr)
		}
	}
	pick := -1
	if err := a.call(func() { pick = a.policy.Choose(d) }); err != nil {
		a.logf("choose step %d: policy failed: %v", d.Step, err)
		pick = -1
	}
	if pick < 0 || pick >= len(d.Candidates) {
		a.Stats.Fallbacks++
		a.logf("choose step %d: fallback (policy answered %d of %d)", d.Step, pick, len(d.Candidates))
		pick = 0
		_ = a.call(func() { pick = a.fallback.Choose(d) })
		if pick < 0 || pick >= len(d.Candidates) {
			pick = 0
		}
	}
	var echo any
	dec := json.NewDecoder(bytes.NewReader(d.Candidates[pick].Semantic.Raw))
	dec.UseNumber()
	if err := dec.Decode(&echo); err != nil {
		return a.errorLine(id, ErrInternal, "semantic re-encode failed")
	}
	return a.response("choice", id, map[string]any{
		"selection": map[string]any{"candidate_id": pick, "semantic_echo": echo},
	})
}

func (a *Agent) gameOver(id string, top map[string]json.RawMessage) []byte {
	if !a.active {
		return a.errorLine(id, ErrUnknownGame, "no active game")
	}
	if a.pending {
		return a.errorLine(id, ErrDecisionPending, "a decision is pending")
	}
	if err := exactKeys(top, "request_type", "protocol", "request_id", "game_id", "terminal"); err != nil {
		return a.errorLine(id, ErrMalformedRequest, err.Error())
	}
	var gameID string
	if json.Unmarshal(top["game_id"], &gameID) != nil || gameID != a.gameID {
		return a.errorLine(id, ErrUnknownGame, "unknown game_id")
	}
	var t Terminal
	if err := json.Unmarshal(top["terminal"], &t); err != nil {
		return a.errorLine(id, ErrMalformedRequest, "terminal: "+err.Error())
	}
	_ = a.call(func() { a.policy.GameOver(&t) })
	_ = a.call(func() { a.fallback.GameOver(&t) })
	a.active, a.gameID = false, ""
	return a.response("ack", id, nil)
}

func (a *Agent) call(f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	f()
	return nil
}

func (a *Agent) response(kind, id string, fields map[string]any) []byte {
	msg := map[string]any{"response_type": kind, "protocol": Protocol, "request_id": id}
	for k, v := range fields {
		msg[k] = v
	}
	out, err := v2agent.CanonicalLine(msg)
	if err != nil {
		return a.errorLine(id, ErrInternal, "response encoding failed")
	}
	return out
}

func (a *Agent) errorLine(id, code, message string) []byte {
	a.Stats.WireErrors++
	a.logf("error %s: %s", code, message)
	message = strings.Join(strings.Fields(message), " ")
	if len(message) > 240 {
		message = message[:240]
	}
	out, _ := v2agent.CanonicalLine(map[string]any{
		"response_type": "error", "protocol": Protocol, "request_id": id,
		"error": map[string]any{"code": code, "message": message},
	})
	return out
}

func (a *Agent) logf(format string, args ...any) {
	if a.opts.Log != nil {
		fmt.Fprintf(a.opts.Log, "sbv1agent: "+format+"\n", args...)
	}
}
