package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// The magecraft task's pins: Mode$ SpellCastOrCopy (the "Whenever you cast
// or copy an instant or sorcery spell, ..." family -- Jadzi, Oracle of
// Arcavios, the Strixhaven apprentices, Storm-Kiln Artist) and Mode$
// SpellCopy ("Whenever you copy a spell, ...") never fired at all --
// triggerMatches' per-mode switch had no case for either mode, so the
// trigger silently never queued. The copy half is a second, distinct gap:
// copies do not re-enter the stack as a PutOnStack -- effects/copy.go emits
// events.StackCopy naming the ORIGINAL spell -- so even a SpellCastOrCopy
// case that delegated everything to trigmatch.spellCastMatches would have kept the
// copy half dead.

// magecraftSrc is the corpus family's exact trigger shape on a synthetic
// carrier (never a committed Forge script): bare "Instant,Sorcery" +
// ValidActivatingPlayer$ You + TriggerZones$ Battlefield.
const magecraftSrc = `Name:Apprentice
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCastOrCopy | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ Magecraft — Whenever you cast or copy an instant or sorcery spell, draw a card.
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`

func stackInstant(t testing.TB, e *Engine, p state.PlayerID, src string) state.ObjID {
	t.Helper()
	c := card(t, src)
	o := e.G.AddObject(c, p)
	o.Zone = state.ZStack
	e.G.SetZone(state.ZStack, p, append(e.G.Stack, o.ID))
	return o.ID
}

// TestMagecraftSpellCastOrCopyFiresOnACast: a real cast (the deferred
// PutOnStack walk) queues the magecraft trigger beside the spell.
func TestMagecraftSpellCastOrCopyFiresOnACast(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, magecraftSrc)
	spell := card(t, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	o := e.G.AddObject(spell, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	e.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
		From: state.ZHand, To: state.ZStack})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 2 { // the spell itself, plus the trigger
		t.Fatalf("stack = %v, want the spell plus one magecraft trigger", e.G.Stack)
	}
}

// TestMagecraftSpellCastOrCopyFiresOnACopy: a StackCopy event queues the
// magecraft trigger. The copy MINT itself adds a stack entry, so the count
// is against the pre-emit baseline, never against zero.
func TestMagecraftSpellCastOrCopyFiresOnACopy(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	onBoard(t, e, 0, magecraftSrc)
	spellID := stackInstant(t, e, 0, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	before := len(e.G.Stack)
	e.emit(events.Event{Kind: events.StackCopy, Obj: spellID, Player: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) != before+2 { // the minted copy, plus the trigger
		t.Fatalf("stack %d -> %d, want baseline + copy + one magecraft trigger", before, len(e.G.Stack))
	}
}

// TestSpellCastTriggerStaysSilentOnACopy is the boundary: a plain Mode$
// SpellCast trigger does not fire for a copy -- a copy is not a cast.
func TestSpellCastTriggerStaysSilentOnACopy(t *testing.T) {
	t.Parallel()
	src := `Name:Watcher
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCast | ValidCard$ Instant | ValidActivatingPlayer$ You | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	spellID := stackInstant(t, e, 0, "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n")
	before := len(e.G.Stack)
	e.emit(events.Event{Kind: events.StackCopy, Obj: spellID, Player: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) != before+1 { // only the minted copy, no trigger
		t.Fatalf("stack %d -> %d, want baseline + copy only (SpellCast silent on a copy)", before, len(e.G.Stack))
	}
}

// TestMagecraftSpellCastOrCopyRejectsNonMatchingShapes: a copy of a spell
// outside ValidCard$ and a copy by a player outside ValidActivatingPlayer$
// queue nothing beyond the mint.
func TestMagecraftSpellCastOrCopyRejectsNonMatchingShapes(t *testing.T) {
	t.Parallel()
	instSrc := "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	sorcSrc := "Name:Rites\nManaCost:1 B\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1 | Defined$ You\nOracle:x\n"
	t.Run("non-matching spell", func(t *testing.T) {
		// ValidCard$ Instant: copying a sorcery does not fire.
		src := `Name:Apprentice
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCastOrCopy | ValidCard$ Instant | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
		e := layerEngine(t)
		onBoard(t, e, 0, src)
		spellID := stackInstant(t, e, 0, sorcSrc)
		before := len(e.G.Stack)
		e.emit(events.Event{Kind: events.StackCopy, Obj: spellID, Player: 0})
		e.putTriggersOnStack()
		if len(e.G.Stack) != before+1 {
			t.Fatalf("stack %d -> %d, want baseline + copy only", before, len(e.G.Stack))
		}
	})
	t.Run("non-matching activator", func(t *testing.T) {
		// ValidActivatingPlayer$ Opponent: seat 0's copy does not fire seat
		// 0's own watcher (its controller).
		src := `Name:Apprentice
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCastOrCopy | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ Opponent | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
		e := layerEngine(t)
		onBoard(t, e, 0, src)
		spellID := stackInstant(t, e, 0, instSrc)
		before := len(e.G.Stack)
		e.emit(events.Event{Kind: events.StackCopy, Obj: spellID, Player: 0})
		e.putTriggersOnStack()
		if len(e.G.Stack) != before+1 {
			t.Fatalf("stack %d -> %d, want baseline + copy only", before, len(e.G.Stack))
		}
	})
}

// TestMagecraftSecondaryYieldsToItsSpellCastOrCopyPrimary: the paired
// Secondary$ SpellCopy companion (the mage_hunter / leonin_lightscribe
// shape -- same Execute$ SVar, Secondary$ True) must not double-fire beside
// its SpellCastOrCopy primary, on a copy OR on a cast.
func TestMagecraftSecondaryYieldsToItsSpellCastOrCopyPrimary(t *testing.T) {
	t.Parallel()
	src := `Name:Paired
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCastOrCopy | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ x
T:Mode$ SpellCopy | ValidCard$ Instant,Sorcery | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw | Secondary$ True | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
	instSrc := "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	t.Run("on a copy", func(t *testing.T) {
		e := layerEngine(t)
		onBoard(t, e, 0, src)
		spellID := stackInstant(t, e, 0, instSrc)
		before := len(e.G.Stack)
		e.emit(events.Event{Kind: events.StackCopy, Obj: spellID, Player: 0})
		e.putTriggersOnStack()
		if len(e.G.Stack) != before+2 { // copy + exactly ONE trigger
			t.Fatalf("stack %d -> %d, want baseline + copy + ONE trigger", before, len(e.G.Stack))
		}
	})
	t.Run("on a cast", func(t *testing.T) {
		e := layerEngine(t)
		onBoard(t, e, 0, src)
		o := e.G.AddObject(card(t, instSrc), 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
		e.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
			From: state.ZHand, To: state.ZStack})
		e.putTriggersOnStack()
		if len(e.G.Stack) != 2 { // the spell + exactly ONE trigger
			t.Fatalf("stack = %v, want the spell plus ONE trigger", e.G.Stack)
		}
	})
}

// TestSpellCopyPrimarySilentOnAPlainCast: a Mode$ SpellCopy-only trigger
// (the parnesse/the_twelfth_doctor shape) does not fire for a plain cast --
// only for a copy.
func TestSpellCopyPrimarySilentOnAPlainCast(t *testing.T) {
	t.Parallel()
	src := `Name:Brush
ManaCost:1 U
Types:Creature Wizard
PT:1/1
T:Mode$ SpellCopy | ValidActivatingPlayer$ You | TriggerZones$ Battlefield | Execute$ TrigDraw | TriggerDescription$ x
SVar:TrigDraw:DB$ Draw | NumCards$ 1 | Defined$ You
Oracle:x
`
	instSrc := "Name:Shock\nManaCost:R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e := layerEngine(t)
	onBoard(t, e, 0, src)
	// A plain cast: no trigger.
	o := e.G.AddObject(card(t, instSrc), 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, []state.ObjID{o.ID})
	e.emit(events.Event{Kind: events.PutOnStack, Obj: o.ID, Player: 0,
		From: state.ZHand, To: state.ZStack})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want the spell alone (SpellCopy silent on a plain cast)", e.G.Stack)
	}
	// A copy of it: the trigger fires (counted against the pre-emit
	// baseline: the stack already holds the original spell).
	before := len(e.G.Stack)
	e.emit(events.Event{Kind: events.StackCopy, Obj: o.ID, Player: 0})
	e.putTriggersOnStack()
	if len(e.G.Stack) != before+2 {
		t.Fatalf("stack %d -> %d, want baseline + copy + one SpellCopy trigger", before, len(e.G.Stack))
	}
}

// TestJadziMagecraftCastRevealsAndOffersThePlayAsk is the end-to-end pin on
// the REAL corpus card: battlefield Jadzi, cast Giant Growth, the magecraft
// trigger resolves, reveals the library's top card (a Grizzly Bears), and
// the chained DB$ Play | PlayCost$ 1 offers to cast it for {1}; answering
// the ask casts the Bears, which resolve onto the battlefield. Jadzi is in
// NO repo deck and NO legacy golden deck, so no chain head depends on this
// card (measured: grepping every SpellCastOrCopy carrier name against
// internal/testutil/decks/*.json returns nothing).
func TestJadziMagecraftCastRevealsAndOffersThePlayAsk(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	forest := searchCorpusCard(t, reg, "Forest")
	mountain := searchCorpusCard(t, reg, "Mountain")
	bears := searchCorpusCard(t, reg, "Grizzly Bears")
	growth := searchCorpusCard(t, reg, "Giant Growth")
	deck := []*cards.Card{searchCorpusCard(t, reg, "Jadzi, Oracle of Arcavios"), bears, growth}
	deck = append(deck, forest, mountain)
	for i := 0; i < 18; i++ {
		deck = append(deck, forest, mountain)
	}
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9207, Names: []string{"jadzi", "opponent"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: reg.Tokens})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	jadziID := searchMoveByName(t, e, "Jadzi, Oracle of Arcavios", state.ZBattlefield)
	if jadziID == 0 {
		t.Fatal("Jadzi not found in hand or library")
	}
	// The library's top card must be the Bears: the trigger reveals exactly
	// it and the chained play offers exactly it. The opening hand may have
	// taken them, so first move the Bears back into the library.
	bearsID := searchMoveByName(t, e, "Grizzly Bears", state.ZLibrary)
	lib := e.G.Zone(state.ZLibrary, 0)
	order := []state.ObjID{bearsID}
	for _, id := range lib {
		if id != bearsID {
			order = append(order, id)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: order})
	// Mana for the Growth ({G}) and for the offered {1} play.
	addMana(t, e, 0, "GG1")
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == jadziID {
			t.Fatalf("Jadzi must not be castable here; options = %+v", d.Options)
		}
		if o.Kind == "cast" && o.Obj != 0 && e.G.Obj(o.Obj) != nil &&
			e.G.Obj(o.Obj).Face() != nil && e.G.Obj(o.Obj).Face().Name == "Giant Growth" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Giant Growth: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// Giant Growth targets a creature: Jadzi is the only one.
	if d = e.Pending(); d != nil && d.Kind == decision.KTarget {
		submitChoices(t, e, d.Options[0].Index)
	}
	// Both seats pass: the Growth resolves, the magecraft trigger fires and
	// resolves, and the chained play ask is the first non-priority decision.
	d = passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("after the cast: %+v, want the PlayCost$ 1 play ask", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != bearsID {
		t.Fatalf("play ask options = %+v, want exactly Play Grizzly Bears (%d)", d.Options, bearsID)
	}
	// The public reveal Note names the revealed card.
	revealed := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && !ev.Secret && containsID(ev.IDs, bearsID) {
			revealed++
		}
	}
	if revealed == 0 {
		t.Fatal("no public reveal Note naming the revealed Bears")
	}
	// Answer the ask: the Bears are cast for {1} and resolve.
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(bearsID); o.Zone != state.ZBattlefield {
		t.Fatalf("Bears zone = %s, want battlefield after the answered play", o.Zone)
	}
}
