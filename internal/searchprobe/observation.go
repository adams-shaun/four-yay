package searchprobe

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"reflect"
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
	Board      Board
	Identities []Identity
	Events     []ObservedEvent
	Decision   *ObservedDecision
	// link is this frame's place in its collector's history chain
	// (historyDigest); nil for a scratch capture or a hand-built frame.
	link *historyLink
}

// Collector alone can read a source engine. History frames contain owned values,
// never this raw-id dictionary or a pointer/callback into the source engine.
type Collector struct {
	actor      state.PlayerID
	known      map[state.ObjID]uint32
	byRef      []state.ObjID
	introduced []Identity
	redacted   []events.Event
	// chain is the last recorded capture's history link (historyDigest);
	// hasher is extendChain's reusable hash and legacy its reusable frame.
	chain  *historyLink
	hasher hashState
	legacy legacyFrame
	// board and frameJSON are the reusable JSON buffers of a recorded
	// capture's history-chain hash (Capture), enc and frameEnc their
	// encoders; canon is every capture's reusable canonical encoder. No
	// capture retains any of them.
	board, frameJSON bytes.Buffer
	enc, frameEnc    *json.Encoder
	canon            canonEncoder
	// probeDec is probeBoundary's reusable observed decision.
	probeDec ObservedDecision
	// retainJSON makes every capture keep a copy of its encoded board in
	// Board.raw (in-package tests that inspect the bytes).
	retainJSON bool
	// noPot is capture's reusable Chars wrapper; see there.
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
	out.chain = c.chain
	out.retainJSON = c.retainJSON
	return out
}

func (c *Collector) clone() *Collector { return c.Clone() }

// Capture records the frame at e's current boundary, burst being the events
// since the previous capture on c: the frame owns everything it holds and
// extends c's history chain (historyDigest).
//
// This is the one place the observation still produces JSON: the history
// chain hashes each frame's legacy JSON encoding because Sample's seeds are
// rooted in that hash (a pinned golden: TestHistoryDigestMatchesTheJSONBoardEncoding,
// the sampler and teacher goldens). The text goes into a reusable buffer, is
// hashed and dropped; no frame keeps it and no reader parses it. A recorded
// capture runs once per real decision of the game, never inside a search.
func (c *Collector) Capture(e *rules.Engine, burst []events.Event) (Frame, error) {
	v, frame, err := c.observe(e, burst, true, true)
	if err != nil {
		return Frame{}, err
	}
	raw, err := c.encodeBoardJSON(&v)
	if err != nil {
		return Frame{}, err
	}
	c.frameJSON.Reset()
	if c.frameEnc == nil {
		c.frameEnc = json.NewEncoder(&c.frameJSON)
	}
	c.legacy = legacyFrame{Board: raw, Identities: frame.Identities, Events: frame.Events, Decision: frame.Decision}
	err = c.frameEnc.Encode(&c.legacy)
	c.legacy = legacyFrame{}
	if err != nil {
		return Frame{}, err
	}
	link, err := c.extendChain(c.frameJSON.Bytes()[:c.frameJSON.Len()-1])
	if err != nil {
		return Frame{}, err
	}
	facts := boardFacts(&v)
	if c.retainJSON {
		facts.raw = bytes.Clone(raw)
	}
	if facts.Sum, facts.Stripped, err = c.boardSums(&v, true); err != nil {
		return Frame{}, err
	}
	frame.Board, frame.link = facts, link
	return frame, nil
}

// captureScratch is Capture for a caller that only COMPARES the frame and
// drops it before the next capture (the sampler's replay). Three things are
// traded for the copy the caller does not need:
//
//   - Frame.Board carries only its digests: no facts, no history link, and
//     no JSON is produced at all.
//   - The board carries no potential_actions: the seat's own legal-offer walk
//     is skipped (noPotentialChars). The board is otherwise identical to
//     Capture's, so a caller compares against the observed board's Stripped
//     digest -- which is what Sample does, leaving the observed History (and
//     therefore the sampler's seeds) byte for byte unchanged.
//     Skipping the walk only preserves which worlds are accepted because no
//     rejection is decided by that field alone. SampleOptions.
//     ComparePotentialActions restores the walk and counts such rejections
//     (SampleResult.BoardPotentialActionsOnly): measured 2026-09-21 over 300
//     games of the ten approved pairs (3191 searched decisions, 204224
//     attempts) the count is zero.
func (c *Collector) captureScratch(e *rules.Engine, burst []events.Event, withPotential bool) (Frame, error) {
	v, frame, err := c.observe(e, burst, withPotential, true)
	if err != nil {
		return Frame{}, err
	}
	if c.retainJSON {
		raw, err := c.encodeBoardJSON(&v)
		if err != nil {
			return Frame{}, err
		}
		frame.Board.raw = bytes.Clone(raw)
	}
	if frame.Board.Sum, frame.Board.Stripped, err = c.boardSums(&v, withPotential); err != nil {
		return Frame{}, err
	}
	return frame, nil
}

// probeBoundary is what Capture(e, nil) on a throwaway Clone of c would
// report of e's board and decision, without the clone: the board's
// canonical encoding (canon.go; aliasing c's reusable buffer until c's next
// capture or probe) and the observed decision (c-owned scratch, valid
// equally long). Every identity the probe introduces is rolled back, so c is
// left exactly as it was and a later probe numbers its objects as a fresh
// clone of c would. The redeal compares every dealt world this way.
func (c *Collector) probeBoundary(e *rules.Engine) ([]byte, *ObservedDecision, error) {
	mark := len(c.byRef)
	defer c.rollback(mark)
	v, _, err := c.observe(e, nil, true, false)
	if err != nil {
		return nil, nil, err
	}
	var d *ObservedDecision
	if v.Decision != nil {
		if d, err = c.observeDecisionInto(&c.probeDec, c.probeDec.Options, v.Decision); err != nil {
			return nil, nil, err
		}
	}
	v.Decision = nil
	c.remap(&v)
	raw, err := c.canonBoard(&v)
	if err != nil {
		return nil, nil, err
	}
	return raw, d, nil
}

// rollback forgets every identity introduced after the first mark refs.
func (c *Collector) rollback(mark int) {
	for _, id := range c.byRef[mark:] {
		delete(c.known, id)
	}
	clear(c.byRef[mark:])
	c.byRef = c.byRef[:mark]
	c.introduced = c.introduced[:0]
}

// canonBoard appends v's canonical encoding into c's reusable buffer.
func (c *Collector) canonBoard(v *view.View) ([]byte, error) {
	c.canon.buf = c.canon.buf[:0]
	err := c.canon.encode(reflect.ValueOf(v).Elem(), viewPlan())
	return c.canon.buf, err
}

// boardSums is a remapped board's Sum and Stripped digests (Board). Stripped
// is the digest with every seat's potential_actions cleared -- the board a
// capture that skips the walk sees -- and is only computed apart from Sum
// when the board carries some (withPotential). It clears them in v.
func (c *Collector) boardSums(v *view.View, withPotential bool) (sum, stripped [sha256.Size]byte, err error) {
	raw, err := c.canonBoard(v)
	if err != nil {
		return sum, stripped, err
	}
	sum = sha256.Sum256(raw)
	carried := false
	if withPotential {
		for i := range v.Players {
			if len(v.Players[i].PotentialActions) > 0 {
				v.Players[i].PotentialActions = nil
				carried = true
			}
		}
	}
	if !carried {
		return sum, sum, nil
	}
	if raw, err = c.canonBoard(v); err != nil {
		return sum, stripped, err
	}
	return sum, sha256.Sum256(raw), nil
}

// encodeBoardJSON encodes v into c's reusable buffer and returns the bytes
// json.Marshal(v) would (an Encoder writes the same bytes -- same HTML
// escaping, same field order -- plus one trailing newline, dropped here).
func (c *Collector) encodeBoardJSON(v *view.View) ([]byte, error) {
	c.board.Reset()
	if c.enc == nil {
		c.enc = json.NewEncoder(&c.board)
	}
	if err := c.enc.Encode(v); err != nil {
		return nil, err
	}
	return c.board.Bytes()[:c.board.Len()-1], nil
}

// observe projects e for c's seat, introduces every displayed object, and
// (full) builds the frame's identities, redacted events and observed
// decision; the returned view is remapped to observation refs with its
// decision cleared. A probe (!full) introduces the same objects but builds
// no frame and leaves the view unremapped with its decision attached, for
// the caller to observe into scratch.
func (c *Collector) observe(e *rules.Engine, burst []events.Event, withPotential, full bool) (view.View, Frame, error) {
	if e == nil || int(c.actor) >= len(e.G.Players) {
		return view.View{}, Frame{}, fmt.Errorf("invalid observation seat or engine")
	}
	c.introduced = c.introduced[:0]
	// The observation frame never carries the seat's genesis manifest (see
	// noPotentialChars); one reusable wrapper for every capture kind keeps
	// them byte-identical.
	c.noPot.Chars = e
	c.noPot.keepPotential = withPotential
	var chars view.Chars = &c.noPot
	v := view.Project(e.G, chars, c.actor, e.Pending())
	v.Round = view.RoundOf(e.G, e.L.Events)
	// Introduce only cards explicitly displayed to this seat. Traversal order is
	// fixed, so observed identities do not encode hidden arena allocation.
	for _, p := range v.Players {
		if len(p.Commanders) > 0 {
			return view.View{}, Frame{}, fail("unsupported", "commander observation is outside the constructed probe")
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
	if !full {
		return v, Frame{}, nil
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
					return view.View{}, Frame{}, fail("unsupported", "identity-bearing note: %q", ev.Text)
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
	if len(redacted) > 0 {
		// Exactly sized: a retained frame keeps no append slack.
		frame.Events = make([]ObservedEvent, 0, len(redacted))
	}
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
		return view.View{}, Frame{}, err
	}
	v.Decision = nil // never serialize in-memory engine continuation state
	c.remap(&v)
	return v, frame, nil
}

// remap rewrites every object reference of a projected view (decision
// already cleared) into observation refs.
func (c *Collector) remap(v *view.View) {
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
