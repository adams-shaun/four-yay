package botpolicy

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The incremental board build.
//
// The search bench refills a Board at every bot decision of every
// simulation, and between two refills of the same engine almost nothing
// moves: measured on the az leg, 96% of refills follow a refill of the same
// engine, the derived-characteristics key is unchanged across 71% of them,
// and the events in between name about one object. Yet the from-scratch
// fill re-derived every battlefield creature and every card in the seat's
// own zones (Characteristics) and re-read every face's static facts.
//
// boardInc is a Board's per-object row cache, dense by ObjID, that carries
// both across refills:
//
//   - a face's static facts (creature, basic, instant, counter spell, mana
//     value), keyed by the face pointer they were read from -- a face is
//     immutable compiled data, and the row holding the pointer keeps it
//     alive, so an equal pointer is the same face;
//   - the object's derived power, toughness and keyword facts, keyed by the
//     engine's own Derived-memo key (rules.Engine.BoardReadKey): they are
//     reused only on the same engine lineage, at an unchanged key, for an
//     object whose derivation the engine reuses across walks
//     (CharacteristicsReusable) and that no event logged since the last
//     refill named. That is exactly the cross-walk memo's reuse condition
//     (rules/derivedmemo.go), so a reused row is what Characteristics would
//     answer.
//
// The tables themselves are rebuilt in full every refill, in the scratch
// fill's exact order, from those rows and the objects' own fields (tapped,
// damage, controller, attachment, activation count, summoning sickness),
// which are read fresh: only the derivation and the face reads are skipped.
// boardIncVerify (the botpolicy test binary, or boardIncVerifyFlag at link
// time) refills every incremental Board from scratch as well and panics on
// any difference, entry order included.

// boardIncVerifyFlag turns verify mode on in a non-test binary:
// go build -ldflags "-X github.com/adams-shaun/gorge/botpolicy.boardIncVerifyFlag=1".
var boardIncVerifyFlag string

var boardIncVerify = boardIncVerifyFlag != ""

// boardReadKeyer is the Chars capability the incremental build needs
// (*rules.Engine): the Derived memo's cross-read key and its per-object
// reuse exclusion. A Chars without it is always filled from scratch.
type boardReadKeyer interface {
	BoardReadKey() (lineage *events.Log, seq uint64, version, objs int, ok bool)
	CharacteristicsReusable(id state.ObjID) bool
}

// boardInc is one Board's row cache (see above). Board copies made after
// it exists share it, like they share the tables.
type boardInc struct {
	lineage   *events.Log
	g         *state.Game
	seen      int // the lineage's log length at the last refill
	seq       uint64
	ver, objs int
	// gen is the current key's generation: a row whose gen equals it holds
	// derived facts valid now. It moves whenever the lineage or the key
	// moves; 0 is never current.
	gen uint32
	// fill numbers the refills, so an object read twice in one refill (the
	// creature census, then the card census) is derived once even when its
	// derivation is not reusable across refills.
	fill uint32
	rows []boardRow
	// derivations counts the Characteristics queries the cache could not
	// answer (the whole-game test's non-vacuity reads it).
	derivations uint64
}

// boardRow is one object's cached facts.
type boardRow struct {
	face *cards.Face
	gen  uint32
	fill uint32
	// static facts of face
	creature, basic, instant, counter, reusable bool
	cmc                                         int32
	// pflash/pflashback are the face's printed keyword list's Flash and
	// Flashback facts.
	pflash, pflashback bool
	// derived facts (valid per gen/fill). kw is the derived keyword list:
	// the face's own printed list when the derivation answered with exactly
	// that (immutable compiled data, so it is carried, not copied), else a
	// copy in kwBuf.
	flash, flashback bool
	power, toughness int32
	kw, kwBuf        []string
}

func (inc *boardInc) nextGen() {
	inc.gen++
	if inc.gen == 0 {
		for i := range inc.rows {
			inc.rows[i].gen = 0
		}
		inc.gen = 1
	}
}

// sync brings the cache's key to the engine's current one: a new lineage
// (or a log that is not an extension of the one last seen) or a moved key
// retires every row's derived facts; otherwise only the objects the events
// logged since the last refill name are retired.
func (inc *boardInc) sync(lineage *events.Log, g *state.Game, seq uint64, ver, objs int) {
	evs := lineage.Events
	if inc.lineage != lineage || inc.g != g || len(evs) < inc.seen {
		inc.lineage, inc.g = lineage, g
		inc.nextGen()
	} else {
		rows := inc.rows
		for i := inc.seen; i < len(evs); i++ {
			if id := evs[i].Obj; id != 0 && int(id) < len(rows) {
				rows[id].gen = 0
			}
		}
		if seq != inc.seq || ver != inc.ver || objs != inc.objs {
			inc.nextGen()
		}
	}
	inc.seen, inc.seq, inc.ver, inc.objs = len(evs), seq, ver, objs
	inc.fill++
	if inc.fill == 0 {
		for i := range inc.rows {
			inc.rows[i].fill = 0
		}
		inc.fill = 1
	}
}

// face is id's row with its face facts current for f.
func (inc *boardInc) face(id state.ObjID, f *cards.Face, k boardReadKeyer) *boardRow {
	if int(id) >= len(inc.rows) {
		inc.rows = slices.Grow(inc.rows, int(id)+1-len(inc.rows))
		inc.rows = inc.rows[:int(id)+1]
	}
	r := &inc.rows[id]
	if r.face != f {
		r.face = f
		r.creature = f.IsCreature()
		r.basic = f.TypeLineHas("Basic", twBasic)
		r.instant = f.TypeLineHas("Instant", twInstant)
		r.counter = isCounterSpell(f)
		r.cmc = cmcOfFace(f)
		r.pflash, r.pflashback = hasFlash(f.Keywords), hasFlashback(f.Keywords)
		r.reusable = k.CharacteristicsReusable(id)
		r.gen, r.fill = 0, 0
	}
	return r
}

// derive makes r's derived facts current for id.
func (inc *boardInc) derive(r *boardRow, id state.ObjID, combined combinedChars) {
	if r.fill == inc.fill || r.gen == inc.gen {
		r.fill = inc.fill
		return
	}
	inc.derivations++
	p, t, kw := combined.Characteristics(id)
	r.power, r.toughness = p, t
	if n := len(kw); n > 0 && n == len(r.face.Keywords) && &kw[0] == &r.face.Keywords[0] {
		r.kw = kw[:n:n]
		r.flash, r.flashback = r.pflash, r.pflashback
	} else {
		r.kwBuf = append(r.kwBuf[:0], kw...)
		r.kw = r.kwBuf
		r.flash, r.flashback = hasFlash(kw), hasFlashback(kw)
	}
	r.fill = inc.fill
	if r.reusable {
		r.gen = inc.gen
	} else {
		r.gen = 0
	}
}

// keywords is r's derived keyword list as the Board carries it: nil when
// empty, else capped so no reader's append reaches the row's storage.
func (r *boardRow) keywords() []string {
	if n := len(r.kw); n > 0 {
		return r.kw[:n:n]
	}
	return nil
}

// fillTablesInc fills Life, Creatures and Cards (the creature census and
// both card censuses of BoardFromGameInto, in the same order) from the row
// cache, reporting false -- having written nothing -- when ch cannot key it.
// The tables were Reset by the caller.
func (b *Board) fillTablesInc(g *state.Game, ch Chars, me state.PlayerID) bool {
	k, ok := ch.(boardReadKeyer)
	if !ok {
		return false
	}
	combined, ok := ch.(combinedChars)
	if !ok {
		return false
	}
	inc := b.inc
	if inc == nil {
		// A Board's first refill is a scratch fill; the cache starts with
		// its second, so a Board built per decision never allocates one.
		if !b.incArmed {
			b.incArmed = true
			return false
		}
		inc = new(boardInc)
		b.inc = inc
	}
	lineage, seq, ver, objs, ok := k.BoardReadKey()
	if !ok {
		inc.lineage = nil
		return false
	}
	inc.sync(lineage, g, seq, ver, objs)
	for i := range g.Players {
		p := &g.Players[i]
		b.Life.Set(p.ID, p.Life)
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			f := o.Face()
			if f == nil || o.Ephemeral() {
				continue
			}
			r := inc.face(id, f, k)
			if !r.creature {
				continue
			}
			inc.derive(r, id, combined)
			*b.Creatures.slot(id) = Creature{
				Power:      r.power,
				Toughness:  r.toughness,
				Damage:     o.Damage,
				Keywords:   r.keywords(),
				Tapped:     o.Tapped,
				Controller: o.Controller,
			}
		}
	}
	for _, z := range [...]state.Zone{state.ZHand, state.ZGraveyard, state.ZBattlefield, state.ZCommand} {
		for _, id := range g.Zone(z, me) {
			o := g.Obj(id)
			if o == nil || o.Ephemeral() {
				continue
			}
			f := o.Face()
			if f == nil {
				continue
			}
			r := inc.face(id, f, k)
			inc.derive(r, id, combined)
			*b.Cards.slot(id) = Card{
				PrintedName:   f.Name,
				Creature:      r.creature,
				Power:         r.power,
				Toughness:     r.toughness,
				CMC:           r.cmc,
				Basic:         r.basic,
				AttachedTo:    o.AttachedTo,
				Activated:     o.ActivatedThisTurn,
				ManaCost:      f.ManaCost,
				Castable:      z == state.ZHand || z == state.ZCommand || (z == state.ZGraveyard && r.flashback),
				OnBattlefield: z == state.ZBattlefield,
				Tapped:        o.Tapped,
				Sick:          o.SummonSick,
				Produces:      f.ManaProduction(),
				InstantSpeed:  r.instant || r.flash,
				Counter:       r.counter,
			}
		}
	}
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == me {
			continue
		}
		for _, id := range g.Zone(state.ZBattlefield, p.ID) {
			o := g.Obj(id)
			if o == nil {
				continue
			}
			f := o.Face()
			if f == nil || o.Ephemeral() {
				continue
			}
			if o.FaceDown {
				b.Cards.Set(id, Card{})
				continue
			}
			r := inc.face(id, f, k)
			inc.derive(r, id, combined)
			*b.Cards.slot(id) = Card{
				PrintedName: f.Name,
				Creature:    r.creature,
				Power:       r.power,
				CMC:         r.cmc,
				Basic:       r.basic,
				ManaCost:    f.ManaCost,
				Toughness:   r.toughness,
				Activated:   o.ActivatedThisTurn, // public; A5 (BoardFromGameInto's foreign walk)
			}
		}
	}
	return true
}

// verifyIncBoard is boardIncVerify's check: b (just filled incrementally)
// must equal a from-scratch fill of the same seat at the same state, every
// table in the same entry order.
func verifyIncBoard(g *state.Game, ch Chars, me state.PlayerID, b *Board) {
	ref := NewBoard(len(g.Players)) // a fresh Board's one refill is a scratch fill
	BoardFromGameInto(g, ch, me, &ref)
	if d := boardsDiffer(*b, ref); d != "" {
		panic(fmt.Sprintf("botpolicy: incremental board for seat %d at log %d differs from a scratch fill: %s", me, b.inc.seen, d))
	}
}

// boardsDiffer is the first difference between two filled Boards' facts,
// table order included, or "".
func boardsDiffer(a, b Board) string {
	switch {
	case !tablesSame(a.Creatures, b.Creatures):
		return fmt.Sprintf("Creatures %v / %v vs %v / %v", a.Creatures.Keys(), a.Creatures.Values(), b.Creatures.Keys(), b.Creatures.Values())
	case !tablesSame(a.Cards, b.Cards):
		return fmt.Sprintf("Cards %v / %+v vs %v / %+v", a.Cards.Keys(), a.Cards.Values(), b.Cards.Keys(), b.Cards.Values())
	case !tablesSame(a.Life, b.Life):
		return "Life"
	case !tablesSame(a.Commanders, b.Commanders):
		return "Commanders"
	case !reflect.DeepEqual(a.Stack, b.Stack) && (len(a.Stack) != 0 || len(b.Stack) != 0):
		return "Stack"
	case a.OwnDeck != b.OwnDeck || a.IsMain != b.IsMain || a.FirstMain != b.FirstMain || a.MyTurn != b.MyTurn ||
		a.Step != b.Step || a.Pool != b.Pool || a.PoolRestricted != b.PoolRestricted ||
		a.LibrarySize != b.LibrarySize || a.HandSize != b.HandSize:
		return "scalars"
	}
	return ""
}

func tablesSame[K TableKey, V any](a, b IDTable[K, V]) bool {
	if !slices.Equal(a.Keys(), b.Keys()) {
		return false
	}
	av, bv := a.Values(), b.Values()
	for i := range av {
		if !reflect.DeepEqual(av[i], bv[i]) {
			return false
		}
	}
	for _, k := range a.Keys() {
		if a.index(k) < 0 || b.index(k) < 0 {
			return false
		}
	}
	return true
}
