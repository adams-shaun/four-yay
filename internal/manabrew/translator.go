package manabrew

import (
	"errors"
	"strconv"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/view"

	mb "github.com/adams-shaun/gorge/protocol/manabrew"
)

var (
	ErrUnmapped    = errors.New("manabrew: decision kind is not mapped yet")
	errNilDecision = errors.New("manabrew: nil decision")
)

// CardText is the operator-approved card-text seam (scoping spec §10.1 Q9):
// the translator asks it for a card's rules text by the NAME the seat's
// redacted CardView already shows, never by anything hidden. gorged
// implements it over its Scryfall catalog (MB-16); nil means every CardDto
// carries the empty text, which is the v1 default (gap G-7).
type CardText interface {
	Text(name string) (string, bool)
}

// Translator is the ManaBrew mapping for one (table, match). It is a pure
// function object: it holds no engine, no host handle and no clock, and
// State/Prompt are pure functions of their inputs.
type Translator struct {
	table string
	match int64
	text  CardText
}

// New builds a Translator for a table's match. text may be nil.
func New(table string, match int64, text CardText) *Translator {
	return &Translator{table: table, match: match, text: text}
}

// GameID mints the ManaBrew gameId (spec §6.1): "<table>/<match>".
//
// The published protocol's own example uses a three-segment form
// ("table/1/matches/3"); the scoping spec pins gorge's mapping to the
// two-segment "<table>/<match>", and the scoping spec is the source of
// truth here. A ManaBrew client treats the id as an opaque string.
func (t *Translator) GameID() string {
	return t.table + "/" + strconv.FormatInt(t.match, 10)
}

// State projects a seat-scoped view into the engine→client `state` message.
func (t *Translator) State(v view.View) mb.EngineMessage {
	return mb.EngineMessage{Value: mb.StateUpdate{Kind: "state", GameView: t.gameView(v)}}
}

// Prompt maps the engine's pending decision into the ManaBrew prompt for its
// decision.Kind. v is the seat view accompanying the prompt (the per-kind
// builders read card shapes off it); a nil v is accepted where a builder
// needs no view.
//
// Until each kind's MB ticket lands, its stub returns an error wrapping
// ErrUnmapped. Later tickets edit only their own stub files, never
// dispatch.go.
func (t *Translator) Prompt(d *decision.Decision, v *view.View) (mb.PromptMessage, error) {
	if d == nil {
		return mb.PromptMessage{}, errNilDecision
	}
	return t.dispatch(d, v)
}
