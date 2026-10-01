package searchprobe

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// KnownCards is the deciding seat's knowledge of which cards sit in the
// hidden zones (every hand, every library) as of the last observed frame: the
// search-only known-card projection (pn21). It is derived from a History
// alone -- the redacted, observer-local frames and the seat's own semantic
// answers that Sample already consumes -- so it can never carry a fact the
// seat did not observe. Cards are named by their observer references
// (Identity.ID), the same references a sampled World's Observer maps back to
// that world's objects.
//
// Every claim is conservative: when the observation does not pin a card to a
// zone (or a library position) the card is simply absent from the
// projection. Claiming less only weakens a sampling constraint; claiming more
// would leak or contradict.
type KnownCards struct {
	Actor     state.PlayerID
	Hands     []KnownHand
	Libraries []KnownLibrary
}

// KnownHand lists the cards the seat knows are in Player's hand, sorted by
// reference. For the actor's own seat it is the whole hand.
type KnownHand struct {
	Player state.PlayerID
	Cards  []Identity
}

// KnownLibrary is the seat's knowledge of Player's library. Top is a known
// contiguous prefix (Top[0] is the next card drawn); Bottom a known
// contiguous suffix (Bottom[len-1] is the bottom card). Members is every card
// known to be somewhere in the library -- a superset of Top and Bottom --
// sorted by reference. In a small, fully-known library one card may appear
// in both Top and Bottom.
type KnownLibrary struct {
	Player  state.PlayerID
	Top     []Identity
	Bottom  []Identity
	Members []Identity
}

// Count is the number of distinct placement claims: hand cards plus library
// members (a positioned card counts once).
func (k KnownCards) Count() int {
	n := 0
	for _, h := range k.Hands {
		n += len(h.Cards)
	}
	for _, l := range k.Libraries {
		n += len(l.Members)
	}
	return n
}

// ProjectKnownCards folds a whole History through a KnownCardTracker.
func ProjectKnownCards(h History) (KnownCards, error) {
	t := NewKnownCardTracker(h.Actor)
	for i, frame := range h.Frames {
		if err := t.Observe(frame); err != nil {
			return KnownCards{}, err
		}
		if answer, ok := h.Answers[i]; ok {
			t.Answer(answer)
		}
	}
	return t.Known(), nil
}

type knownLoc struct {
	zone   state.Zone // ZHand or ZLibrary
	player state.PlayerID
}

// KnownCardTracker is the incremental form of ProjectKnownCards: Observe each
// frame in order, then Answer the actor's semantic answer to that frame's
// decision (when there is one) before observing the next frame.
//
// What it tracks and how each fact is invalidated:
//
//   - A card whose observed zone move names it (Obj != 0) into a hand or a
//     library is known there; it is forgotten when an observed move or the
//     public board names it anywhere else. A move INTO a library appends it
//     at the bottom (events.Move's semantics; any reorder is its own
//     Shuffle/LibraryOrder event), extending the known bottom suffix.
//   - A move whose card the seat could not see (Obj == 0) out of a hand
//     forgets every non-public hand claim; out of a library by anything but a
//     draw forgets every library claim; into a library forgets every known
//     bottom suffix. The event's Player is not trusted to name the zone's
//     owner (it is the effect controller at some MoveZone sites), so these
//     are applied to every seat.
//   - An unseen draw takes the drawer's top card: a known top card moves to
//     that player's known hand. With no known top, every position-less
//     member of that library is forgotten (it may have been drawn), and the
//     known bottom too unless the library provably held more cards.
//   - A Shuffle forgets the player's known positions; membership survives.
//     A LibraryOrder does too, unless it is the reorder the actor's own
//     answered arrange (scry, surveil, rearrange) just made, which is applied
//     exactly. The pile the actor did not keep on top is only claimed in
//     order when it is a single card: History records the kept pile's order
//     but not the rest pile's (Intent.Rest), so a longer rest pile is
//     membership only.
//   - An arrange decision posed to the actor names the top k cards of its
//     library, top first.
//   - A "revealed ... as a cost" note names cards that stay in their owner's
//     hand.
//   - At the end of every frame the public board wins: a card shown in a
//     public zone or on the stack is in no hidden zone, the actor's own hand
//     is exactly what the board shows, and a player whose zone sizes disagree
//     with the size this tracker expected (an unmodelled change) or who holds
//     more claims than cards loses that zone's claims.
//
// Deliberately not tracked (the card stays UNKNOWN): public reveals whose
// zone the note does not state (RevealHand, a revealed library window), the
// private "looks at the top of the library" note, Dig/hideaway arranges,
// the view's library_top grant, and every sideboard.
type KnownCardTracker struct {
	actor state.PlayerID
	ids   map[uint32]Identity
	loc   map[uint32]knownLoc
	top   map[state.PlayerID][]uint32
	bot   map[state.PlayerID][]uint32
	// libSize/handSize are the tracker's expected zone sizes, resynchronised
	// from the board at each frame end; sizeKnown turns false for every seat
	// when an unattributable move changes a size mid-frame.
	libSize, handSize map[state.PlayerID]int
	sizeKnown         bool
	players           []state.PlayerID
	frame             int
	lastDecision      *ObservedDecision
	pending           *pendingArrange
}

type pendingArrange struct {
	frame        int
	kind         string
	window       []uint32
	pileA, pileB []uint32
}

func NewKnownCardTracker(actor state.PlayerID) *KnownCardTracker {
	return &KnownCardTracker{
		actor: actor,
		ids:   make(map[uint32]Identity),
		loc:   make(map[uint32]knownLoc),
		top:   make(map[state.PlayerID][]uint32),
		bot:   make(map[state.PlayerID][]uint32),

		libSize:  make(map[state.PlayerID]int),
		handSize: make(map[state.PlayerID]int),
	}
}

// Observe folds one frame: its identities, its events in order, its
// decision, then the end-of-frame board.
func (t *KnownCardTracker) Observe(frame Frame) error {
	board := &frame.Board
	if len(board.Players) == 0 {
		return fmt.Errorf("known cards: frame %d board: no players", t.frame)
	}
	if t.players == nil {
		for _, p := range board.Players {
			t.players = append(t.players, p.Seat)
		}
	}
	for _, identity := range frame.Identities {
		t.ids[identity.ID] = identity
	}
	if t.pending != nil && t.pending.frame != t.frame {
		t.pending = nil
	}
	for _, ev := range frame.Events {
		t.event(ev)
	}
	t.pending = nil
	t.lastDecision = frame.Decision
	t.decision(frame.Decision)
	t.reconcile(board)
	t.frame++
	return nil
}

// Answer records the actor's answer to the decision of the frame last
// observed. Only an arrange answer carries placement knowledge; it is applied
// at the matching LibraryOrder in the next frame.
func (t *KnownCardTracker) Answer(actions []Action) {
	d := t.lastDecision
	if d == nil || d.Player != t.actor || d.Kind != decision.KArrange {
		return
	}
	window, kind, ok := arrangeWindow(d)
	if !ok {
		return
	}
	chosen := make([]bool, len(d.Options))
	var pileA []uint32
	for _, a := range actions {
		found := -1
		for i, o := range d.Options {
			if o.Action == a {
				if found >= 0 {
					return
				}
				found = i
			}
		}
		if found < 0 || chosen[found] {
			return
		}
		chosen[found] = true
		pileA = append(pileA, window[found])
	}
	var pileB []uint32
	for i := range d.Options {
		if !chosen[i] {
			pileB = append(pileB, window[i])
		}
	}
	t.pending = &pendingArrange{frame: t.frame, kind: kind, window: window, pileA: pileA, pileB: pileB}
}

// arrangeWindow reads an actor KArrange whose options are, by construction,
// the top of the actor's library top first (Scry, Surveil, Rearrange). Other
// arrange shapes (Dig's and Hideaway's all-to-bottom asks) are not read.
func arrangeWindow(d *ObservedDecision) ([]uint32, string, bool) {
	if len(d.Options) == 0 {
		return nil, "", false
	}
	kind := d.Options[0].Action.Kind
	switch kind {
	case "", "top", "bottom", "graveyard":
	default:
		return nil, "", false
	}
	window := make([]uint32, 0, len(d.Options))
	seen := make(map[uint32]bool, len(d.Options))
	for _, o := range d.Options {
		if o.Action.Kind != kind || o.Action.Obj == 0 || seen[o.Action.Obj] {
			return nil, "", false
		}
		seen[o.Action.Obj] = true
		window = append(window, o.Action.Obj)
	}
	return window, kind, true
}

func (t *KnownCardTracker) validPlayer(p state.PlayerID) bool {
	for _, q := range t.players {
		if q == p {
			return true
		}
	}
	return false
}

func (t *KnownCardTracker) event(ev ObservedEvent) {
	switch ev.Kind {
	case events.Shuffle:
		t.clearPositionsOf(ev.Player)
	case events.LibraryOrder:
		if p := t.pending; p != nil && ev.Player == t.actor {
			t.pending = nil
			t.applyArrange(p)
			return
		}
		t.clearPositionsOf(ev.Player)
	case events.Draw, events.MoveZone, events.PutOnStack:
		if ev.Obj != 0 {
			t.knownMove(ev)
		} else {
			t.unknownMove(ev)
		}
	case events.Note:
		if !ev.Secret && strings.HasPrefix(ev.Text, "revealed ") && strings.HasSuffix(ev.Text, " as a cost") {
			for _, ref := range ev.IDs {
				identity, ok := t.ids[ref]
				if !ok || identity.Owner != ev.Player {
					continue
				}
				t.forget(ref)
				t.loc[ref] = knownLoc{zone: state.ZHand, player: identity.Owner}
			}
		}
	}
}

// clearPositionsOf forgets a player's known library order; an out-of-range
// player (never expected) clears every seat.
func (t *KnownCardTracker) clearPositionsOf(p state.PlayerID) {
	if !t.validPlayer(p) {
		for _, q := range t.players {
			t.clearPositionsOf(q)
		}
		return
	}
	delete(t.top, p)
	delete(t.bot, p)
}

func (t *KnownCardTracker) knownMove(ev ObservedEvent) {
	ref := ev.Obj
	identity, ok := t.ids[ref]
	if !ok {
		// A reference with no observed identity cannot be placed; treat the
		// move as unseen.
		ev.Obj = 0
		t.unknownMove(ev)
		return
	}
	owner := identity.Owner
	if ev.Kind == events.Draw {
		// A draw takes the top card. A known top that is some other card
		// means this tracker's order was wrong: forget it.
		if top := t.top[owner]; len(top) > 0 && top[0] != ref {
			t.clearPositionsOf(owner)
		}
	}
	if ev.From == state.ZLibrary {
		t.libSize[owner]--
	}
	if ev.From == state.ZHand {
		t.handSize[owner]--
	}
	t.forget(ref)
	switch ev.To {
	case state.ZHand:
		t.loc[ref] = knownLoc{zone: state.ZHand, player: owner}
		t.handSize[owner]++
	case state.ZLibrary:
		t.loc[ref] = knownLoc{zone: state.ZLibrary, player: owner}
		t.bot[owner] = append(t.bot[owner], ref)
		t.libSize[owner]++
	}
}

func (t *KnownCardTracker) unknownMove(ev ObservedEvent) {
	if ev.Kind == events.Draw && ev.From == state.ZLibrary && t.validPlayer(ev.Player) {
		t.unknownDraw(ev.Player)
		if ev.To == state.ZHand {
			t.handSize[ev.Player]++
		}
		return
	}
	if ev.From == state.ZHand || ev.To == state.ZHand {
		t.sizeKnown = false
	}
	if ev.From == state.ZLibrary || ev.To == state.ZLibrary {
		t.sizeKnown = false
	}
	if ev.From == state.ZHand {
		for ref, l := range t.loc {
			if l.zone == state.ZHand {
				delete(t.loc, ref)
			}
		}
	}
	if ev.From == state.ZLibrary {
		for ref, l := range t.loc {
			if l.zone == state.ZLibrary {
				delete(t.loc, ref)
			}
		}
		clear(t.top)
		clear(t.bot)
	}
	if ev.To == state.ZLibrary {
		clear(t.bot)
	}
}

func (t *KnownCardTracker) unknownDraw(p state.PlayerID) {
	size, before := t.libSize[p], t.sizeKnown
	t.libSize[p]--
	if top := t.top[p]; len(top) > 0 {
		ref := top[0]
		t.forget(ref)
		t.loc[ref] = knownLoc{zone: state.ZHand, player: p}
		return
	}
	bottom := t.bot[p]
	if before && size > 0 && size == len(bottom) {
		// The library is exactly the known bottom suffix: its top is
		// bottom[0].
		ref := bottom[0]
		t.forget(ref)
		t.loc[ref] = knownLoc{zone: state.ZHand, player: p}
		return
	}
	keepBottom := before && size > len(bottom)
	onBottom := make(map[uint32]bool, len(bottom))
	if keepBottom {
		for _, ref := range bottom {
			onBottom[ref] = true
		}
	} else {
		delete(t.bot, p)
	}
	for ref, l := range t.loc {
		if l.zone == state.ZLibrary && l.player == p && !onBottom[ref] {
			delete(t.loc, ref)
		}
	}
}

// forget removes every hidden-zone claim about ref.
func (t *KnownCardTracker) forget(ref uint32) {
	if l, ok := t.loc[ref]; ok && l.zone == state.ZLibrary {
		t.top[l.player] = without(t.top[l.player], ref)
		t.bot[l.player] = without(t.bot[l.player], ref)
	}
	delete(t.loc, ref)
}

func without(list []uint32, ref uint32) []uint32 {
	for i, r := range list {
		if r == ref {
			out := make([]uint32, 0, len(list)-1)
			out = append(out, list[:i]...)
			return append(out, list[i+1:]...)
		}
	}
	return list
}

// decision reads an actor arrange's window: the options are the top of the
// actor's library, top first.
func (t *KnownCardTracker) decision(d *ObservedDecision) {
	if d == nil || d.Player != t.actor || d.Kind != decision.KArrange {
		return
	}
	window, _, ok := arrangeWindow(d)
	if !ok {
		return
	}
	p := t.actor
	top := t.top[p]
	consistent := true
	for i := 0; i < len(top) && i < len(window); i++ {
		if top[i] != window[i] {
			consistent = false
		}
	}
	var rest []uint32
	if consistent && len(top) > len(window) {
		rest = append(rest, top[len(window):]...)
	}
	if !consistent {
		t.clearPositionsOf(p)
	}
	for _, ref := range window {
		if l, ok := t.loc[ref]; ok && !(l.zone == state.ZLibrary && l.player == p) {
			t.forget(ref)
		}
		t.loc[ref] = knownLoc{zone: state.ZLibrary, player: p}
	}
	// A window card already on the known bottom is only consistent in a
	// library too small to tell apart; do not try to reconcile the two.
	for _, ref := range window {
		for _, b := range t.bot[p] {
			if b == ref {
				delete(t.bot, p)
			}
		}
	}
	t.top[p] = append(append([]uint32(nil), window...), rest...)
}

// applyArrange applies the actor's answered arrange at its LibraryOrder.
func (t *KnownCardTracker) applyArrange(a *pendingArrange) {
	p := t.actor
	top := t.top[p]
	k := len(a.window)
	var rest []uint32
	if len(top) >= k {
		match := true
		for i := 0; i < k; i++ {
			if top[i] != a.window[i] {
				match = false
			}
		}
		if match {
			rest = append(rest, top[k:]...)
		}
	}
	inWindow := make(map[uint32]bool, k)
	for _, ref := range a.window {
		inWindow[ref] = true
	}
	for _, ref := range t.bot[p] {
		if inWindow[ref] {
			delete(t.bot, p)
			break
		}
	}
	newTop := append([]uint32(nil), a.pileA...)
	switch a.kind {
	case "", "top":
		if len(a.pileB) <= 1 {
			newTop = append(newTop, a.pileB...)
			newTop = append(newTop, rest...)
		}
	case "bottom":
		newTop = append(newTop, rest...)
		switch len(a.pileB) {
		case 0:
		case 1:
			t.bot[p] = append(append([]uint32(nil), t.bot[p]...), a.pileB[0])
		default:
			delete(t.bot, p)
		}
	case "graveyard":
		// Pile B leaves the library by its own observed moves next.
		newTop = append(newTop, rest...)
	}
	t.top[p] = newTop
	if len(newTop) == 0 {
		delete(t.top, p)
	}
}

// reconcile applies the end-of-frame board.
func (t *KnownCardTracker) reconcile(board *Board) {
	for _, p := range board.Players {
		for _, zone := range [][]BoardCard{p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for _, c := range zone {
				t.forget(c.ID)
			}
		}
	}
	for _, s := range board.Stack {
		t.forget(s.ID)
	}
	for _, p := range board.Players {
		if p.Seat != t.actor {
			continue
		}
		inHand := make(map[uint32]bool, len(p.Hand))
		for _, c := range p.Hand {
			if c.ID == 0 {
				continue
			}
			inHand[c.ID] = true
			if l, ok := t.loc[c.ID]; !ok || l.zone != state.ZHand || l.player != t.actor {
				t.forget(c.ID)
				t.loc[c.ID] = knownLoc{zone: state.ZHand, player: t.actor}
			}
		}
		for ref, l := range t.loc {
			if l.zone == state.ZHand && l.player == t.actor && !inHand[ref] {
				delete(t.loc, ref)
			}
		}
	}
	counts := make(map[knownLoc]int)
	for _, l := range t.loc {
		counts[l]++
	}
	for _, p := range board.Players {
		lib := knownLoc{zone: state.ZLibrary, player: p.Seat}
		hand := knownLoc{zone: state.ZHand, player: p.Seat}
		if t.frame > 0 && t.sizeKnown && t.libSize[p.Seat] != p.LibrarySize || counts[lib] > p.LibrarySize || len(t.top[p.Seat]) > p.LibrarySize || len(t.bot[p.Seat]) > p.LibrarySize {
			t.clearZone(lib)
		}
		if t.frame > 0 && t.sizeKnown && t.handSize[p.Seat] != p.HandSize || counts[hand] > p.HandSize {
			t.clearZone(hand)
		}
		t.libSize[p.Seat], t.handSize[p.Seat] = p.LibrarySize, p.HandSize
	}
	t.sizeKnown = true
}

func (t *KnownCardTracker) clearZone(z knownLoc) {
	for ref, l := range t.loc {
		if l == z {
			delete(t.loc, ref)
		}
	}
	if z.zone == state.ZLibrary {
		t.clearPositionsOf(z.player)
	}
}

// Known materialises the projection, sorted for determinism.
func (t *KnownCardTracker) Known() KnownCards {
	out := KnownCards{Actor: t.actor}
	refs := make([]uint32, 0, len(t.loc))
	for ref := range t.loc {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i] < refs[j] })
	identities := func(list []uint32) []Identity {
		out := make([]Identity, 0, len(list))
		for _, ref := range list {
			out = append(out, t.ids[ref])
		}
		return out
	}
	for _, p := range t.players {
		var hand, members []uint32
		for _, ref := range refs {
			switch t.loc[ref] {
			case knownLoc{zone: state.ZHand, player: p}:
				hand = append(hand, ref)
			case knownLoc{zone: state.ZLibrary, player: p}:
				members = append(members, ref)
			}
		}
		if len(hand) > 0 {
			out.Hands = append(out.Hands, KnownHand{Player: p, Cards: identities(hand)})
		}
		if len(members) > 0 {
			// Positions are contiguous from their end: a list entry that is
			// no longer a known member of this library ends the prefix (or
			// starts the suffix) there. By construction it never happens; the
			// cut keeps a lapse from becoming a false position claim.
			in := func(ref uint32) bool { return t.loc[ref] == knownLoc{zone: state.ZLibrary, player: p} }
			top := t.top[p]
			for i, ref := range top {
				if !in(ref) {
					top = top[:i]
					break
				}
			}
			bottom := t.bot[p]
			for i := len(bottom) - 1; i >= 0; i-- {
				if !in(bottom[i]) {
					bottom = bottom[i+1:]
					break
				}
			}
			out.Libraries = append(out.Libraries, KnownLibrary{Player: p, Top: identities(top), Bottom: identities(bottom), Members: identities(members)})
		}
	}
	return out
}

// Holds reports the first way world w contradicts the projection, or nil.
// Cards are resolved through the world's own observer references.
func (k KnownCards) Holds(w World) error {
	if w.Engine == nil || w.Observer == nil {
		return fmt.Errorf("known cards: world has no engine or observer")
	}
	return k.holds(w.Engine, w.Observer)
}

func (k KnownCards) holds(e *rules.Engine, c *Collector) error {
	resolve := func(identity Identity) (*state.Object, error) {
		o := e.G.Obj(c.object(identity.ID))
		if o == nil {
			return nil, fmt.Errorf("known card %d (%s) has no object", identity.ID, identity.Name)
		}
		name := ""
		if o.Card != nil && len(o.Card.Faces) > 0 {
			name = o.Card.Faces[0].Name
		}
		if name != identity.Name || o.Owner != identity.Owner {
			return nil, fmt.Errorf("known card %d is %s/%d, want %s/%d", identity.ID, name, o.Owner, identity.Name, identity.Owner)
		}
		return o, nil
	}
	for _, h := range k.Hands {
		for _, identity := range h.Cards {
			o, err := resolve(identity)
			if err != nil {
				return err
			}
			if o.Zone != state.ZHand || o.Owner != h.Player {
				return fmt.Errorf("known hand card %d (%s) of player %d is in zone %v", identity.ID, identity.Name, h.Player, o.Zone)
			}
		}
	}
	for _, l := range k.Libraries {
		lib := e.G.Zone(state.ZLibrary, l.Player)
		for _, identity := range l.Members {
			o, err := resolve(identity)
			if err != nil {
				return err
			}
			if o.Zone != state.ZLibrary || o.Owner != l.Player {
				return fmt.Errorf("known library card %d (%s) of player %d is in zone %v", identity.ID, identity.Name, l.Player, o.Zone)
			}
		}
		if len(l.Top) > len(lib) || len(l.Bottom) > len(lib) {
			return fmt.Errorf("player %d library holds %d cards, fewer than its known positions", l.Player, len(lib))
		}
		for i, identity := range l.Top {
			if lib[i] != c.object(identity.ID) {
				return fmt.Errorf("player %d library position %d is not known card %d (%s)", l.Player, i, identity.ID, identity.Name)
			}
		}
		for i, identity := range l.Bottom {
			at := len(lib) - len(l.Bottom) + i
			if lib[at] != c.object(identity.ID) {
				return fmt.Errorf("player %d library position %d (from the bottom %d) is not known card %d (%s)", l.Player, at, len(l.Bottom)-i, identity.ID, identity.Name)
			}
		}
	}
	return nil
}
