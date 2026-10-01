package searchprobe

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"sort"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// RedealBase opts Sample into the redeal fallback (pn21 stretch): when the
// rejection sampler starves, worlds are built from Engine -- the engine the
// deciding seat is playing in, at the observed boundary -- by re-dealing every
// hidden card the seat does not know, with the known-card projection pinning
// the rest. Observer is that seat's collector at the same boundary (it maps
// the history's references to Engine's objects). Neither is modified.
//
// What of Engine a redealt world keeps is exactly its PUBLIC state (checked:
// the redealt world must capture the same board and decision as the last
// observed frame), the seat's known cards (the projection), and each hidden
// zone's SIZE. Hidden identities are re-dealt from each owner's pool, the
// hidden cards Engine actually holds; the redeal proceeds only when that pool
// equals the pool the seat can derive itself (the public deck list minus every
// card it has seen outside the hidden zones and every known hidden card), so
// the pool carries nothing the seat could not count. Future chance is
// re-seeded (rules.Engine.CloneHypothetical): a world never inherits the
// game's own random future.
//
// Every refusal is fail-closed and named in SampleResult.RedealRefused. Two of
// them compare against the real engine (the projection must hold there, and
// the derivable pool must match), so a refusal can depend on hidden state;
// both only fire when the seat's own accounting is wrong, never in normal
// play, and they cost a missing world rather than a wrong one.
type RedealBase struct {
	Engine   *rules.Engine
	Observer *Collector
}

// redealPlan is one player's fixed facts: which cards stay put and which are
// dealt.
type redealPlan struct {
	player   state.PlayerID
	hand     []state.ObjID // base hand, base order
	libLen   int
	pins     []state.ObjID // the pinned hand cards, by ObjID
	top, bot []state.ObjID
	loose    []state.ObjID // known library members with no known position
	unknown  []state.ObjID // sorted by name, then ObjID
	handFree int
	// canon is unknown and loose together in (name, ObjID) order, and
	// unknownAt[i] is unknown[i]'s index in it: the library deal's
	// canonical order, fixed per boundary, so a Deal filters it instead of
	// sorting by name (redealPlayer).
	canon     []state.ObjID
	unknownAt []int32
}

func redealWorlds(setup PublicGame, h History, known KnownCards, base *RedealBase, n int, seed func(int) [2]uint64) ([]World, string) {
	if base == nil {
		return nil, "no base engine for this seat"
	}
	r, refused := NewRedealer(setup, h, known, *base)
	if refused != "" {
		return nil, refused
	}
	var worlds []World
	for i := 0; i < n; i++ {
		w, reason := r.Deal(seed(i), nil)
		if reason != "" {
			return nil, reason
		}
		worlds = append(worlds, World{Engine: w, Observer: base.Observer.Clone()})
	}
	return worlds, ""
}

// Redealer is the redeal prepared once at one decision boundary: the checks
// that compare the seat's history, known-card projection and derivable pool
// against the base engine run in NewRedealer, and each Deal is then one
// uniform redeal (RedealBase documents what a redealt world keeps and what
// it re-deals). It is the honest world source's building block (the SpellBench
// M1 "redeal" worlds, internal/azmcts.RedealSource) as well as Sample's
// starvation fallback, so both draw worlds the same way.
//
// A Redealer reads its base engine and observer and never modifies them;
// Deal may be called any number of times, sequentially.
type Redealer struct {
	e     *rules.Engine
	known KnownCards
	obs   *Collector
	// probe is a clone of obs that every Deal probes its world through and
	// rolls back (Collector.probeBoundary), so each deal observes the world
	// exactly as a fresh clone of obs would.
	probe *Collector
	// nowBoard and nowDecision are the base boundary as probe observes it:
	// the encoded board and the observed decision every world must match.
	nowBoard    []byte
	nowDecision *ObservedDecision
	plans       []redealPlan
	scratch     dealScratch
	// blind is observationBlind at the base boundary: no world can change
	// the seat's observation, so Deal skips the probe (redeal_blind.go).
	blind bool
}

// NewRedealer prepares the redeal of base.Engine for the seat whose History
// h is, at h's last frame -- which must be base.Engine's current boundary as
// base.Observer captures it. known is h's known-card projection
// (ProjectKnownCards, or a KnownCardTracker folded through the same frames
// and answers). setup.Decks are the deck lists the seat believes each player
// holds (for the SpellBench mirror benchmark, its own list twice); a hidden
// pool that list cannot account for refuses. A non-empty reason is the
// fail-closed refusal and the Redealer is nil.
func NewRedealer(setup PublicGame, h History, known KnownCards, base RedealBase) (*Redealer, string) {
	if base.Engine == nil || base.Observer == nil || base.Observer.actor != h.Actor || len(h.Frames) == 0 {
		return nil, "no base engine for this seat"
	}
	e := base.Engine
	last := h.Frames[len(h.Frames)-1]
	probe := base.Observer.Clone()
	nowRaw, nowDec, err := probe.probeBoundary(e)
	if err != nil {
		return nil, "base capture: " + err.Error()
	}
	nowBoard := bytes.Clone(nowRaw)
	if nowDec != nil {
		owned := *nowDec
		owned.Options = slices.Clone(nowDec.Options)
		nowDec = &owned
	}
	if sha256.Sum256(nowBoard) != last.Board.Sum || !reflect.DeepEqual(nowDec, last.Decision) {
		return nil, "base engine is not at the observed boundary"
	}
	if err := known.holds(e, base.Observer); err != nil {
		return nil, "projection does not hold in the base engine: " + err.Error()
	}
	names := make(map[uint32]Identity)
	for _, frame := range h.Frames {
		for _, identity := range frame.Identities {
			names[identity.ID] = identity
		}
	}
	board := &last.Board
	// Every object the observed frame still refers to must be public, the
	// actor's own, or pinned: re-dealing one would change what the decision
	// or the stack means.
	pinned := make(map[state.ObjID]bool)
	for _, hand := range known.Hands {
		for _, c := range hand.Cards {
			pinned[base.Observer.object(c.ID)] = true
		}
	}
	for _, lib := range known.Libraries {
		for _, c := range lib.Members {
			pinned[base.Observer.object(c.ID)] = true
		}
	}
	var referenced []uint32
	for _, s := range board.Stack {
		referenced = append(referenced, s.ID, s.Source)
		referenced = append(referenced, s.Targets...)
	}
	if d := last.Decision; d != nil {
		referenced = append(referenced, d.Source)
		for _, o := range d.Options {
			referenced = append(referenced, o.Action.Obj, o.Action.Attacker)
		}
	}
	for _, ref := range referenced {
		id := base.Observer.object(ref)
		if o := e.G.Obj(id); o != nil && o.Zone.Hidden() && !pinned[id] {
			return nil, "observed frame refers to an unpinned hidden card"
		}
	}
	// The seat-derivable pool: deck list minus cards seen outside hidden
	// zones minus known hidden cards.
	public := make(map[state.PlayerID]map[string]int)
	take := func(owner state.PlayerID, name string) {
		if int(owner) >= len(setup.Decks) || !deckHas(setup.Decks[owner], name) {
			// A token (or anything else no deck-list card could be) is not
			// part of the owner's pool.
			return
		}
		if public[owner] == nil {
			public[owner] = make(map[string]int)
		}
		public[owner][name]++
	}
	// A token or a copy is never a deck-list card, even when it bears one's
	// name (an embalmed Sacred Cat's token, a copied spell); whether an
	// object is one is public, so the check reads nothing hidden.
	minted := func(ref uint32) bool {
		o := e.G.Obj(base.Observer.object(ref))
		return o != nil && (o.IsToken || o.IsCopy)
	}
	for _, p := range board.Players {
		for _, zone := range [][]BoardCard{p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for _, c := range zone {
				if identity, ok := names[c.ID]; ok && !minted(c.ID) {
					take(identity.Owner, identity.Name)
				}
			}
		}
	}
	for _, s := range board.Stack {
		if o := e.G.Obj(base.Observer.object(s.ID)); o != nil && o.Card != nil && !o.IsToken && !o.IsCopy {
			if identity, ok := names[s.ID]; ok {
				take(identity.Owner, identity.Name)
			}
		}
	}
	var plans []redealPlan
	for pi := range e.G.Players {
		p := state.PlayerID(pi)
		plan := redealPlan{player: p, hand: append([]state.ObjID(nil), e.G.Zone(state.ZHand, p)...)}
		pinHand := make(map[state.ObjID]bool)
		lib := e.G.Zone(state.ZLibrary, p)
		plan.libLen = len(lib)
		pinLib := make(map[state.ObjID]bool)
		for _, hand := range known.Hands {
			if hand.Player == p {
				for _, c := range hand.Cards {
					pinHand[base.Observer.object(c.ID)] = true
					take(p, c.Name)
				}
			}
		}
		for _, l := range known.Libraries {
			if l.Player != p {
				continue
			}
			positioned := make(map[state.ObjID]bool)
			for _, c := range l.Top {
				plan.top = append(plan.top, base.Observer.object(c.ID))
				positioned[base.Observer.object(c.ID)] = true
			}
			for _, c := range l.Bottom {
				plan.bot = append(plan.bot, base.Observer.object(c.ID))
				positioned[base.Observer.object(c.ID)] = true
			}
			for _, c := range l.Members {
				id := base.Observer.object(c.ID)
				pinLib[id] = true
				take(p, c.Name)
				if !positioned[id] {
					plan.loose = append(plan.loose, id)
				}
			}
		}
		for _, id := range append(append([]state.ObjID(nil), plan.hand...), lib...) {
			if !pinHand[id] && !pinLib[id] {
				plan.unknown = append(plan.unknown, id)
			}
		}
		sortByName(e, plan.unknown)
		plan.handFree = len(plan.hand) - len(pinHand)
		for id := range pinHand {
			plan.pins = append(plan.pins, id)
		}
		slices.Sort(plan.pins)
		plan.index(e)
		if pi >= len(setup.Decks) {
			return nil, "seat outside the public deck lists"
		}
		derivable := make(map[string]int)
		for _, c := range setup.Decks[pi] {
			derivable[c.Faces[0].Name]++
		}
		for name, count := range public[p] {
			derivable[name] -= count
		}
		actual := make(map[string]int)
		for _, id := range plan.unknown {
			if o := e.G.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 {
				actual[o.Card.Faces[0].Name]++
			} else {
				return nil, fmt.Sprintf("player %d hidden object %d has no card", p, id)
			}
		}
		// The first mismatch by name (sorted, so the reason is deterministic)
		// names the card whose accounting is off.
		var off []string
		for name, count := range derivable {
			if count != actual[name] {
				off = append(off, name)
			}
		}
		for name, count := range actual {
			if derivable[name] != count {
				off = append(off, name)
			}
		}
		if len(off) > 0 {
			sort.Strings(off)
			return nil, fmt.Sprintf("player %d hidden pool is not derivable from public information (%s: derivable %d, hidden %d)", p, off[0], derivable[off[0]], actual[off[0]])
		}
		plans = append(plans, plan)
	}
	return &Redealer{e: e, known: known, obs: base.Observer, probe: probe, nowBoard: nowBoard, nowDecision: nowDec, plans: plans,
		blind: observationBlind(e, h.Actor, plans)}, ""
}

// Deal builds one redealt world from seed: a hypothetical clone of the base
// engine (rules.Engine.CloneHypotheticalInto, drawing its arrays from *sp
// when sp is non-nil) whose future chance is seeded by seed[1], with every
// player's unknown hidden cards dealt uniformly by a PCG stream seeded by
// seed, through Secret events on the world's own log. The world is checked
// against the known-card projection and must capture exactly the observed
// board and decision; any failure is a non-empty reason and a nil engine.
// The world's objects keep the base engine's ids, so an observer of the base
// boundary (a clone of it) maps the world's decision the same way.
func (r *Redealer) Deal(seed [2]uint64, sp *rules.Spare) (*rules.Engine, string) {
	rng := rand.New(rand.NewPCG(seed[0], seed[1]))
	w := r.e.CloneHypotheticalInto(seed[1], sp)
	for i := range r.plans {
		if reason := redealPlayer(w, &r.plans[i], rng, &r.scratch); reason != "" {
			return nil, reason
		}
	}
	if err := r.known.holds(w, r.obs); err != nil {
		return nil, "redealt world breaks the projection: " + err.Error()
	}
	if r.blind && !redealProbeVerify {
		// No world can change the observation (observationBlind).
		return w, ""
	}
	board, dec, err := r.probe.probeBoundary(w)
	if err != nil || !bytes.Equal(board, r.nowBoard) || !reflect.DeepEqual(dec, r.nowDecision) {
		if r.blind {
			panic(fmt.Sprintf("searchprobe: a hidden-blind redeal (seed %v) changed the observation (err %v)", seed, err))
		}
		return nil, "redealt world changes the observation"
	}
	return w, ""
}

// index builds the plan's canonical library order (canon, unknownAt) from
// its unknown and loose cards.
func (plan *redealPlan) index(e *rules.Engine) {
	plan.canon = append(append([]state.ObjID(nil), plan.unknown...), plan.loose...)
	sortByName(e, plan.canon)
	at := make(map[state.ObjID]int32, len(plan.canon))
	for i, id := range plan.canon {
		at[id] = int32(i)
	}
	plan.unknownAt = make([]int32, len(plan.unknown))
	for i, id := range plan.unknown {
		plan.unknownAt[i] = at[id]
	}
}

// redealPlayer deals one player's unknown hidden cards uniformly: into the
// hand's free slots, then with the position-less known members across the
// library's free positions. Every change is a Secret event through
// events.Emit on the world's own log. sc is the caller's scratch, reused
// across deals (nothing it holds survives the call).
func redealPlayer(w *rules.Engine, plan *redealPlan, r *rand.Rand, sc *dealScratch) string {
	// Both deals start from a canonical (name, object) order, so the names
	// dealt are a function of the name multiset and the seed alone -- not of
	// which same-named copy the real game happened to leave hidden.
	//
	// The hand deal shuffles unknown's indices: Shuffle's draws depend on
	// the length alone, so deal[k] = unknown[idx[k]] is exactly the shuffled
	// copy of unknown.
	n := len(plan.unknown)
	idx := sc.idx[:0]
	for i := 0; i < n; i++ {
		idx = append(idx, int32(i))
	}
	sc.idx = idx
	r.Shuffle(n, func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	if plan.handFree < 0 || plan.handFree > n {
		return fmt.Sprintf("player %d hand does not fit its pool", plan.player)
	}
	// The library's pool -- every unknown card the hand did not take, plus
	// the position-less known members -- in (name, object) order is canon
	// minus the dealt hand.
	dealt := sc.dealt[:0]
	dealt = append(dealt, make([]bool, len(plan.canon))...)
	sc.dealt = dealt
	for _, k := range idx[:plan.handFree] {
		dealt[plan.unknownAt[k]] = true
	}
	free := sc.free[:0]
	for i, id := range plan.canon {
		if !dealt[i] {
			free = append(free, id)
		}
	}
	sc.free = free
	r.Shuffle(len(free), func(i, j int) { free[i], free[j] = free[j], free[i] })
	// lib is scratch too: Log.Append copies an event's IDs and the
	// LibraryOrder fold copies them again into the zone, so no world keeps
	// it.
	lib := sc.lib[:0]
	lib = append(lib, make([]state.ObjID, plan.libLen)...)
	sc.lib = lib
	for i, id := range plan.top {
		lib[i] = id
	}
	for i, id := range plan.bot {
		at := plan.libLen - len(plan.bot) + i
		if lib[at] != 0 && lib[at] != id {
			return fmt.Sprintf("player %d known top and bottom disagree", plan.player)
		}
		lib[at] = id
	}
	next := 0
	for i := range lib {
		if lib[i] != 0 {
			continue
		}
		if next >= len(free) {
			return fmt.Sprintf("player %d library does not fit its pool", plan.player)
		}
		lib[i] = free[next]
		next++
	}
	if next != len(free) {
		return fmt.Sprintf("player %d library does not fit its pool", plan.player)
	}
	if plan.handFree > 0 {
		// Rebuild the hand in a canonical order -- pinned cards by object,
		// then the dealt cards in deal order -- so which dealt cards were
		// really in the hand (and where) does not survive as hand order.
		for _, id := range plan.hand {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: plan.player, Obj: id, From: state.ZHand, To: state.ZLibrary, Secret: true})
		}
		for _, id := range plan.pins {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: plan.player, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
		}
		for _, k := range idx[:plan.handFree] {
			events.Emit(w.G, w.L, events.Event{Kind: events.MoveZone, Player: plan.player, Obj: plan.unknown[k], From: state.ZLibrary, To: state.ZHand, Secret: true})
		}
	}
	events.Emit(w.G, w.L, events.Event{Kind: events.LibraryOrder, Player: plan.player, IDs: lib, Secret: true})
	if got := w.G.Zone(state.ZHand, plan.player); len(got) != len(plan.hand) || !slices.Equal(w.G.Zone(state.ZLibrary, plan.player), lib) {
		// slices.Equal, not reflect.DeepEqual: an empty library is a nil
		// zone and a zero-length lib, which DeepEqual calls different.
		gl := w.G.Zone(state.ZLibrary, plan.player)
		return fmt.Sprintf("player %d redeal did not land (hand %d want %d, library %d want %d, pinned hand %d)", plan.player, len(got), len(plan.hand), len(gl), len(lib), len(plan.pins))
	}
	return ""
}

// dealScratch is redealPlayer's reusable storage.
type dealScratch struct {
	idx       []int32
	dealt     []bool
	free, lib []state.ObjID
}

func deckHas(deck []*cards.Card, name string) bool {
	for _, c := range deck {
		if c != nil && len(c.Faces) > 0 && c.Faces[0] != nil && c.Faces[0].Name == name {
			return true
		}
	}
	return false
}

// sortByName orders objects by card name, then object id.
func sortByName(e *rules.Engine, ids []state.ObjID) {
	name := func(id state.ObjID) string {
		if o := e.G.Obj(id); o != nil && o.Card != nil && len(o.Card.Faces) > 0 {
			return o.Card.Faces[0].Name
		}
		return ""
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := name(ids[i]), name(ids[j])
		if a != b {
			return a < b
		}
		return ids[i] < ids[j]
	})
}
