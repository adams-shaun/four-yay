package searchprobe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/deck"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
	"github.com/adams-shaun/gorge/view"
)

type Identity struct {
	ID    uint32
	Name  string
	Owner state.PlayerID
}
type ObservedEvent struct {
	Kind          events.Kind
	Player        state.PlayerID
	Obj           uint32
	From, To      state.Zone
	Amount        int32
	Step          state.Step
	Counter, Text string
	IDs           []uint32
	Pairs         [][2]uint32
	Secret        bool
}
type Frame struct {
	Board      json.RawMessage
	Identities []Identity
	Events     []ObservedEvent
	Decision   *ObservedDecision
}

// Collector alone can read a source engine. History frames contain owned values,
// never this raw-id dictionary or a pointer/callback into the source engine.
type Collector struct {
	actor      state.PlayerID
	known      map[state.ObjID]uint32
	byRef      []state.ObjID
	introduced []Identity
	redacted   []events.Event
	// board is captureScratch's reusable encode buffer; see there.
	board bytes.Buffer
	// noPot is captureScratch's reusable Chars wrapper; see there.
	noPot noPotentialChars
}

// noPotentialChars is a view.Chars whose seat projection carries no
// potential_actions. The field is rules.PotentialActions -- a whole second
// legal-offer walk per frame, priced against the hypothetical tapped-out pool
// -- and it is the single most expensive derived fact in a capture. A replay
// that only COMPARES its frames does not need it: see captureScratch.
//
// It also suppresses OwnDeck. The seat's genesis manifest is constant setup
// metadata: no frame compares it, no event carries it, and attaching it costs
// a per-capture Clone (view.Chars.OwnDeck) on this hot path. A seat that needs
// the manifest gets it from the ordinary view / botpolicy.Board path
// (bots.Env, searchseat), never from the observation frame.
type noPotentialChars struct {
	view.Chars
	// keepPotential is true when the caller asked for the full offer walk
	// (SampleOptions.ComparePotentialActions); false is the production
	// scratch reuse.
	keepPotential bool
}

func (c noPotentialChars) PotentialActions(p state.PlayerID) []decision.PotentialAction {
	if c.keepPotential {
		return c.Chars.PotentialActions(p)
	}
	return nil
}

func (noPotentialChars) OwnDeck(state.PlayerID) *deck.Manifest { return nil }

// SuppressOwnLibrary tells view.Project not to build the own-library CONTENTS
// list at all. The observation frame must never carry it, exactly as it never
// carries PotentialActions or OwnDeck: the sampler's whole purpose is to
// search over the draws the seat has not seen, and the recorded engine and the
// hypothetical engine allocate hidden library objects from different arenas,
// so a raw library in the board can never compare equal (before this,
// TestSampleRealDeckGolden reported every world rejected at frame 0 on "board
// state"). The seat's own library CONTENTS are a view fact now
// (own_library_list, view.PlayerView.Library), but the observation is the
// deliberately limited knowledge model the search reasons from, not the
// seat's full view -- the same split OwnDeck already makes. Suppressing it at
// the projection also keeps the sampler's hottest path from allocating a list
// it would discard (TestCollectorCaptureReusesRedactionStorageWithoutAliasingFrames).
// LibraryTop is deliberately NOT suppressed: a live MayLookAt grant makes it
// legitimately revealed information, and the capture introduces and remaps it.
func (noPotentialChars) SuppressOwnLibrary() bool { return true }

func NewCollector(actor state.PlayerID) *Collector {
	return &Collector{actor: actor, known: make(map[state.ObjID]uint32), byRef: []state.ObjID{0}}
}

// Clone returns an independent copy at the current observation boundary.
// It is used by undo-style diagnostics that retain an earlier engine and must
// later map semantic actions back into that engine's raw object ids.
func (c *Collector) Clone() *Collector {
	out := NewCollector(c.actor)
	for id, ref := range c.known {
		out.known[id] = ref
	}
	out.byRef = append(out.byRef[:0], c.byRef...)
	out.introduced = append([]Identity(nil), c.introduced...)
	return out
}

func (c *Collector) clone() *Collector { return c.Clone() }

func (c *Collector) Capture(e *rules.Engine, burst []events.Event) (Frame, error) {
	return c.capture(e, burst, false, true)
}

// captureScratch is Capture for a caller that only COMPARES the frame and
// drops it before the next capture (the sampler's replay). Two things are
// traded for the copy the caller does not need:
//
//   - Frame.Board aliases the collector's reusable encode buffer instead of
//     owning a fresh copy, the largest single allocation of a capture. The
//     frame must not be retained past the next capture on c.
//   - The board carries no potential_actions: the seat's own legal-offer walk
//     is skipped (noPotentialChars). The bytes are otherwise identical to
//     Capture's, so a caller compares against stripPotentialActions of the
//     observed board -- which is what Sample does, leaving the observed
//     History (and therefore the sampler's seeds) byte for byte unchanged.
//     Skipping the walk only preserves which worlds are accepted because no
//     rejection is decided by that field alone. SampleOptions.
//     ComparePotentialActions restores the walk and counts such rejections
//     (SampleResult.BoardPotentialActionsOnly): measured 2026-09-21 over 300
//     games of the ten approved pairs (3191 searched decisions, 204224
//     attempts) the count is zero.
func (c *Collector) captureScratch(e *rules.Engine, burst []events.Event, withPotential bool) (Frame, error) {
	return c.capture(e, burst, true, withPotential)
}

func (c *Collector) capture(e *rules.Engine, burst []events.Event, scratch, withPotential bool) (Frame, error) {
	if e == nil || int(c.actor) >= len(e.G.Players) {
		return Frame{}, fmt.Errorf("invalid observation seat or engine")
	}
	c.introduced = nil
	// The observation frame never carries the seat's genesis manifest (see
	// noPotentialChars); one reusable wrapper for both the recorded and the
	// scratch capture keeps them byte-identical.
	c.noPot.Chars = e
	c.noPot.keepPotential = withPotential
	var chars view.Chars = &c.noPot
	v := view.Project(e.G, chars, c.actor, e.Pending())
	v.Round = view.RoundOf(e.G, e.L.Events)
	// Introduce only cards explicitly displayed to this seat. Traversal order is
	// fixed, so observed identities do not encode hidden arena allocation.
	for _, p := range v.Players {
		if len(p.Commanders) > 0 {
			return Frame{}, fail("unsupported", "commander observation is outside the constructed probe")
		}
		for _, zone := range [][]view.CardView{p.Battlefield, p.Hand, p.Graveyard, p.Exile, p.Command} {
			for _, card := range zone {
				c.introduce(e, card.ID)
			}
		}
		if p.LibraryTop != nil {
			c.introduce(e, p.LibraryTop.ID)
		}
	}
	for _, s := range v.Stack {
		c.introduce(e, s.ID)
		c.introduce(e, s.Source)
	}
	for _, p := range v.Pending {
		c.introduce(e, p.Source)
	}
	if d := v.Decision; d != nil {
		c.introduce(e, d.Source)
		for _, o := range d.Options {
			c.introduce(e, o.Obj)
			c.introduce(e, o.Attacker)
		}
	}
	redacted := c.redacted[:0]
	defer func() {
		clear(redacted)
		c.redacted = redacted[:0]
	}()
	for _, raw := range burst {
		ev := view.RedactEvent(e.G, raw, c.actor)
		if ev.Kind == events.Shuffle || ev.Kind == events.LibraryOrder {
			// Retain occurrence, never any whole-library payload, even for owner.
			ev = events.Event{Kind: ev.Kind, Player: ev.Player, Secret: ev.Secret}
		}
		if ev.Kind == events.DecisionMade {
			ev.Text = ""
		}
		// Explicit reveal/look channels and visible zone transitions can name a
		// card absent from the end-of-burst board. No other unknown reference is
		// a license to query that object's hidden card identity.
		if !ev.Secret || ev.Player == c.actor {
			switch ev.Kind {
			case events.Note:
				if len(ev.IDs) > 0 && ev.Text != "" && ev.Text != "looks at the top of the library" && !(strings.HasPrefix(ev.Text, "revealed ") && strings.HasSuffix(ev.Text, " as a cost")) {
					return Frame{}, fail("unsupported", "identity-bearing note: %q", ev.Text)
				}
				for _, id := range ev.IDs {
					c.introduce(e, id)
				}
			case events.MoveZone, events.Draw, events.PutOnStack:
				if !ev.From.Hidden() || !ev.To.Hidden() || (ev.Secret && ev.Player == c.actor) {
					c.introduce(e, ev.Obj)
				}
			}
		}
		redacted = append(redacted, ev)
	}
	frame := Frame{Identities: append([]Identity(nil), c.introduced...)}
	for _, ev := range redacted {
		out := ObservedEvent{Kind: ev.Kind, Player: ev.Player, Obj: c.ref(ev.Obj), From: ev.From, To: ev.To, Amount: ev.Amount, Step: ev.Step, Counter: ev.Counter, Text: ev.Text, Secret: ev.Secret}
		for _, id := range ev.IDs {
			if ref := c.ref(id); ref != 0 {
				out.IDs = append(out.IDs, ref)
			}
		}
		for _, pair := range ev.Pairs {
			a, b := c.ref(pair[0]), c.ref(pair[1])
			if a != 0 && b != 0 {
				out.Pairs = append(out.Pairs, [2]uint32{a, b})
			}
		}
		frame.Events = append(frame.Events, out)
	}
	var err error
	frame.Decision, err = c.observeDecision(v.Decision)
	if err != nil {
		return Frame{}, err
	}
	v.Decision = nil // never serialize in-memory engine continuation state
	for i := range v.Players {
		p := &v.Players[i]
		p.Hand = c.cards(p.Hand)
		p.Battlefield = c.cards(p.Battlefield)
		p.Graveyard = c.cards(p.Graveyard)
		p.Exile = c.cards(p.Exile)
		p.Command = c.cards(p.Command)
		if p.LibraryTop != nil {
			card := c.card(*p.LibraryTop)
			p.LibraryTop = &card
		}
		// PotentialActions (the viewer's own offer walk, view/view.go) names
		// its objects by engine ObjID like every other board field; left raw,
		// a sampled world whose hidden objects were allocated different IDs
		// can never serialize the same board, and every world is rejected at
		// its first frame. Map them through the same observation refs.
		if len(p.PotentialActions) > 0 {
			pa := append([]decision.PotentialAction(nil), p.PotentialActions...)
			for j := range pa {
				pa[j].Obj = state.ObjID(c.ref(pa[j].Obj))
			}
			p.PotentialActions = pa
		}
	}
	for i := range v.Stack {
		s := &v.Stack[i]
		s.ID = state.ObjID(c.ref(s.ID))
		s.Source = state.ObjID(c.ref(s.Source))
		if s.Card != nil {
			card := c.card(*s.Card)
			s.Card = &card
		}
		s.Targets = append([]view.TargetView(nil), s.Targets...)
		for j := range s.Targets {
			s.Targets[j].Obj = state.ObjID(c.ref(s.Targets[j].Obj))
		}
	}
	for i := range v.Pending {
		v.Pending[i].Source = state.ObjID(c.ref(v.Pending[i].Source))
	}
	if !scratch {
		frame.Board, err = json.Marshal(v)
		return frame, err
	}
	// json.Marshal encodes into a pooled buffer and then copies the result
	// out; an Encoder writes the same bytes (same HTML escaping, same field
	// order) plus one trailing newline straight into a buffer we keep.
	c.board.Reset()
	if err := json.NewEncoder(&c.board).Encode(v); err != nil {
		return Frame{}, err
	}
	frame.Board = c.board.Bytes()[:c.board.Len()-1]
	return frame, nil
}

// stripPotentialActions removes every `"potential_actions"` member from a
// marshalled view.View, yielding exactly the bytes the same view marshals to
// when no seat carries the field (it is tagged omitempty, so a nil slice is an
// absent key). The scan is string-aware, so a card name spelling the key is
// not a member. TestStripPotentialActionsMatchesASkippedCapture pins the
// equality against a real capture on every frame of the bench fixture.
func stripPotentialActions(board []byte) []byte {
	const key = `,"potential_actions":`
	if !bytes.Contains(board, []byte(key)) {
		return board
	}
	out := make([]byte, 0, len(board))
	for i := 0; i < len(board); {
		switch c := board[i]; {
		case c == '"':
			end := skipJSONString(board, i)
			out = append(out, board[i:end]...)
			i = end
		case c == ',' && bytes.HasPrefix(board[i:], []byte(key)):
			i = skipJSONValue(board, i+len(key))
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// skipJSONString returns the index just past the string literal opening at i.
func skipJSONString(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return i
}

// skipJSONValue returns the index just past the value starting at i.
func skipJSONValue(b []byte, i int) int {
	depth := 0
	for i < len(b) {
		switch c := b[i]; c {
		case '"':
			i = skipJSONString(b, i)
			if depth == 0 {
				return i
			}
			continue
		case '[', '{':
			depth++
		case ']', '}':
			if depth--; depth <= 0 {
				if depth < 0 {
					return i
				}
				return i + 1
			}
		case ',':
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return i
}

func (c *Collector) introduce(e *rules.Engine, id state.ObjID) {
	if id == 0 || c.known[id] != 0 {
		return
	}
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	ref := uint32(len(c.known) + 1)
	name := ""
	if o.Card != nil && len(o.Card.Faces) > 0 {
		name = o.Card.Faces[0].Name
	}
	c.known[id] = ref
	c.byRef = append(c.byRef, id)
	c.introduced = append(c.introduced, Identity{ID: ref, Name: name, Owner: o.Owner})
}

func (c *Collector) ref(id state.ObjID) uint32 {
	if _, ok := id.PlayerRef(); ok {
		return uint32(id)
	}
	return c.known[id]
}

func (c *Collector) object(ref uint32) state.ObjID {
	if ref == 0 || int(ref) >= len(c.byRef) {
		return 0
	}
	return c.byRef[ref]
}

func (c *Collector) card(card view.CardView) view.CardView {
	card.ID = state.ObjID(c.ref(card.ID))
	card.Token = ""
	card.AttachedTo = state.ObjID(c.ref(card.AttachedTo))
	card.BlockedBy = append([]state.ObjID(nil), card.BlockedBy...)
	for i := range card.BlockedBy {
		card.BlockedBy[i] = state.ObjID(c.ref(card.BlockedBy[i]))
	}
	return card
}

// cards rewrites a projected zone in place. view.Project builds every zone
// slice (and every CardView.BlockedBy) fresh per call and hands ownership to
// the caller, so there is nothing to alias; the copy this used to make was the
// second largest allocation of a capture.
func (c *Collector) cards(cards []view.CardView) []view.CardView {
	for i := range cards {
		card := &cards[i]
		card.ID = state.ObjID(c.ref(card.ID))
		card.Token = ""
		card.AttachedTo = state.ObjID(c.ref(card.AttachedTo))
		for j := range card.BlockedBy {
			card.BlockedBy[j] = state.ObjID(c.ref(card.BlockedBy[j]))
		}
	}
	return cards
}
