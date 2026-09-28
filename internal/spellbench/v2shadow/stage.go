package v2shadow

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/rules"
	"github.com/adams-shaun/gorge/state"
)

// Shadow is one staged gorge engine for one v2 decision.
type Shadow struct {
	E  *rules.Engine
	Me state.PlayerID
	// V2ToObj maps every staged v2 object id (records and stack entries)
	// to its gorge object; ObjToV2 is the reverse.
	V2ToObj map[string]state.ObjID
	ObjToV2 map[state.ObjID]string
	// Lossy lists what the staging could not reproduce (one entry per gap,
	// a stable reason string); Fatal is set when the engine is not
	// positioned at a decision for Me (the shadow is then unusable).
	Lossy []string
	Fatal string
	// OppHand, OppLib and MyLib are the gorge objects dealt into hidden
	// zones (the redeal pools).
	OppHand, OppLib, MyLib []state.ObjID
	// StagedStep is the step the observation was staged at.
	StagedStep state.Step
	// StackGap is set when a stack item could not be staged: the shadow's
	// stack is shorter than the real one, so a sorcery-speed play the
	// shadow offers is not legal on the wire.
	StackGap bool
}

// Tracker keeps the deck object each v2 object id claimed, across the
// rebuilds of one game, so object identities are stable (a lowering or a
// combat plan made at one decision names the same gorge objects at the
// next). One per game; not safe for concurrent use.
type Tracker struct {
	ids map[string]state.ObjID // lookup only
}

// NewTracker starts a game's id memory.
func NewTracker() *Tracker { return &Tracker{ids: map[string]state.ObjID{}} }

// Options tune one Build.
type Options struct {
	// Seed picks the deal of hidden cards and the engine's future chance.
	Seed uint64
	// NoPumps skips staging unexplained P/T and keyword differences as
	// until-end-of-turn effects.
	NoPumps bool
	// NoAdvance leaves the engine unadvanced (a choice decision is lifted
	// onto the staged state instead of being re-posed).
	NoAdvance bool
	// Priority marks a priority decision: in declare attackers (blockers)
	// the declaration has then been made, and the priority seat is staged.
	Priority bool
}

// phaseSteps maps spec 6.2's phase_step onto gorge steps. Priority in the
// combat damage step comes after the damage: gorge's step would deal it
// again, so it is staged as end of combat (lossy for a first-strike step).
var phaseSteps = map[string]state.Step{
	"untap": state.StepUntap, "upkeep": state.StepUpkeep, "draw": state.StepDraw,
	"precombat_main": state.StepMain1, "beginning_of_combat": state.StepBeginCombat,
	"declare_attackers": state.StepDeclareAttackers, "declare_blockers": state.StepDeclareBlockers,
	"combat_damage": state.StepEndCombat, "first_strike_damage": state.StepEndCombat,
	"end_of_combat": state.StepEndCombat, "postcombat_main": state.StepMain2, "end_step": state.StepEnd,
	"end": state.StepEnd, "cleanup": state.StepCleanup,
}

func seatID(seat string) state.PlayerID {
	if seat == "p1" {
		return 1
	}
	return 0
}

// SeatName maps a gorge player to its v2 seat.
func SeatName(p state.PlayerID) string {
	if p == 1 {
		return "p1"
	}
	return "p0"
}

type builder struct {
	s    *Setup
	tr   *Tracker
	sh   *Shadow
	e    *rules.Engine
	g    *state.Game
	obs  *v2agent.Observation
	rng  *rand.Rand
	free [2]map[string][]state.ObjID // folded name -> unassigned deck objects
	// deckObj[p] lists seat p's deck objects in deck order.
	deckObj [2][]state.ObjID
	claimed map[state.ObjID]bool // lookup only
	visible map[string]bool      // lookup only
}

func (b *builder) ev(e events.Event) events.Event { return events.Emit(b.g, b.e.L, e) }

func (b *builder) lossy(format string, args ...any) {
	b.sh.Lossy = append(b.sh.Lossy, fmt.Sprintf(format, args...))
}

// recName is the name a record is matched on: its full name when sent,
// else its card name ("" when hidden).
func recName(r *v2agent.ObjectRecord) string {
	if r.FullName != nil && *r.FullName != "" {
		return *r.FullName
	}
	if r.CardName != nil {
		return *r.CardName
	}
	return ""
}

// Build stages obs (the acting seat's observation) into a fresh engine.
func (s *Setup) Build(obs *v2agent.Observation, tr *Tracker, o Options) (sh *Shadow) {
	me := seatID(obs.Viewer)
	sh = &Shadow{Me: me, V2ToObj: map[string]state.ObjID{}, ObjToV2: map[state.ObjID]string{}}
	if tr == nil {
		tr = NewTracker()
	}
	defer func() {
		if r := recover(); r != nil {
			sh.Fatal = fmt.Sprintf("staging panicked: %v", r)
		}
	}()
	cfg := rules.Config{
		Seed:         o.Seed,
		Names:        []string{"p0", "p1"},
		Decks:        [][]*cards.Card{s.Decks[0], s.Decks[1]},
		Tokens:       s.Reg.Tokens,
		NameUniverse: s.Reg.Cards,
	}
	e := rules.New(cfg)
	b := &builder{s: s, tr: tr, sh: sh, e: e, g: e.G, obs: obs, claimed: map[state.ObjID]bool{},
		rng: rand.New(rand.NewPCG(o.Seed, o.Seed^0x76327368))}
	sh.E = e
	if e.G.Over {
		sh.Fatal = "genesis ended the game"
		return sh
	}
	b.indexDeck()
	b.stageZones()
	if sh.Fatal != "" {
		return sh
	}
	b.stageTurn(o.Priority)
	if sh.Fatal != "" {
		return sh
	}
	b.stageStack()
	if sh.Fatal != "" {
		return sh
	}
	b.stageCombat(o.Priority)
	if !o.NoPumps {
		b.stagePumps()
	}
	if o.NoAdvance {
		return sh
	}
	b.advance()
	return sh
}

// indexDeck maps deck objects (genesis creates them in deck order, seat by
// seat) and returns every card to its library.
func (b *builder) indexDeck() {
	for p := 0; p < 2; p++ {
		b.free[p] = map[string][]state.ObjID{}
	}
	for i := range b.g.Objs {
		o := &b.g.Objs[i]
		if o.IsToken || o.Card == nil || int(o.Owner) > 1 {
			continue
		}
		b.deckObj[o.Owner] = append(b.deckObj[o.Owner], o.ID)
	}
	for p := 0; p < 2; p++ {
		for _, id := range b.deckObj[p] {
			o := b.g.Obj(id)
			seen := map[string]bool{} // lookup only
			var faces []string
			add := func(n string) {
				if k := fold(n); k != "" && !seen[k] {
					seen[k] = true
					b.free[p][k] = append(b.free[p][k], id)
				}
			}
			for _, f := range o.Card.Faces {
				if f == nil {
					continue
				}
				add(f.Name)
				faces = append(faces, f.Name)
			}
			if len(faces) >= 2 {
				add(strings.Join(faces[:2], " // "))
			}
		}
		for _, id := range append([]state.ObjID(nil), b.g.Zone(state.ZHand, state.PlayerID(p))...) {
			b.ev(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZLibrary})
		}
	}
}

// hasName reports whether deck object id carries name.
func (b *builder) hasName(id state.ObjID, name string) bool {
	o := b.g.Obj(id)
	if o == nil || o.Card == nil {
		return false
	}
	k := fold(name)
	var faces []string
	for _, f := range o.Card.Faces {
		if f == nil {
			continue
		}
		if fold(f.Name) == k {
			return true
		}
		faces = append(faces, f.Name)
	}
	return len(faces) >= 2 && fold(strings.Join(faces[:2], " // ")) == k
}

// bind records the v2 id <-> gorge object pair for this build.
func (b *builder) bind(v2id string, id state.ObjID) {
	b.sh.V2ToObj[v2id] = id
	b.sh.ObjToV2[id] = v2id
}

// claim assigns owner's deck object named name to v2 object v2id: the one
// it claimed at an earlier decision when still free, else the first free
// one of that name.
func (b *builder) claim(owner state.PlayerID, name, v2id string) (state.ObjID, bool) {
	if id, ok := b.sh.V2ToObj[v2id]; ok {
		return id, true
	}
	if name == "" || owner > 1 {
		return 0, false
	}
	if id, ok := b.tr.ids[v2id]; ok && !b.claimed[id] {
		if o := b.g.Obj(id); o != nil && o.Owner == owner && b.hasName(id, name) {
			b.claimed[id] = true
			b.bind(v2id, id)
			return id, true
		}
	}
	for _, id := range b.free[owner][fold(name)] {
		if b.claimed[id] || b.reservedElsewhere(id, v2id) {
			continue
		}
		b.claimed[id] = true
		b.tr.ids[v2id] = id
		b.bind(v2id, id)
		return id, true
	}
	// Every object of that name is reserved by an id not in this
	// observation's first pass: take any unclaimed one.
	for _, id := range b.free[owner][fold(name)] {
		if b.claimed[id] {
			continue
		}
		b.claimed[id] = true
		b.tr.ids[v2id] = id
		b.bind(v2id, id)
		return id, true
	}
	return 0, false
}

// reservedElsewhere reports whether id is remembered for another v2 id that
// this observation still shows (so a new id must not steal it).
func (b *builder) reservedElsewhere(id state.ObjID, v2id string) bool {
	for other := range b.visible {
		if other != v2id && b.tr.ids[other] == id {
			return true
		}
	}
	return false
}

func (b *builder) move(id state.ObjID, to state.Zone) {
	o := b.g.Obj(id)
	if o == nil || o.Zone == to {
		return
	}
	b.ev(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: to})
}

type permEntry struct {
	r     *v2agent.ObjectRecord
	ctrl  state.PlayerID
	id    state.ObjID
	fresh bool // summoning sick: staged after the turn boundary
}

// visible is the set of v2 ids this observation shows (records and stack
// spells), filled before any claim.
func (b *builder) collectVisible() {
	b.visible = map[string]bool{}
	for pi := range b.obs.Players {
		p := &b.obs.Players[pi]
		for _, z := range [][]v2agent.ObjectRecord{p.Hand, p.Battlefield, p.Graveyard, p.Exile, p.Command} {
			for i := range z {
				b.visible[z[i].ObjectID] = true
			}
		}
	}
	for i := range b.obs.Stack {
		b.visible[b.obs.Stack[i].ObjectID] = true
	}
}

func (b *builder) stageZones() {
	b.collectVisible()
	obs := b.obs
	var perms []permEntry
	// First the objects that keep an earlier claim, so a new id never
	// takes a remembered object from an old one.
	type rec struct {
		r    *v2agent.ObjectRecord
		zone state.Zone
		ctrl state.PlayerID
	}
	var recs []rec
	for pi := range obs.Players {
		p := &obs.Players[pi]
		ctrl := seatID(p.Seat)
		for i := range p.Battlefield {
			recs = append(recs, rec{&p.Battlefield[i], state.ZBattlefield, ctrl})
		}
		for i := range p.Graveyard {
			recs = append(recs, rec{&p.Graveyard[i], state.ZGraveyard, ctrl})
		}
		for i := range p.Exile {
			recs = append(recs, rec{&p.Exile[i], state.ZExile, ctrl})
		}
		for i := range p.Hand {
			recs = append(recs, rec{&p.Hand[i], state.ZHand, ctrl})
		}
	}
	for pass := 0; pass < 2; pass++ {
		for _, rc := range recs {
			r := rc.r
			if r.Token {
				continue
			}
			if _, done := b.sh.V2ToObj[r.ObjectID]; done {
				continue
			}
			id, remembered := b.tr.ids[r.ObjectID]
			if pass == 0 && (!remembered || b.claimed[id]) {
				continue
			}
			_, _ = b.claim(seatID(r.OwnerSeat), recName(r), r.ObjectID)
		}
		for i := range obs.Stack {
			it := &obs.Stack[i]
			if it.StackKind != "spell" || it.Copy || it.CardName == nil {
				continue
			}
			if _, done := b.sh.V2ToObj[it.ObjectID]; done {
				continue
			}
			id, remembered := b.tr.ids[it.ObjectID]
			if pass == 0 && (!remembered || b.claimed[id]) {
				continue
			}
			_, _ = b.claim(seatID(it.OwnerSeat), *it.CardName, it.ObjectID)
		}
	}
	for _, rc := range recs {
		r := rc.r
		switch {
		case rc.zone == state.ZBattlefield:
			pe := permEntry{r: r, ctrl: rc.ctrl}
			if !r.Token {
				id, ok := b.sh.V2ToObj[r.ObjectID]
				if !ok {
					b.lossy("unmatched battlefield card %q", recName(r))
					continue
				}
				pe.id = id
			}
			pe.fresh = r.Permanent != nil && r.Permanent.SummoningSick
			perms = append(perms, pe)
		case r.Token:
			// A token off the battlefield ceases to exist (CR 704.5d).
		default:
			id, ok := b.sh.V2ToObj[r.ObjectID]
			if !ok {
				b.lossy("unmatched %s card %q", zoneWord(rc.zone), recName(r))
				continue
			}
			b.move(id, rc.zone)
		}
	}
	for i := range obs.Stack {
		it := &obs.Stack[i]
		if it.StackKind == "spell" && !it.Copy {
			if _, ok := b.sh.V2ToObj[it.ObjectID]; !ok {
				b.lossy("unmatched stack spell")
			}
		}
	}
	b.enterPerms(perms, false)
	b.hiddenDeal()
	active := state.PlayerID(0)
	if obs.ActiveSeat != nil {
		active = seatID(*obs.ActiveSeat)
	}
	turn := int32(obs.Turn)
	if turn < 1 {
		turn = 1
	}
	if turn > 1 {
		b.ev(events.Event{Kind: events.TurnChange, Player: 1 - active, Amount: turn - 1})
	}
	b.ev(events.Event{Kind: events.TurnChange, Player: active, Amount: turn})
	b.enterPerms(perms, true)
	b.permState(perms)
}

func zoneWord(z state.Zone) string {
	switch z {
	case state.ZGraveyard:
		return "graveyard"
	case state.ZExile:
		return "exile"
	case state.ZHand:
		return "hand"
	}
	return "zone"
}

func (b *builder) enterPerms(perms []permEntry, fresh bool) {
	for i := range perms {
		pe := &perms[i]
		if pe.fresh != fresh {
			continue
		}
		r := pe.r
		if r.Token {
			name := recName(r)
			before := len(b.g.Objs)
			if stem, ok := b.s.TokenStem(name); ok && !r.Copy {
				b.ev(events.Event{Kind: events.TokenCreate, Player: pe.ctrl, Text: stem})
			} else if src, ok := b.cardNamed(name); ok {
				// A token copy of a card (embalm, encore, a copy
				// effect): mint it from any deck object of that name.
				b.ev(events.Event{Kind: events.CardToken, Player: pe.ctrl, Obj: src})
				b.lossy("card-copy token")
			} else {
				b.lossy("unknown token %q", name)
				continue
			}
			if len(b.g.Objs) == before {
				b.lossy("token %q not created", name)
				continue
			}
			pe.id = b.g.Objs[len(b.g.Objs)-1].ID
			b.bind(r.ObjectID, pe.id)
			if o := b.g.Obj(pe.id); o != nil && o.Zone != state.ZBattlefield {
				b.move(pe.id, state.ZBattlefield)
			}
		} else {
			if pe.id == 0 {
				continue
			}
			b.move(pe.id, state.ZBattlefield)
			if o := b.g.Obj(pe.id); o != nil && r.CardName != nil && o.Card != nil {
				for fi, f := range o.Card.Faces {
					if fi > 0 && f != nil && fold(f.Name) == fold(*r.CardName) && int(o.FaceIdx) != fi {
						b.ev(events.Event{Kind: events.FlipFace, Obj: pe.id, Amount: int32(fi)})
						break
					}
				}
			}
		}
		if o := b.g.Obj(pe.id); o != nil && o.Controller != pe.ctrl {
			b.ev(events.Event{Kind: events.ControlChange, Obj: pe.id, Player: pe.ctrl})
		}
	}
}

// permState stages tapped state, damage, counters and attachments.
func (b *builder) permState(perms []permEntry) {
	for i := range perms {
		pe := &perms[i]
		if pe.id == 0 || pe.r.Permanent == nil {
			continue
		}
		pm := pe.r.Permanent
		o := b.g.Obj(pe.id)
		if o == nil {
			continue
		}
		if pm.Tapped != o.Tapped {
			if pm.Tapped {
				b.ev(events.Event{Kind: events.Tap, Obj: pe.id})
			} else {
				b.ev(events.Event{Kind: events.Untap, Obj: pe.id})
			}
		}
		if pm.Damage > 0 && o.Damage == 0 {
			b.ev(events.Event{Kind: events.Damage, Obj: pe.id, Amount: int32(pm.Damage)})
		}
		for _, k := range sortedCounterKinds(pm.Counters) {
			kind := strings.ToUpper(k)
			if have := o.Counter(kind); int32(pm.Counters[k]) != have {
				b.ev(events.Event{Kind: events.CounterChange, Obj: pe.id, Counter: kind, Amount: int32(pm.Counters[k]) - have})
			}
		}
		if pm.PhasedOut {
			b.lossy("phased out permanent")
		}
	}
	for i := range perms {
		pe := &perms[i]
		if pe.id == 0 || pe.r.Permanent == nil || pe.r.Permanent.AttachedTo == nil {
			continue
		}
		at := pe.r.Permanent.AttachedTo
		switch {
		case at.Object != nil:
			tid, ok := b.sh.V2ToObj[at.Object.ObjectID]
			if !ok {
				b.lossy("unmatched attachment")
				continue
			}
			b.ev(events.Event{Kind: events.Attach, Obj: pe.id, IDs: []state.ObjID{tid}})
		case at.Player != nil:
			b.lossy("attached to a player")
		}
	}
}

func sortedCounterKinds(m map[string]uint32) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// hiddenDeal fills the hidden zones: our library from our unseen cards
// (random order, known positions pinned), the opponent's hand and library
// from its unseen cards (revealed hand cards pinned).
func (b *builder) hiddenDeal() {
	obs := b.obs
	for pl := 0; pl < 2; pl++ {
		owner := state.PlayerID(pl)
		pv := obs.Player(SeatName(owner))
		if pv == nil {
			b.sh.Fatal = "observation lacks a player"
			return
		}
		var unseen []state.ObjID
		for _, id := range b.deckObj[pl] {
			if !b.claimed[id] {
				unseen = append(unseen, id)
			}
		}
		b.rng.Shuffle(len(unseen), func(i, j int) { unseen[i], unseen[j] = unseen[j], unseen[i] })
		// Pins from known (spec 6.7): cards the decision shows in a hidden
		// zone of this owner.
		type pin struct {
			id  state.ObjID
			pos int // library position from the top, -1 for the hand
		}
		var pins []pin
		take := func(name string) (state.ObjID, bool) {
			for i, id := range unseen {
				if b.hasName(id, name) {
					unseen = append(unseen[:i:i], unseen[i+1:]...)
					return id, true
				}
			}
			return 0, false
		}
		for _, k := range obs.Known {
			if seatID(k.OwnerSeat) != owner {
				continue
			}
			switch {
			case k.Zone == "library" && k.PositionFromTop != nil:
				if id, ok := take(k.CardName); ok {
					pins = append(pins, pin{id, int(*k.PositionFromTop)})
					if k.ObjectID != nil {
						b.bind(*k.ObjectID, id)
					}
				}
			case k.Zone == "hand" && owner != b.sh.Me:
				if id, ok := take(k.CardName); ok {
					pins = append(pins, pin{id, -1})
					if k.ObjectID != nil {
						b.bind(*k.ObjectID, id)
					}
				}
			}
		}
		handN := 0
		if owner != b.sh.Me {
			handN = int(pv.HandCount)
		}
		var hand []state.ObjID
		for _, p := range pins {
			if p.pos < 0 {
				hand = append(hand, p.id)
			}
		}
		for len(hand) < handN && len(unseen) > 0 {
			hand = append(hand, unseen[0])
			unseen = unseen[1:]
		}
		if len(hand) < handN {
			b.lossy("hand %d exceeds unseen pool (seat %d)", handN, pl)
		}
		lib := unseen
		want := int(pv.LibraryCount)
		// Place pinned library cards at their positions, top first (an
		// insert then never shifts an earlier pin).
		sort.SliceStable(pins, func(i, j int) bool { return pins[i].pos < pins[j].pos })
		for _, p := range pins {
			if p.pos < 0 {
				continue
			}
			pos := p.pos
			if pos > len(lib) {
				pos = len(lib)
			}
			lib = append(lib[:pos:pos], append([]state.ObjID{p.id}, lib[pos:]...)...)
		}
		for _, id := range hand {
			b.move(id, state.ZHand)
		}
		for _, id := range lib {
			b.move(id, state.ZLibrary)
		}
		if len(lib) != want {
			b.lossy("library size %d vs observed %d (seat %d)", len(lib), want, pl)
		}
		// A library longer than observed loses its excess (bottom) to
		// exile, so draws line up.
		for len(lib) > want && want >= 0 {
			id := lib[len(lib)-1]
			lib = lib[:len(lib)-1]
			b.move(id, state.ZExile)
		}
		order := append([]state.ObjID(nil), lib...)
		b.ev(events.Event{Kind: events.Shuffle, Player: owner, IDs: order, Secret: true})
		if owner == b.sh.Me {
			b.sh.MyLib = order
		} else {
			b.sh.OppHand = append([]state.ObjID(nil), hand...)
			b.sh.OppLib = order
		}
	}
}

func (b *builder) stageTurn(priority bool) {
	obs := b.obs
	for pl := 0; pl < 2; pl++ {
		pv := obs.Player(SeatName(state.PlayerID(pl)))
		if pv == nil {
			continue
		}
		if d := pv.Life - b.g.Players[pl].Life; d != 0 {
			b.ev(events.Event{Kind: events.LifeChange, Player: state.PlayerID(pl), Amount: d})
		}
		mp := pv.ManaPool
		for i, n := range []uint32{mp.W, mp.U, mp.B, mp.R, mp.G, mp.C} {
			if n > 0 {
				b.ev(events.Event{Kind: events.ManaAdd, Player: state.PlayerID(pl), Counter: "WUBRGC"[i : i+1], Amount: int32(n)})
			}
		}
		for k := uint32(0); k < pv.LandsPlayedThisTurn; k++ {
			b.ev(events.Event{Kind: events.LandPlayed, Player: state.PlayerID(pl)})
		}
	}
	st, ok := phaseSteps[obs.PhaseStep]
	if !ok {
		b.sh.Fatal = "unknown phase_step " + obs.PhaseStep
		return
	}
	if obs.PhaseStep == "combat_damage" || obs.PhaseStep == "first_strike_damage" {
		b.lossy("combat damage step staged as end of combat")
	}
	b.sh.StagedStep = st
	if st >= state.StepBeginCombat && st <= state.StepEndCombat && st != state.StepBeginCombat {
		// Enter combat through its beginning (the per-turn combat count).
		b.ev(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	}
	b.ev(events.Event{Kind: events.StepChange, Step: st})
}

// stageStack places the stack bottom to top (spec 6.5: index 0 is the
// bottom).
func (b *builder) stageStack() {
	for i := range b.obs.Stack {
		it := &b.obs.Stack[i]
		ctrl := seatID(it.ControllerSeat)
		switch {
		case it.StackKind == "spell" && !it.Copy:
			id, ok := b.sh.V2ToObj[it.ObjectID]
			if !ok {
				b.sh.Fatal = "stack spell not staged"
				return
			}
			o := b.g.Obj(id)
			b.ev(events.Event{Kind: events.PutOnStack, Obj: id, From: o.Zone, To: state.ZStack, Player: ctrl})
			if o := b.g.Obj(id); o != nil && o.Controller != ctrl {
				b.ev(events.Event{Kind: events.ControlChange, Obj: id, Player: ctrl})
			}
			if it.XValue != nil && *it.XValue > 0 {
				b.ev(events.Event{Kind: events.CastInfo, Obj: id, Amount: int32(*it.XValue)})
			}
			b.stageTargets(id, it)
		case it.StackKind == "spell":
			b.sh.Fatal = "copied spell on the stack"
			return
		case it.StackKind == "triggered_ability" || it.StackKind == "activated_ability":
			if why := b.stageAbility(it, ctrl); why != "" {
				// An ability the shadow cannot identify (a dungeon room,
				// a keyword's rules-level trigger such as madness) is left
				// off the stack: the shadow decides without it.
				b.lossy("stack ability skipped: %s", why)
				b.sh.StackGap = true
			}
		default:
			b.sh.Fatal = "unstageable stack item " + it.StackKind
			return
		}
	}
}

// stageAbility puts one triggered or activated ability on the stack; a
// non-empty reason means it could not be identified.
func (b *builder) stageAbility(it *v2agent.StackEntry, ctrl state.PlayerID) string {
	src, ok := b.abilitySource(it)
	if !ok {
		return "source not staged"
	}
	o := b.g.Obj(src)
	f := o.Face()
	if f == nil {
		return "source has no face"
	}
	before := len(b.g.Objs)
	if it.StackKind == "triggered_ability" {
		if n, names := cards.SagaChapters(f); n > 0 && len(names) > 0 {
			lore := int(o.Counter("LORE"))
			if lore < 1 {
				lore = 1
			}
			if lore > len(names) {
				lore = len(names)
			}
			b.ev(events.Event{Kind: events.DelayedPush, Player: ctrl, Obj: src, Amount: -1, Counter: names[lore-1]})
		} else {
			idx, sure := pickTrigger(f, o)
			if idx < 0 {
				return "no printed trigger"
			}
			if !sure {
				b.lossy("ambiguous trigger")
			}
			b.ev(events.Event{Kind: events.TriggerPush, Player: ctrl, Obj: src, Amount: int32(idx)})
		}
	} else {
		idx := -1
		n := 0
		for j, sa := range f.Abilities {
			if sa != nil && sa.Kind == "AB" && sa.API != "Mana" {
				if idx < 0 {
					idx = j
				}
				n++
			}
		}
		if n == 0 {
			return "no printed activated ability"
		}
		if n > 1 {
			b.lossy("ambiguous activated ability")
		}
		b.ev(events.Event{Kind: events.AbilityPush, Player: ctrl, Obj: src, Amount: int32(idx)})
	}
	if len(b.g.Objs) == before {
		return "not created"
	}
	aid := b.g.Objs[len(b.g.Objs)-1].ID
	b.bind(it.ObjectID, aid)
	b.stageTargets(aid, it)
	return ""
}

// abilitySource finds a stack ability's source: its source reference, else
// a staged object of the ability's name (a dies trigger's card in a
// graveyard), preferring the controller's.
func (b *builder) abilitySource(it *v2agent.StackEntry) (state.ObjID, bool) {
	if it.Source != nil {
		if id, ok := b.sh.V2ToObj[it.Source.ObjectID]; ok {
			return id, true
		}
	}
	if it.CardName == nil {
		return 0, false
	}
	ctrl := seatID(it.ControllerSeat)
	var fallback state.ObjID
	for _, id := range sortedObjIDs(b.sh.ObjToV2) {
		o := b.g.Obj(id)
		if o == nil || o.Zone == state.ZLibrary {
			continue
		}
		if f := o.Face(); (f == nil || fold(f.Name) != fold(*it.CardName)) && !b.hasName(id, *it.CardName) {
			continue
		}
		if o.Controller == ctrl {
			return id, true
		}
		if fallback == 0 {
			fallback = id
		}
	}
	if fallback != 0 {
		return fallback, true
	}
	// A source in a hidden zone: any deck object of the name.
	if id, ok := b.cardNamed(*it.CardName); ok {
		b.lossy("ability source from a hidden zone")
		return id, true
	}
	return 0, false
}

func sortedObjIDs(m map[state.ObjID]string) []state.ObjID {
	out := make([]state.ObjID, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (b *builder) stageTargets(id state.ObjID, it *v2agent.StackEntry) {
	for _, t := range it.Targets {
		switch {
		case t == nil:
			b.lossy("stack target left")
		case t.Player != nil:
			b.ev(events.Event{Kind: events.TargetsChosen, Obj: id, Player: seatID(*t.Player), Amount: 3})
		case t.Object != nil:
			tid, ok := b.sh.V2ToObj[t.Object.ObjectID]
			if !ok {
				b.lossy("stack target not staged")
				continue
			}
			b.ev(events.Event{Kind: events.TargetsChosen, Obj: id, IDs: []state.ObjID{tid}, Amount: 2})
		}
	}
}

// stageCombat replays the declarations the observation shows: attackers
// (and blockers) are declared once the step is past the declaration, or at
// a priority decision inside that step.
func (b *builder) stageCombat(priority bool) {
	st := b.g.Step
	if st < state.StepDeclareAttackers || st > state.StepCombatDamage {
		return
	}
	declaredAttack := st > state.StepDeclareAttackers || priority
	if !declaredAttack {
		return
	}
	active := b.g.Active
	var attackers []state.ObjID
	type blk struct{ a, bl state.ObjID }
	var blocks []blk
	for pi := range b.obs.Players {
		p := &b.obs.Players[pi]
		for i := range p.Battlefield {
			r := &p.Battlefield[i]
			if r.Permanent == nil {
				continue
			}
			id, ok := b.sh.V2ToObj[r.ObjectID]
			if !ok {
				continue
			}
			if r.Permanent.Attacking {
				attackers = append(attackers, id)
				if at := r.Permanent.AttackTarget; at != nil && at.Object != nil {
					b.lossy("attack on a permanent")
				}
			}
			if r.Permanent.Blocking {
				for _, a := range r.Permanent.BlockedAttackers {
					if aid, ok := b.sh.V2ToObj[a.ObjectID]; ok {
						blocks = append(blocks, blk{aid, id})
					}
				}
			}
		}
	}
	if st != state.StepDeclareAttackers {
		b.ev(events.Event{Kind: events.StepChange, Step: state.StepDeclareAttackers})
	}
	b.ev(events.Event{Kind: events.DeclareAttackers, Player: 1 - active, IDs: attackers})
	if st == state.StepDeclareAttackers {
		return
	}
	b.ev(events.Event{Kind: events.StepChange, Step: state.StepDeclareBlockers})
	if st > state.StepDeclareBlockers || priority {
		var pairs [][2]state.ObjID
		for _, x := range blocks {
			pairs = append(pairs, [2]state.ObjID{x.a, x.bl})
		}
		b.ev(events.Event{Kind: events.DeclareBlockers, Player: 1 - active, Pairs: pairs})
	}
	if st != state.StepDeclareBlockers {
		b.ev(events.Event{Kind: events.StepChange, Step: st})
	}
}

// stagePumps registers, for every battlefield creature whose derived P/T
// or keywords differ from the observed characteristics, an
// until-end-of-turn continuous effect closing the difference (spec D§4.2
// step 3: unexplained deltas are pump effects).
func (b *builder) stagePumps() {
	for pi := range b.obs.Players {
		p := &b.obs.Players[pi]
		for i := range p.Battlefield {
			r := &p.Battlefield[i]
			c := r.Characteristics
			id, ok := b.sh.V2ToObj[r.ObjectID]
			if !ok || c == nil || c.Power == nil || c.Toughness == nil || !b.e.IsCreature(id) {
				continue
			}
			ctrl := seatID(p.Seat)
			dp := *c.Power - b.e.Power(id)
			dt := *c.Toughness - b.e.Toughness(id)
			if dp != 0 || dt != 0 {
				b.e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Controller: ctrl,
					Layer: state.LPT, Sub: state.SubModify, AddPower: dp, AddToughness: dt, UntilEOT: true})
				b.lossy("pump staged")
			}
			var kws []string
			for _, kw := range c.Keywords {
				g, ok := gorgeKeyword[kw]
				if ok && !b.e.HasKeyword(id, g) {
					kws = append(kws, g)
				}
			}
			if len(kws) > 0 {
				b.e.AddContinuous(state.ContinuousEffect{Source: id, Affects: "Card.Self", Controller: ctrl,
					Layer: state.LAbilities, AddKeywords: kws, UntilEOT: true})
				b.lossy("keyword grant staged")
			}
		}
	}
}

// gorgeKeyword maps spec 6.10 keyword names to the gorge keywords a pump
// can grant (the ones combat and targeting read).
var gorgeKeyword = map[string]string{
	"flying": "Flying", "reach": "Reach", "haste": "Haste", "vigilance": "Vigilance", "trample": "Trample",
	"first_strike": "First Strike", "double_strike": "Double Strike", "deathtouch": "Deathtouch",
	"menace": "Menace", "defender": "Defender", "lifelink": "Lifelink", "hexproof": "Hexproof",
	"indestructible": "Indestructible", "shroud": "Shroud",
}

// advance positions the engine at its next decision: priority to the
// observed priority seat (one pass counted when it is not the active seat:
// the active seat passed to it), then Advance.
func (b *builder) advance() {
	if b.sh.Fatal != "" {
		return
	}
	if b.obs.PrioritySeat != nil {
		pp := seatID(*b.obs.PrioritySeat)
		passes := int32(0)
		if pp != b.g.Active {
			passes = 1
		}
		b.ev(events.Event{Kind: events.Priority, Player: pp, Amount: passes})
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				b.sh.Fatal = fmt.Sprintf("advance panicked: %v", r)
			}
		}()
		b.e.Advance()
	}()
	if b.sh.Fatal != "" {
		return
	}
	if b.g.Over {
		b.sh.Fatal = "staged game is over"
		return
	}
	d := b.e.Pending()
	if d == nil {
		b.sh.Fatal = "no pending decision"
		return
	}
	if d.Player != b.sh.Me {
		b.sh.Fatal = fmt.Sprintf("pending decision is the opponent's (%s)", d.Kind)
	}
}

// pickTrigger chooses which of the source face's triggers a stack trigger
// is: the only one; else the zone-change trigger the source's position
// implies; else the first (sure false).
func pickTrigger(f *cards.Face, o *state.Object) (int, bool) {
	switch len(f.Triggers) {
	case 0:
		return -1, false
	case 1:
		return 0, true
	}
	var enters, leaves []int
	for i, tr := range f.Triggers {
		if tr.Mode != "ChangesZone" && tr.Mode != "ChangesZoneAll" {
			continue
		}
		switch {
		case tr.Params["Destination"] == "Battlefield":
			enters = append(enters, i)
		case tr.Params["Origin"] == "Battlefield":
			leaves = append(leaves, i)
		}
	}
	if o.Zone == state.ZBattlefield && o.SummonSick && len(enters) == 1 {
		return enters[0], true
	}
	if o.Zone != state.ZBattlefield && len(leaves) == 1 {
		return leaves[0], true
	}
	if o.Zone == state.ZBattlefield && len(enters) == 1 {
		return enters[0], false
	}
	return 0, false
}

// cardNamed finds any deck object (either seat) with a face named name.
func (b *builder) cardNamed(name string) (state.ObjID, bool) {
	for p := 0; p < 2; p++ {
		for _, id := range b.deckObj[p] {
			if b.hasName(id, name) {
				return id, true
			}
		}
	}
	return 0, false
}
