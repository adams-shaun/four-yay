// Package protocol is the versioned wire between a match host and its
// clients: one envelope, a closed set of frame types, and JSON bodies whose
// TypeScript twins cmd/gentypes generates. It is types only — it never
// imports rules, so a client library can depend on it without pulling in
// the engine.
package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/adams-shaun/gorge/view"
)

// Version bumps only on a breaking change to a frame body. Additive fields
// do not bump it.
const Version = 1

// FrameType is the envelope's discriminator.
type FrameType string

const (
	THello       FrameType = "hello"
	TWidget      FrameType = "widget"
	TMatchStart  FrameType = "match_start"
	TSnapshot    FrameType = "snapshot"
	TEvent       FrameType = "event"
	TDecision    FrameType = "decision"
	TMatchEnd    FrameType = "match_end"
	TTableHalted FrameType = "table_halted"
	TOverflow    FrameType = "overflow"
	TError       FrameType = "error"
	TRewind      FrameType = "rewind"
)

// Subscription modes and the wildcard table.
const (
	ModeOverview = "overview"
	ModeFocus    = "focus"
	TableAll     = "*"
)

// Table states as shown in TableInfo.State / Widget.State.
const (
	TableIdle     = "idle"
	TableLive     = "live"
	TableCooldown = "cooldown"
	TableHalted   = "halted"
)

// Match states as recorded in a sidecar and shown in MatchInfo.State.
const (
	MatchLive     = "live"
	MatchFinished = "finished"
	MatchAborted  = "aborted"
	MatchCrashed  = "crashed"
)

// Frame is the envelope. ID is the session-wide frame counter and the SSE
// id; 0 means "not resumable" (widgets). Table/Match/Seq locate the body in
// a match's chain: Seq is the engine's own event sequence, the number the
// hash chain covers.
type Frame struct {
	V     int             `json:"v"`
	T     FrameType       `json:"t"`
	ID    uint64          `json:"id,omitempty"`
	Table string          `json:"table,omitempty"`
	Match int             `json:"match,omitempty"`
	Seq   uint64          `json:"seq"`
	Body  json.RawMessage `json:"body"`
}

// NewFrame marshals body into an envelope of the current Version.
func NewFrame(t FrameType, table string, match int, seq uint64, body any) (Frame, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Frame{}, fmt.Errorf("protocol: encode %s body: %w", t, err)
	}
	return Frame{V: Version, T: t, Table: table, Match: match, Seq: seq, Body: raw}, nil
}

// Decode unmarshals the body into a typed struct.
func (f Frame) Decode(into any) error {
	if err := json.Unmarshal(f.Body, into); err != nil {
		return fmt.Errorf("protocol: decode %s body: %w", f.T, err)
	}
	return nil
}

// Hello opens every stream: the session id the client echoes in POSTs and
// the table list as of now.
type Hello struct {
	Session string      `json:"session"`
	Tables  []TableInfo `json:"tables"`
}

// TableInfo is one row of the overview.
type TableInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Seats     int    `json:"seats"`
	Spectator string `json:"spectator"`
	State     string `json:"state"`
	Match     int    `json:"match"`
	Perpetual bool   `json:"perpetual"`
	// Format is the table's construction format, "constructed" or
	// "commander" (host.Format.String()). Always emitted: the zero
	// Format is "constructed", a real value, so an omitted field
	// would be indistinguishable from it.
	Format string `json:"format"`
	// BotPolicy is the effective hosted policy for bot seats and human-seat
	// caretakers. It is public table configuration, never private game state.
	BotPolicy string `json:"bot_policy"`
	// SeatNames names the table's seats in seat order: the deck name each
	// seat is playing, as MatchStart carries them. Empty when no match
	// has started on this table yet. It is public information -- the
	// overview already shows every table's life totals and deck
	// identities in its transcript -- so it is not redacted per viewer.
	// omitempty is right here, unlike Format: an absent list and an empty
	// list mean the same thing (no match yet), so there is no value that
	// omitting would make ambiguous.
	SeatNames []string `json:"seat_names,omitempty"`
	// Mulligans is the table's London mulligan allowance (TableConfig.Mulligans):
	// each player may take up to this many pre-game mulligans. Always emitted,
	// like Format: the zero value is 0, a real value that disables the pre-game
	// round, so an omitted field would be indistinguishable from it. It is
	// public table configuration, never private game state — a restart control
	// reads it to recreate the same game.
	Mulligans int `json:"mulligans"`
	// AutoMana enables the human payment-plan UI for this table.
	AutoMana bool `json:"auto_mana"`
}

// Widget is the overview cell: enough to draw a 2x2 life grid, a turn
// marker and a stack-depth badge, plus the last transcript line.
type Widget struct {
	Turn       int32   `json:"turn"`
	Step       string  `json:"step"`
	Phase      string  `json:"phase"`
	Active     uint8   `json:"active"`
	Priority   uint8   `json:"priority"`
	Life       []int32 `json:"life"`
	Lost       []bool  `json:"lost"`
	StackDepth int     `json:"stack_depth"`
	Last       string  `json:"last"`
	State      string  `json:"state"`
}

// SeatInfo names a seat for the identity bars; Colour is the seat colour
// the client keeps consistent from overview to focused view. Human marks a
// seat played by a real person (TableConfig.Humans): additive and omitempty
// (old sidecars and every bot seat omit it), it is what tells a client
// whether an undo control may be offered — the registry refuses an undo on
// any table whose human seats are not all the requester, so a seat that can
// see two humans on the wire can know its UNDO button would be refused
// (dispatch fb-20260911T201015Z).
type SeatInfo struct {
	Name   string `json:"name"`
	Deck   string `json:"deck"`
	Colour string `json:"colour"`
	Human  bool   `json:"human,omitempty"`
	// DeckID is the exact deck id this seat plays — the TableConfig.Decks
	// entry the match loaded (host/match.go's deckNames), NOT the deck's
	// display Name. The two can differ: cmd/gorged's catalogue falls back to
	// the file stem only when a deck file has no Name. A client that restarts
	// the game must post this id back as human_deck/bot_deck; posting the
	// display name would 400 "unknown deck". omitempty matches Human's
	// additive shape.
	DeckID string `json:"deck_id,omitempty"`
}

// MatchStart announces match k on a subscribed table.
type MatchStart struct {
	Seats     []SeatInfo `json:"seats"`
	Seed      uint64     `json:"seed"`
	Spectator string     `json:"spectator"`
	BotPolicy string     `json:"bot_policy"`
}

// Snapshot is the whole view at Head plus the turn-start seqs so far — the
// DVR's scrub ticks.
type Snapshot struct {
	View       view.View `json:"view"`
	TurnStarts []uint64  `json:"turn_starts"`
	Head       uint64    `json:"head"`
	// Seats is the live match's seat list, the same []SeatInfo MatchStart
	// carries. A focus subscriber (host/session.go's Subscribe) receives a
	// snapshot but not the match's match_start frame — that went out before
	// it connected — so without this the table route has no way to learn the
	// seats for the current match (ui16). Always populated for a live match;
	// never omitempty, so the client type stays a plain SeatInfo[].
	Seats []SeatInfo `json:"seats"`
}

// EventBody is one redacted event with its transcript line.
type EventBody struct {
	Event Event  `json:"event"`
	Line  string `json:"line"`
}

// DecisionBody says who is being asked what; options come with the player
// seat (M2b), not here.
type DecisionBody struct {
	Player uint8  `json:"player"`
	Kind   string `json:"kind"`
	Prompt string `json:"prompt"`
}

// MatchEnd closes a match: Result is "win" or "draw"; Winner is null for a
// draw; Head is the chain head the .events file replays to.
type MatchEnd struct {
	Result string `json:"result"`
	Winner *uint8 `json:"winner"`
	Head   string `json:"head"`
}

// TableHaltedBody is spec D15: a crashed table stops and says why.
type TableHaltedBody struct {
	Reason string `json:"reason"`
}

// Rewind announces an in-place rollback of the live match it is addressed
// to (dispatch fb-20260911T201015Z, the undo slice): the match id and its
// hash chain are unchanged, but every event past Head was undone — later
// seqs are re-used by the replayed game and mean different events than
// they did before the rewind. The body is a full Snapshot at the new head
// (the same shape a fresh focus subscription receives), so a client
// applies it exactly like any snapshot frame and, unlike a snapshot,
// DISCARDS everything it holds past Head first: pending decisions,
// buffered intents and transcript lines past Head are all stale. The
// frame's own envelope Seq is the new head.
type Rewind = Snapshot

// Overflow is the last frame on a stream whose session channel filled.
type Overflow struct {
	Dropped int `json:"dropped"`
}

// ErrorBody is every error reply and the error frame. Head is set only on
// a 409 "seq beyond head" reply: the last valid seq, so a client can clamp.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Head    uint64 `json:"head,omitempty"`
}

// MatchInfo is one row of a table's match list, from its sidecar.
type MatchInfo struct {
	Table     string     `json:"table"`
	Match     int        `json:"match"`
	Seed      uint64     `json:"seed"`
	Seats     []SeatInfo `json:"seats"`
	State     string     `json:"state"`
	Result    string     `json:"result,omitempty"`
	Winner    *uint8     `json:"winner"`
	Head      string     `json:"head,omitempty"`
	Events    int        `json:"events"`
	Turns     int32      `json:"turns"`
	BotPolicy string     `json:"bot_policy"`
}

// Subscribe and Unsubscribe are the POST bodies.
type Subscribe struct {
	Session string `json:"session"`
	Table   string `json:"table"`
	Mode    string `json:"mode"`
}

type Unsubscribe struct {
	Session string `json:"session"`
	Table   string `json:"table"`
}

// SeatColours are assigned by seat index and never change during a match.
var SeatColours = [...]string{"#e5484d", "#3b82f6", "#22c55e", "#eab308", "#a855f7", "#f97316", "#14b8a6", "#ec4899"}
