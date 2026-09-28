package v2engine

import (
	"strconv"

	"github.com/adams-shaun/gorge/internal/spellbench/v2agent"
	"github.com/adams-shaun/gorge/state"
)

// look is a card in a zone hidden from the viewer that the current gorge
// decision shows it (spec 6.7): a library card being searched or looked at,
// or a card of the other seat's hand being revealed.
type look struct {
	obj state.ObjID
	how string // searching, looked_at, revealed
}

// obsBuild is one observation for one viewer, with the object reference of
// every object it holds (the only references a decision may carry, V4).
type obsBuild struct {
	g      *Game
	viewer state.PlayerID
	obs    *v2agent.Observation
	refs   map[state.ObjID]v2agent.ObjectRef // lookup only
	// hidden marks refs to cards in zones hidden from the viewer (V5 order).
	hidden map[state.ObjID]bool // lookup only
}

func (b *obsBuild) ref(id state.ObjID) (v2agent.ObjectRef, bool) {
	r, ok := b.refs[id]
	return r, ok
}

func (b *obsBuild) refPtr(id state.ObjID) *v2agent.ObjectRef {
	if r, ok := b.refs[id]; ok {
		return &r
	}
	return nil
}

// visibleName is the name the viewer may see for o, or nil.
func (g *Game) visibleName(o *state.Object, viewer state.PlayerID) *string {
	f := o.Face()
	if f == nil {
		return nil
	}
	if o.FaceDown {
		looker := o.Controller
		if o.HasMayLook {
			looker = o.MayLookPlayer
		}
		if viewer != looker {
			return nil
		}
	}
	name := f.Name
	if o.Zone == state.ZBattlefield && !o.FaceDown {
		if n := g.e.Name(o.ID); n != "" {
			name = n
		}
	}
	return &name
}

func (g *Game) characteristics(o *state.Object, viewer state.PlayerID) *v2agent.Characteristics {
	f := o.Face()
	if f == nil {
		return nil
	}
	onBF := o.Zone == state.ZBattlefield
	if o.FaceDown && !onBF && o.Zone != state.ZStack && g.visibleName(o, viewer) == nil {
		return nil
	}
	if o.FaceDown && o.Zone == state.ZStack {
		// A face-down spell shows its face-down characteristics to every
		// viewer (CR 708.2, 708.4): a nameless 2/2 colourless creature.
		two := int32(2)
		return &v2agent.Characteristics{Supertypes: []string{}, Types: []string{"creature"}, Subtypes: []string{},
			Colors: []string{}, Power: &two, Toughness: &two, Keywords: []string{}}
	}
	var words []string
	var colors string
	var kws []string
	if onBF {
		d := g.e.Derived(o.ID)
		words, colors, kws = d.Types, d.Colors, d.Keywords
	} else {
		words, kws = f.Types, f.Keywords
		colors = colorLettersOf(o)
	}
	c := &v2agent.Characteristics{}
	c.Supertypes, c.Types, c.Subtypes = splitTypes(words)
	c.Colors = colorWords(colors)
	mv := f.Cmc()
	if o.FaceDown {
		mv = 0
	}
	if o.Zone == state.ZStack && o.X > 0 {
		mv += o.X
	}
	if mv < 0 {
		mv = 0
	}
	c.ManaValue = uint32(mv)
	if c.HasType("creature") {
		var p, t int32
		if onBF {
			p, t = g.e.Power(o.ID), g.e.Toughness(o.ID)
		} else {
			p, t = int32(f.Power()), int32(f.Toughness())
		}
		c.Power, c.Toughness = &p, &t
	}
	c.Keywords = []string{}
	seen := map[string]bool{} // lookup only
	for _, k := range kws {
		if n := keywordName(k); n != "" && !seen[n] {
			seen[n] = true
			c.Keywords = append(c.Keywords, n)
		}
	}
	return c
}

// buildObservation builds the viewer's observation (spec 6). looks are the
// hidden-zone cards the pending decision shows.
func (g *Game) buildObservation(viewer state.PlayerID, priority bool, looks []look) *obsBuild {
	gs := g.e.G
	b := &obsBuild{g: g, viewer: viewer, refs: map[state.ObjID]v2agent.ObjectRef{}, hidden: map[state.ObjID]bool{}}
	obs := &v2agent.Observation{
		Viewer: seatOf(viewer), Turn: int64(gs.Turn), PhaseStep: phaseStep(gs.Step),
		Stack: []v2agent.StackEntry{}, Known: []v2agent.KnownEntry{},
	}
	if gs.Turn <= 0 {
		obs.Turn = 1
	}
	active := seatOf(gs.Active)
	obs.ActiveSeat = &active
	if priority {
		ps := seatOf(g.e.Pending().Player)
		obs.PrioritySeat = &ps
	}
	b.obs = obs

	// Records, first pass: every visible object gets its reference.
	type placed struct {
		o      *state.Object
		pi, ri int
	}
	var bfObjs []placed
	var stackObjs []*state.Object
	players := make([]v2agent.Player, 2)
	seen := map[state.ObjID]bool{} // lookup only
	mkRec := func(o *state.Object) v2agent.ObjectRecord {
		name := g.visibleName(o, viewer)
		controller := o.Owner
		if o.Zone == state.ZBattlefield {
			controller = o.Controller
		}
		r := v2agent.ObjectRef{ObjectID: g.objectID(viewer, o.ID, ""), CardName: name,
			OwnerSeat: seatOf(o.Owner), ControllerSeat: seatOf(controller), Zone: zoneName(o.Zone)}
		b.refs[o.ID] = r
		seen[o.ID] = true
		return v2agent.ObjectRecord{ObjectRef: r, FaceDown: o.FaceDown, Token: o.IsToken, Copy: o.IsCopy,
			Characteristics: g.characteristics(o, viewer)}
	}
	for pi := 0; pi < 2; pi++ {
		p := state.PlayerID(pi)
		pl := &gs.Players[pi]
		players[pi] = v2agent.Player{Seat: seatOf(p), Life: pl.Life,
			ManaPool: v2agent.ManaPool{W: u32(pl.Pool[state.MW]), U: u32(pl.Pool[state.MU]), B: u32(pl.Pool[state.MB]),
				R: u32(pl.Pool[state.MR]), G: u32(pl.Pool[state.MG]), C: u32(pl.Pool[state.MC])},
			LandsPlayedThisTurn: u32(pl.LandsPlayed),
			Battlefield:         []v2agent.ObjectRecord{}, Graveyard: []v2agent.ObjectRecord{},
			Exile: []v2agent.ObjectRecord{}, Command: []v2agent.ObjectRecord{},
		}
		if p == viewer {
			players[pi].Hand = []v2agent.ObjectRecord{}
		}
	}
	// Owned zones, placed by owner (spec 6.3): hands, graveyards, exiles,
	// command zones; the counts of the hidden ones.
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary, state.ZGraveyard, state.ZExile, state.ZCommand} {
		for pi := 0; pi < 2; pi++ {
			for _, o := range g.cards(z, state.PlayerID(pi)) {
				if o.Owner > 1 {
					continue
				}
				pv := &players[o.Owner]
				switch z {
				case state.ZHand:
					pv.HandCount++
					if o.Owner == viewer {
						pv.Hand = append(pv.Hand, mkRec(o))
					}
				case state.ZLibrary:
					pv.LibraryCount++
				case state.ZGraveyard:
					pv.Graveyard = append(pv.Graveyard, mkRec(o))
				case state.ZExile:
					pv.Exile = append(pv.Exile, mkRec(o))
				case state.ZCommand:
					pv.Command = append(pv.Command, mkRec(o))
				}
			}
		}
	}
	for pi := 0; pi < 2; pi++ {
		for _, id := range gs.Zone(state.ZBattlefield, state.PlayerID(pi)) {
			o := gs.Obj(id)
			if o == nil || o.Zone != state.ZBattlefield || o.Face() == nil || seen[id] || o.Controller > 1 {
				continue
			}
			rec := mkRec(o)
			rec.Permanent = &v2agent.Permanent{Tapped: o.Tapped, SummoningSick: o.SummonSick, Damage: u32(o.Damage),
				Counters: map[string]uint32{}, Attacking: o.IsAttacking, BlockedAttackers: []v2agent.ObjectRef{},
				PhasedOut: o.PhasedOut}
			for _, c := range o.Counters {
				if n := counterName(c.Kind); n != "" && c.N > 0 {
					rec.Permanent.Counters[n] += uint32(c.N)
				}
			}
			ctl := int(o.Controller)
			bfObjs = append(bfObjs, placed{o, ctl, len(players[ctl].Battlefield)})
			players[ctl].Battlefield = append(players[ctl].Battlefield, rec)
		}
	}
	// Stack entries (index 0 is the bottom).
	for _, id := range gs.Stack {
		o := gs.Obj(id)
		if o == nil || seen[id] {
			continue
		}
		seen[id] = true
		se := v2agent.StackEntry{Targets: []*v2agent.TargetRef{}, FaceDown: o.FaceDown, Copy: o.IsCopy}
		if o.Card != nil && o.Face() != nil {
			se.StackKind = "spell"
			se.Characteristics = g.characteristics(o, viewer)
			if o.FaceDown && o.Controller != viewer {
				se.CardName = nil
			} else {
				n := o.Face().Name
				se.CardName = &n
			}
			if g.hasX(o) {
				x := u32(o.X)
				se.XValue = &x
			}
		} else {
			switch state.StackKindOf(gs, o) {
			case state.StackKindTriggered:
				se.StackKind = "triggered_ability"
			default:
				se.StackKind = "activated_ability"
			}
			if src := gs.Obj(o.Source); src != nil && src.Face() != nil {
				se.CardName = g.visibleName(src, viewer)
			}
		}
		se.ObjectRef = v2agent.ObjectRef{ObjectID: g.objectID(viewer, id, ""), CardName: se.CardName,
			OwnerSeat: seatOf(o.Controller), ControllerSeat: seatOf(o.Controller), Zone: "stack"}
		if o.Card != nil {
			se.ObjectRef.OwnerSeat = seatOf(o.Owner)
		}
		b.refs[id] = se.ObjectRef
		obs.Stack = append(obs.Stack, se)
		stackObjs = append(stackObjs, o)
	}
	// Hidden-zone cards the decision shows (spec 6.7), with fresh ids.
	for _, lk := range looks {
		o := gs.Obj(lk.obj)
		if o == nil || o.Face() == nil || seen[lk.obj] {
			continue
		}
		seen[lk.obj] = true
		name := o.Face().Name
		id := g.objectID(viewer, lk.obj, "look:"+strconv.Itoa(int(g.lookN(viewer, lk.obj))))
		ke := v2agent.KnownEntry{OwnerSeat: seatOf(o.Owner), Zone: zoneName(o.Zone), CardName: name, ObjectID: &id, How: lk.how}
		if o.Zone == state.ZLibrary && lk.how != "searching" {
			// A searched card's position is not part of the knowledge
			// (spec 6.7: a searching entry may have both positions null).
			for i, lid := range gs.Zone(state.ZLibrary, o.Owner) {
				if lid == lk.obj {
					pos := uint32(i)
					ke.PositionFromTop = &pos
					break
				}
			}
		}
		b.refs[lk.obj] = v2agent.ObjectRef{ObjectID: id, CardName: &name, OwnerSeat: ke.OwnerSeat, ControllerSeat: ke.OwnerSeat, Zone: ke.Zone}
		b.hidden[lk.obj] = true
		obs.Known = append(obs.Known, ke)
	}
	sortKnown(obs.Known)

	// Second pass: cross references (absent objects become null, spec 5.1).
	for _, pl := range bfObjs {
		o, perm := pl.o, players[pl.pi].Battlefield[pl.ri].Permanent
		if o.HasAttachedPlayer {
			s := seatOf(state.PlayerID(o.AttachedTo))
			perm.AttachedTo = &v2agent.TargetRef{Player: &s}
		} else if o.AttachedTo != 0 {
			if r := b.refPtr(o.AttachedTo); r != nil && !b.hidden[o.AttachedTo] {
				perm.AttachedTo = &v2agent.TargetRef{Object: r}
			}
		}
		if o.IsAttacking {
			if o.AttackingBattle != 0 {
				if r := b.refPtr(o.AttackingBattle); r != nil {
					perm.AttackTarget = &v2agent.TargetRef{Object: r}
				}
			} else {
				s := seatOf(o.Attacking)
				perm.AttackTarget = &v2agent.TargetRef{Player: &s}
			}
		}
	}
	for _, a := range bfObjs {
		for _, blk := range a.o.BlockedBy {
			for _, bl := range bfObjs {
				if bl.o.ID == blk {
					perm := players[bl.pi].Battlefield[bl.ri].Permanent
					perm.Blocking = true
					if r, ok := b.ref(a.o.ID); ok {
						perm.BlockedAttackers = append(perm.BlockedAttackers, r)
					}
				}
			}
		}
	}
	for si := range obs.Stack {
		se := &obs.Stack[si]
		o := stackObjs[si]
		if se.StackKind != "spell" && o.Source != 0 {
			if src := gs.Obj(o.Source); src != nil && src.Incarnation == o.SourceIncarnation {
				se.Source = b.refPtr(o.Source)
			}
		}
		for _, t := range o.Targets {
			if t.IsPlayer {
				s := seatOf(t.Player)
				se.Targets = append(se.Targets, &v2agent.TargetRef{Player: &s})
			} else if r := b.refPtr(t.Obj); r != nil && !b.hidden[t.Obj] {
				se.Targets = append(se.Targets, &v2agent.TargetRef{Object: r})
			} else {
				se.Targets = append(se.Targets, nil)
			}
		}
	}
	obs.Players = players
	return b
}

// cards lists the real cards (a Face, not ephemeral) of a hidden or public
// zone of p, in zone order.
func (g *Game) cards(z state.Zone, p state.PlayerID) []*state.Object {
	var out []*state.Object
	for _, id := range g.e.G.Zone(z, p) {
		o := g.e.G.Obj(id)
		if o == nil || o.Face() == nil || o.Zone != z || o.Ephemeral() {
			continue
		}
		out = append(out, o)
	}
	return out
}

func (g *Game) hasX(o *state.Object) bool {
	f := o.Face()
	if f == nil {
		return false
	}
	for i := 0; i < len(f.ManaCost); i++ {
		if f.ManaCost[i] == 'X' {
			return true
		}
	}
	return false
}

func colorLettersOf(o *state.Object) string {
	f := o.Face()
	if f == nil {
		return ""
	}
	out := ""
	for _, c := range "WUBRG" {
		for i := 0; i < len(f.ManaCost); i++ {
			if rune(f.ManaCost[i]) == c {
				out += string(c)
				break
			}
		}
	}
	if out == "" && f.Colors != "" {
		for _, w := range []struct {
			l, word string
		}{{"W", "white"}, {"U", "blue"}, {"B", "black"}, {"R", "red"}, {"G", "green"}} {
			if containsFold(f.Colors, w.word) {
				out += w.l
			}
		}
	}
	if f.HasKeyword("Devoid") {
		return ""
	}
	return out
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func u32(n int32) uint32 {
	if n < 0 {
		return 0
	}
	return uint32(n)
}

// sortKnown orders known entries per spec 6.7: owner_seat, zone,
// card_name, position_from_top, position_from_bottom, how, object_id,
// nulls first.
func sortKnown(k []v2agent.KnownEntry) {
	less := func(a, b *v2agent.KnownEntry) bool {
		if a.OwnerSeat != b.OwnerSeat {
			return a.OwnerSeat < b.OwnerSeat
		}
		if a.Zone != b.Zone {
			return a.Zone < b.Zone
		}
		if a.CardName != b.CardName {
			return a.CardName < b.CardName
		}
		if c := cmpU32(a.PositionFromTop, b.PositionFromTop); c != 0 {
			return c < 0
		}
		if c := cmpU32(a.PositionFromBottom, b.PositionFromBottom); c != 0 {
			return c < 0
		}
		if a.How != b.How {
			return a.How < b.How
		}
		return cmpStr(a.ObjectID, b.ObjectID) < 0
	}
	for i := 1; i < len(k); i++ {
		for j := i; j > 0 && less(&k[j], &k[j-1]); j-- {
			k[j], k[j-1] = k[j-1], k[j]
		}
	}
}

func cmpU32(a, b *uint32) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	}
	return 0
}

func cmpStr(a, b *string) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	}
	return 0
}
