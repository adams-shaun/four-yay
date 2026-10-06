package searchprobe

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
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
	// unchained marks a frame recorded by an Unchain'ed collector: it has
	// no link, and historyDigest refuses a history that holds one rather
	// than root Sample's seeds in a different digest.
	unchained bool
}

// Collector alone can read a source engine. History frames contain owned values,
// never this raw-id dictionary or a pointer/callback into the source engine.
type Collector struct {
	actor state.PlayerID
	// known is the raw-id dictionary, dense by engine ObjID (object ids are
	// handed out from 1 upward): known[id] is id's observation ref, or 0.
	// byRef is its inverse, byRef[ref] = id (byRef[0] is the zero id).
	known      []uint32
	byRef      []state.ObjID
	gen        uint64 // bumped whenever known changes (Forker)
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
	// rec is the append-only arena recorded frames are written into (they
	// own what they hold); scratch is captureScratch's, reset per capture.
	rec, scratch frameArena
	// probeDec/probeOpts are probeBoundary's reusable observed decision and
	// obsDec/obsOpts ObserveDecision's; cands is Candidates' storage.
	probeDec, obsDec   ObservedDecision
	probeOpts, obsOpts []ObservedOption
	candPool, candOut  []Action
	// keyActs and keyBuf are IntentKey's reusable action list and key.
	keyActs []Action
	keyBuf  []byte
	// retainJSON makes every capture keep a copy of its encoded board in
	// Board.raw (in-package tests that inspect the bytes).
	retainJSON bool
	// unchained skips the JSON history chain (Unchain).
	unchained bool
	// noPot is capture's reusable Chars wrapper; see there.
	noPot noPotentialChars
	// view is observe's reusable projection (view.ProjectInto): every
	// capture and probe refills it, so the view observe returns -- and every
	// slice in it -- is valid only until c's next observation. No frame
	// retains any of it (boardFacts copies, the digests hash it). viewDec
	// keeps the projected decision's storage across the observations that
	// clear the returned view's decision.
	view    view.View
	viewDec *decision.Decision
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

// BeginDerivedReads/EndDerivedReads pass the engine's pure-read memo scope
// through the wrapper (view.projectInto opens it), which the embedded
// view.Chars interface would otherwise hide.
func (c noPotentialChars) BeginDerivedReads() {
	if e, ok := c.Chars.(*rules.Engine); ok {
		e.BeginDerivedReads()
	}
}

func (c noPotentialChars) EndDerivedReads() {
	if e, ok := c.Chars.(*rules.Engine); ok {
		e.EndDerivedReads()
	}
}

// viewCharacteristics is view's optional one-call characteristics read
// (rules.Engine.ViewCharacteristics).
type viewCharacteristics interface {
	ViewCharacteristics(state.ObjID) (string, []string, int32, int32)
}

// ViewCharacteristics forwards the projection's one-call read of a card's
// keywords, power and toughness (one Derived instead of the Power, Toughness
// and Keywords walks the projector makes without it) -- every capture and
// every redeal probe projects each visible card through it. The wrapper
// embeds the view.Chars INTERFACE, so the projector's assertion sees only
// what is declared here: without this method the engine's own
// ViewCharacteristics was silently dropped.
//
// The name is withheld (""), so the projector keeps the printed name: this
// wrapper has never forwarded the engine's Name either (the observation
// projects printed names, and every frame and probe compares under that
// rule).
func (c noPotentialChars) ViewCharacteristics(id state.ObjID) (string, []string, int32, int32) {
	if vc, ok := c.Chars.(viewCharacteristics); ok {
		_, kw, p, t := vc.ViewCharacteristics(id)
		return "", kw, p, t
	}
	return "", c.Chars.Keywords(id), c.Chars.Power(id), c.Chars.Toughness(id)
}

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

// SuppressCardTokens tells view.Project not to format CardView.Token: remap
// blanks the token of every card it rewrites. The one CardView list remap
// does not rewrite, a planar deck, gets its tokens back there, so no board
// changes.
func (noPotentialChars) SuppressCardTokens() bool { return true }

func NewCollector(actor state.PlayerID) *Collector {
	return &Collector{actor: actor, byRef: []state.ObjID{0}}
}

// Clone returns an independent copy at the current observation boundary.
// It is used by undo-style diagnostics that retain an earlier engine and must
// later map semantic actions back into that engine's raw object ids.
func (c *Collector) Clone() *Collector {
	out := NewCollector(c.actor)
	out.known = append([]uint32(nil), c.known...)
	out.byRef = append(out.byRef[:0], c.byRef...)
	out.introduced = append([]Identity(nil), c.introduced...)
	out.chain = c.chain
	out.retainJSON = c.retainJSON
	out.unchained = c.unchained
	return out
}

// Unchain stops c's recorded captures from extending the history chain --
// the per-frame JSON encoding and hash that only historyDigest (Sample's
// seed root) reads. A seat whose world source never calls Sample (azmcts:
// the clairvoyant and redeal sources) skips that work on every real
// decision. Every frame captured from here on is marked unchained, and
// historyDigest -- so Sample -- refuses a history holding one, so an
// unchained stream can never silently seed a sampler differently.
func (c *Collector) Unchain() { c.unchained = true }

func (c *Collector) clone() *Collector { return c.Clone() }

// Introduced is how many objects c has given references: references run
// 1..Introduced() in introduction order, so an object whose reference
// exceeds an earlier Introduced() count was first observed after it.
func (c *Collector) Introduced() int { return len(c.byRef) - 1 }

// Forker hands out clones of one collector to a caller that uses them one
// at a time, each dead before the next is asked for (internal/azmcts's
// simulations: every world's observer is a clone of the root observer, and
// a simulation's observer is spent when the next simulation starts). Fork
// returns the SAME collector every time, rolled back to the base's boundary,
// which is exactly the state a fresh Clone of the base would have; only when
// the base itself has changed since does it clone again.
type Forker struct {
	base    *Collector
	c       *Collector
	baseGen uint64
	mark    int
}

// Forker returns a Forker over c.
func (c *Collector) Forker() *Forker { return &Forker{base: c} }

// Fork returns a clone of the base at its current boundary, valid until the
// next Fork.
func (f *Forker) Fork() *Collector {
	if f.c == nil || f.base.gen != f.baseGen {
		f.c = f.base.Clone()
		f.baseGen, f.mark = f.base.gen, len(f.c.byRef)
		return f.c
	}
	f.c.rollback(f.mark)
	f.c.chain = f.base.chain
	return f.c
}

// Capture records the frame at e's current boundary, burst being the events
// since the previous capture on c: the frame owns everything it holds (cut
// from c's append-only recording arena) and extends c's history chain
// (historyDigest).
//
// This is the one place the observation still produces JSON: the history
// chain hashes each frame's legacy JSON encoding because Sample's seeds are
// rooted in that hash (a pinned golden: TestHistoryDigestMatchesTheJSONBoardEncoding,
// the sampler and teacher goldens). The text goes into a reusable buffer, is
// hashed and dropped; no frame keeps it and no reader parses it. A recorded
// capture runs once per real decision of the game, never inside a search.
func (c *Collector) Capture(e *rules.Engine, burst []events.Event) (Frame, error) {
	v, frame, err := c.observe(e, burst, true, &c.rec)
	if err != nil {
		return Frame{}, err
	}
	if c.unchained {
		frame.Board = boardFacts(&v, &c.rec)
		if c.retainJSON {
			raw, err := c.encodeBoardJSON(&v)
			if err != nil {
				return Frame{}, err
			}
			frame.Board.raw = bytes.Clone(raw)
		}
		if frame.Board.Sum, frame.Board.Stripped, err = c.boardSums(&v, true); err != nil {
			return Frame{}, err
		}
		frame.unchained = true
		return frame, nil
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
	link, err := c.extendChain(c.frameJSON.Bytes()[:c.frameJSON.Len()-1], &c.rec)
	if err != nil {
		return Frame{}, err
	}
	frame.Board = boardFacts(&v, &c.rec)
	if c.retainJSON {
		frame.Board.raw = bytes.Clone(raw)
	}
	if frame.Board.Sum, frame.Board.Stripped, err = c.boardSums(&v, true); err != nil {
		return Frame{}, err
	}
	frame.link = link
	return frame, nil
}

// CaptureBoardJSON is Capture that also returns the frame's board as the
// JSON it encoded -- the legacy Board encoding a frame used to carry -- for
// a caller that digests that text (internal/searchbench's
// PublicStateDigest).
func (c *Collector) CaptureBoardJSON(e *rules.Engine, burst []events.Event) (Frame, []byte, error) {
	keep := c.retainJSON
	c.retainJSON = true
	f, err := c.Capture(e, burst)
	c.retainJSON = keep
	if err != nil {
		return Frame{}, nil, err
	}
	raw := f.Board.raw
	if !keep {
		f.Board.raw = nil
	}
	return f, raw, nil
}

// captureScratch is Capture for a caller that only COMPARES the frame and
// drops it before the next capture (the sampler's replay). Three things are
// traded for the copy the caller does not need:
//
//   - The frame lives in c's scratch arena, reused by the next scratch
//     capture: it must not be retained past it. Its Board carries only its
//     digests (no facts, no history link), and no JSON is produced.
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
	c.scratch.reset()
	v, frame, err := c.observe(e, burst, withPotential, &c.scratch)
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
	v, _, err := c.observe(e, nil, true, nil)
	if err != nil {
		return nil, nil, err
	}
	var d *ObservedDecision
	if v.Decision != nil {
		d, err = c.observeDecisionInto(&c.probeDec, c.probeOpts[:0], v.Decision)
		if len(c.probeDec.Options) > cap(c.probeOpts) {
			c.probeOpts = c.probeDec.Options
		}
		if err != nil {
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
	if mark >= len(c.byRef) {
		c.introduced = c.introduced[:0]
		return
	}
	for _, id := range c.byRef[mark:] {
		c.known[id] = 0
	}
	clear(c.byRef[mark:])
	c.byRef = c.byRef[:mark]
	c.introduced = c.introduced[:0]
	c.gen++
}

// canonBoard appends v's canonical encoding into c's reusable buffer.
func (c *Collector) canonBoard(v *view.View) ([]byte, error) {
	c.canon.buf = c.canon.buf[:0]
	err := c.canon.encodeView(v)
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
// builds the frame's identities, redacted events and observed decision in
// a; the returned view is remapped to observation refs with its decision
// cleared. A probe (nil a) introduces the same objects but builds no frame
// and leaves the view unremapped with its decision attached, for the caller
// to observe into its own scratch.
func (c *Collector) observe(e *rules.Engine, burst []events.Event, withPotential bool, a *frameArena) (view.View, Frame, error) {
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
	if c.view.Decision == nil {
		c.view.Decision = c.viewDec
	}
	view.ProjectInto(&c.view, e.G, chars, c.actor, e.Pending())
	if c.view.Decision != nil {
		c.viewDec = c.view.Decision
	}
	c.view.Round = view.RoundOf(e.G, e.L.Events)
	v := c.view
	// Introduce only cards explicitly displayed to this seat. Traversal order is
	// fixed, so observed identities do not encode hidden arena allocation.
	for i := range v.Players {
		p := &v.Players[i]
		if len(p.Commanders) > 0 {
			return view.View{}, Frame{}, fail("unsupported", "commander observation is outside the constructed probe")
		}
		for _, zone := range [...][]view.CardView{p.Battlefield, p.Hand, p.Graveyard, p.Exile, p.Command} {
			for j := range zone {
				c.introduce(e, zone[j].ID)
			}
		}
		if p.LibraryTop != nil {
			c.introduce(e, p.LibraryTop.ID)
		}
	}
	for i := range v.Stack {
		c.introduce(e, v.Stack[i].ID)
		c.introduce(e, v.Stack[i].Source)
	}
	for i := range v.Pending {
		c.introduce(e, v.Pending[i].Source)
	}
	if d := v.Decision; d != nil {
		c.introduce(e, d.Source)
		for i := range d.Options {
			c.introduce(e, d.Options[i].Obj)
			c.introduce(e, d.Options[i].Attacker)
		}
	}
	if a == nil {
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
	// Every slice is exactly sized and nil when empty, as an append from nil
	// would leave it: frames are compared with reflect.DeepEqual.
	frame := Frame{Identities: a.identities.take(len(c.introduced))}
	copy(frame.Identities, c.introduced)
	frame.Events = a.events.take(len(redacted))
	for i := range redacted {
		ev := &redacted[i]
		out := &frame.Events[i]
		*out = ObservedEvent{Kind: ev.Kind, Player: ev.Player, Obj: c.ref(ev.Obj), From: ev.From, To: ev.To, Amount: ev.Amount, Step: ev.Step, Counter: ev.Counter, Text: ev.Text, Secret: ev.Secret}
		if len(ev.IDs) > 0 {
			ids, k := a.refs.take(len(ev.IDs)), 0
			for _, id := range ev.IDs {
				if ref := c.ref(id); ref != 0 {
					ids[k] = ref
					k++
				}
			}
			if k > 0 {
				out.IDs = ids[:k:k]
			}
		}
		if len(ev.Pairs) > 0 {
			pairs, k := a.pairs.take(len(ev.Pairs)), 0
			for _, pair := range ev.Pairs {
				x, y := c.ref(pair[0]), c.ref(pair[1])
				if x != 0 && y != 0 {
					pairs[k] = [2]uint32{x, y}
					k++
				}
			}
			if k > 0 {
				out.Pairs = pairs[:k:k]
			}
		}
	}
	if d := v.Decision; d != nil {
		var err error
		out := &a.decisions.take(1)[0]
		if frame.Decision, err = c.observeDecisionInto(out, a.options.take(len(d.Options)), d); err != nil {
			return view.View{}, Frame{}, err
		}
	}
	v.Decision = nil // never serialize in-memory engine continuation state
	c.remap(&v)
	return v, frame, nil
}

// remap rewrites every object reference of a projected view (decision
// already cleared) into observation refs, in place: the projection owns
// every slice and pointer it holds -- c's reusable view (zones, BlockedBy,
// LibraryTop, stack cards and targets) and the potential actions
// rules.PotentialActions builds fresh per call -- so nothing aliases the
// engine, and the next ProjectInto rewrites every field remap touched.
func (c *Collector) remap(v *view.View) {
	for i := range v.Players {
		p := &v.Players[i]
		c.cards(p.Hand)
		c.cards(p.Battlefield)
		c.cards(p.Graveyard)
		c.cards(p.Exile)
		c.cards(p.Command)
		if p.LibraryTop != nil {
			c.card(p.LibraryTop)
		}
		for j := range p.PlanarDeck {
			// Not rewritten, so not blanked: the token the projection
			// would have formatted (SuppressCardTokens).
			p.PlanarDeck[j].Token = "#" + strconv.FormatUint(uint64(p.PlanarDeck[j].ID), 10)
		}
		// PotentialActions (the viewer's own offer walk, view/view.go) names
		// its objects by engine ObjID like every other board field; left raw,
		// a sampled world whose hidden objects were allocated different IDs
		// can never serialize the same board, and every world is rejected at
		// its first frame. Map them through the same observation refs.
		for j := range p.PotentialActions {
			p.PotentialActions[j].Obj = state.ObjID(c.ref(p.PotentialActions[j].Obj))
		}
	}
	for i := range v.Stack {
		s := &v.Stack[i]
		s.ID = state.ObjID(c.ref(s.ID))
		s.Source = state.ObjID(c.ref(s.Source))
		if s.Card != nil {
			c.card(s.Card)
		}
		// An empty target list becomes nil (JSON null), as the copy this
		// rewrite replaced made it.
		if len(s.Targets) == 0 {
			s.Targets = nil
		}
		for j := range s.Targets {
			s.Targets[j].Obj = state.ObjID(c.ref(s.Targets[j].Obj))
		}
	}
	for i := range v.Pending {
		v.Pending[i].Source = state.ObjID(c.ref(v.Pending[i].Source))
	}
}

func (c *Collector) introduce(e *rules.Engine, id state.ObjID) {
	if id == 0 || int(id) < len(c.known) && c.known[id] != 0 {
		return
	}
	o := e.G.Obj(id)
	if o == nil {
		return
	}
	ref := uint32(len(c.byRef))
	name := ""
	if o.Card != nil && len(o.Card.Faces) > 0 {
		name = o.Card.Faces[0].Name
	}
	if int(id) >= len(c.known) {
		c.known = append(c.known, make([]uint32, int(id)+1-len(c.known))...)
	}
	c.known[id] = ref
	c.byRef = append(c.byRef, id)
	c.gen++
	c.introduced = append(c.introduced, Identity{ID: ref, Name: name, Owner: o.Owner})
}

func (c *Collector) ref(id state.ObjID) uint32 {
	if _, ok := id.PlayerRef(); ok {
		return uint32(id)
	}
	if int(id) < len(c.known) {
		return c.known[id]
	}
	return 0
}

func (c *Collector) object(ref uint32) state.ObjID {
	if ref == 0 || int(ref) >= len(c.byRef) {
		return 0
	}
	return c.byRef[ref]
}

// card rewrites one pointed-to projected card (a library top, a stack
// entry's card) in place. An empty BlockedBy becomes nil, as the copy this
// rewrite replaced made it.
func (c *Collector) card(card *view.CardView) {
	if len(card.BlockedBy) == 0 {
		card.BlockedBy = nil
	}
	c.zoneCard(card)
}

func (c *Collector) zoneCard(card *view.CardView) {
	card.ID = state.ObjID(c.ref(card.ID))
	card.Token = ""
	card.AttachedTo = state.ObjID(c.ref(card.AttachedTo))
	for i := range card.BlockedBy {
		card.BlockedBy[i] = state.ObjID(c.ref(card.BlockedBy[i]))
	}
}

// cards rewrites a projected zone in place (see remap).
func (c *Collector) cards(cards []view.CardView) {
	for i := range cards {
		c.zoneCard(&cards[i])
	}
}
