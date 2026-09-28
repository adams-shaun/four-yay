package rules

// cost_filter_grammar_test.go — the FILTER-GRAMMAR class of
// TestCostStaticCensus' BROKEN table: cost-modifier statics the engine could
// never apply because their ValidSpell$ / ValidCard$ spelling named a
// construct the matcher did not read. Each carrier is driven through the
// REAL offer gate (legalActions against a floating pool) and, where the
// construct prices a charge, the real cast/payment flow, in BOTH directions:
// the modifier applies when the construct holds and does not when it does
// not. Carriers are corpus cards looked up by name; the spells they price are
// freely-authored fixtures (no Forge script text is committed).

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// cfModes returns the cast modes seat 0 is offered for id right now.
func cfModes(e *Engine, id state.ObjID) map[string]bool {
	out := map[string]bool{}
	for _, o := range e.legalActions(0) {
		if o.Kind == "cast" && o.Obj == id {
			out[o.Mode] = true
		}
	}
	return out
}

// cfHand adds c to seat 0's hand (owned by owner) and returns its id.
func cfHand(e *Engine, c string, t *testing.T, owner state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(card(t, c), owner)
	o.Zone = state.ZHand
	o.Controller = 0
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	return o.ID
}

// cfCorpusPerm puts the named corpus card onto p's battlefield through a
// real MoveZone, so every log-keyed cache sees it.
func cfCorpusPerm(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(corpusAlternativeCard(t, name), p)
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZLibrary, To: state.ZBattlefield})
	return o.ID
}

// cfPool sets seat 0's floating pool to exactly symbols (WUBRGC letters).
func cfPool(e *Engine, symbols string) {
	e.G.Players[0].Pool = state.Mana{}
	for _, r := range symbols {
		e.G.Players[0].Pool[state.ManaIndex(byte(r))]++
	}
}

// cfCast begins a cast of id in mode and answers every follow-up ask with its
// first option until priority returns; it fails the test when id did not
// reach the stack.
func cfCast(t *testing.T, e *Engine, id state.ObjID, mode string) {
	t.Helper()
	castMode(t, e, id, mode)
	for i := 0; i < 12; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		submitChoices(t, e, d.Options[0].Index)
	}
	if z := e.G.Obj(id).Zone; z != state.ZStack {
		t.Fatalf("cast (%s) left the spell in %s, want stack; pending %+v", mode, z, e.Pending())
	}
}

// ---- ValidSpell$ Spell.<cast option> ------------------------------------

// Warbringer: "Dash costs you pay cost {2} less." The dashed cast of a
// {3}{R} / Dash {2}{R} creature costs {R}; the plain cast is untouched.
func TestCostFilterWarbringerReducesDashOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Warbringer"))
	spell := e.G.Zone(state.ZHand, 0)[0]
	cfPool(e, "R")
	if cfModes(e, spell)["dashed"] {
		t.Fatal("precondition: the dash cast is offered from {R} with no Warbringer in play")
	}
	cfCorpusPerm(t, e, 0, "Warbringer")
	if !cfModes(e, spell)["dashed"] {
		t.Fatalf("dash cast not offered from {R} with Warbringer in play: %v", cfModes(e, spell))
	}
	cfPool(e, "RRR")
	if cfModes(e, spell)[""] {
		t.Fatal("the plain {3}{R} cast was offered from three mana: Spell.Dash reduced a non-dash cast")
	}
	cfPool(e, "R")
	cfCast(t, e, spell, "dashed")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the dash cast = %d, want 0 (charged {R})", left)
	}
}

const cfBuybackSpell = "Name:Test Recall\nManaCost:1 U\nTypes:Sorcery\nK:Buyback:3\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"

// Memory Crystal: "Buyback costs cost {2} less." A {1}{U} / Buyback {3}
// spell cast with buyback costs {2}{U}; the plain cast stays {1}{U}.
func TestCostFilterMemoryCrystalReducesBuybackOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	spell := cfHand(e, cfBuybackSpell, t, 0)
	cfPool(e, "UCC")
	if cfModes(e, spell)["buyback"] {
		t.Fatal("precondition: the buyback cast is offered from three mana with no Memory Crystal")
	}
	cfCorpusPerm(t, e, 0, "Memory Crystal")
	if !cfModes(e, spell)["buyback"] {
		t.Fatalf("buyback cast not offered from {U}{C}{C} with Memory Crystal: %v", cfModes(e, spell))
	}
	cfPool(e, "U")
	if cfModes(e, spell)[""] {
		t.Fatal("the plain {1}{U} cast was offered from {U}: Spell.Buyback reduced a cast without buyback")
	}
	cfPool(e, "UCC")
	cfCast(t, e, spell, "buyback")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the buyback cast = %d, want 0 (charged {2}{U})", left)
	}
}

const cfMorphSpell = "Name:Test Morpher\nManaCost:4 G\nTypes:Creature Beast\nPT:4/4\nK:Morph:2 G\nOracle:x\n"

// Dream Chisel: "Face-down creature spells you cast cost {1} less." The
// face-down {3} cast costs {2}; the printed {4}{G} cast is untouched.
func TestCostFilterDreamChiselReducesFaceDownCastOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	spell := cfHand(e, cfMorphSpell, t, 0)
	cfPool(e, "CC")
	if cfModes(e, spell)["morphed"] {
		t.Fatal("precondition: the face-down cast is offered from {2} with no Dream Chisel")
	}
	cfCorpusPerm(t, e, 0, "Dream Chisel")
	if !cfModes(e, spell)["morphed"] {
		t.Fatalf("face-down cast not offered from {2} with Dream Chisel: %v", cfModes(e, spell))
	}
	cfPool(e, "GGGG")
	if cfModes(e, spell)[""] {
		t.Fatal("the printed {4}{G} cast was offered from four mana: Spell.isCastFaceDown reduced a face-up cast")
	}
	cfPool(e, "CC")
	cfCast(t, e, spell, "morphed")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the face-down cast = %d, want 0 (charged {2})", left)
	}
}

// ---- ValidSpell$ Static.<special action> ----------------------------------

const cfPlotSpell = "Name:Test Plotter\nManaCost:3 R\nTypes:Sorcery\nK:Plot:3 R\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"

// Doc Aurlock: "Plotting cards from your hand costs {2} less." Plotting a
// Plot {3}{R} card costs {1}{R}; casting the same card is untouched.
func TestCostFilterDocAurlockReducesPlottingOnly(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	spell := cfHand(e, cfPlotSpell, t, 0)
	cfPool(e, "RC")
	if cfModes(e, spell)["plot"] {
		t.Fatal("precondition: plotting is offered from {1}{R} with no Doc Aurlock")
	}
	cfCorpusPerm(t, e, 0, "Doc Aurlock, Grizzled Genius")
	if !cfModes(e, spell)["plot"] {
		t.Fatalf("plot not offered from {1}{R} with Doc Aurlock: %v", cfModes(e, spell))
	}
	cfPool(e, "RCC")
	if cfModes(e, spell)[""] {
		t.Fatal("the {3}{R} cast was offered from three mana: Static.Plotting reduced a cast")
	}
}

// Inquisitive Glimmer: "Unlock costs you pay cost {1} less." Unlocking a
// Room's {2}{W} door costs {1}{W}.
func TestCostFilterInquisitiveGlimmerReducesUnlock(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	room := cfCorpusPerm(t, e, 0, "Dazzling Theater")
	ro := e.G.Obj(room)
	if lf := roomLockedFace(ro); lf == nil || lf.Name != "Prop Room" {
		t.Fatalf("precondition: the placed room's locked door is not Prop Room: %+v", lf)
	}
	unlockOffered := func() bool {
		for _, o := range e.legalActions(0) {
			if o.Kind == "unlock" && o.Obj == room {
				return true
			}
		}
		return false
	}
	cfPool(e, "WC")
	if unlockOffered() {
		t.Fatal("precondition: the {2}{W} unlock is offered from two mana with no Inquisitive Glimmer")
	}
	cfCorpusPerm(t, e, 0, "Inquisitive Glimmer")
	if !unlockOffered() {
		t.Fatal("unlock not offered from {1}{W} with Inquisitive Glimmer in play")
	}
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "unlock" && o.Obj == room {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no unlock option in the priority decision: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	if !e.G.Obj(room).Unlocked {
		t.Fatal("the room is still locked after the unlock action")
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the unlock = %d, want 0 (charged {1}{W})", left)
	}
}

// Exiled Doomsayer: "All morph costs cost {2} more. (This doesn't affect the
// cost to cast creature spells face down.)" Kin-Tree Warden's {G} turn-up
// costs {2}{G}; the {3} face-down cast is untouched.
func TestCostFilterExiledDoomsayerRaisesMorphUpOnly(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Kin-Tree Warden", "Exiled Doomsayer", "Kin-Tree Warden")
	doom := searchMoveByName(t, e, "Exiled Doomsayer", state.ZBattlefield)
	if e.G.Obj(doom).Zone != state.ZBattlefield {
		t.Fatal("precondition: Exiled Doomsayer is not on the battlefield")
	}
	// The face-down cast still costs exactly {3} (morphDownCast asserts the
	// pool left over): Static.MorphUp must not reach a spell cast.
	id := morphDownCast(t, e, "Kin-Tree Warden", "morphed", "CCCG", 1)
	turnUpOffered := func() bool {
		for _, o := range e.legalActions(0) {
			if o.Kind == "turn_face_up" && o.Obj == id {
				return true
			}
		}
		return false
	}
	if turnUpOffered() {
		t.Fatal("the turn-up was offered from {G} alone: Exiled Doomsayer's {2} raise was not applied")
	}
	addMana(t, e, 0, "CC")
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	if e.G.Obj(id).FaceDown {
		t.Fatal("Kin-Tree Warden is still face down after the turn-up")
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the raised turn-up = %d, want 0 (charged {2}{G})", left)
	}
}

// Exiled Doomsayer's Static.MorphUp is Forge's isMorphUp: a DISGUISE
// turn-up is not a morph cost, so Basilica Stalker's {4}{B} turn-up stays
// {4}{B} with the Doomsayer in play.
func TestCostFilterExiledDoomsayerLeavesDisguiseUpAlone(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Basilica Stalker", "Exiled Doomsayer")
	searchMoveByName(t, e, "Exiled Doomsayer", state.ZBattlefield)
	id := morphDownCast(t, e, "Basilica Stalker", "disguised", "CCCCCB", 3)
	addMana(t, e, 0, "CC")
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	if e.G.Obj(id).FaceDown {
		t.Fatal("Basilica Stalker is still face down after the turn-up")
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the disguise turn-up = %d, want 0 (charged exactly {4}{B})", left)
	}
}

// Harrowing Swarm: "each face-down creature you control gains 'This
// permanent costs {2} less to turn face up'" -- a GRANTED Static.isTurnFaceUp
// reduction (AnimateAll staticAbilities$) with ValidCard$ Card.Self:
// Broodhatch Nantuko's {2}{G} turn-up costs {G}.
func TestCostFilterHarrowingSwarmGrantReducesTurnUp(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, _ := manifestEngine(t, reg, "Broodhatch Nantuko", "Harrowing Swarm")
	id := morphDownCast(t, e, "Broodhatch Nantuko", "morphed", "CCCG", 1)
	for _, o := range e.legalActions(0) {
		if o.Kind == "turn_face_up" && o.Obj == id {
			t.Fatal("precondition: the {2}{G} turn-up is offered from {G} before Harrowing Swarm")
		}
	}
	swarm := searchMoveByName(t, e, "Harrowing Swarm", state.ZHand)
	addMana(t, e, 0, "G")
	castMode(t, e, swarm, "")
	// Resolve the Swarm, answering its manifest-dread pick with the first
	// offered card (the manifested card is a second face-down creature the
	// grant also reaches; the test turns up only the Warden).
	for i := 0; i < 60 && (len(e.G.Stack) > 0 || (e.Pending() != nil && e.Pending().Kind != decision.KPriority)); i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while resolving Harrowing Swarm")
		}
		if d.Kind == decision.KPriority {
			pass := -1
			for _, o := range d.Options {
				if o.Kind == "pass" {
					pass = o.Index
				}
			}
			submitChoices(t, e, pass)
			continue
		}
		choices := make([]int, 0, d.Min)
		for j := 0; j < d.Min && j < len(d.Options); j++ {
			choices = append(choices, d.Options[j].Index)
		}
		if len(choices) == 0 && len(d.Options) > 0 {
			choices = append(choices, d.Options[0].Index)
		}
		submitChoices(t, e, choices...)
	}
	if z := e.G.Obj(swarm).Zone; z != state.ZGraveyard {
		t.Fatalf("precondition: Harrowing Swarm is in %s after resolving, want graveyard", z)
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("precondition: pool after Harrowing Swarm = %d, want 0", left)
	}
	addMana(t, e, 0, "G")
	idx := turnFaceUpIndex(t, e, id)
	submitChoices(t, e, idx)
	if e.G.Obj(id).FaceDown {
		t.Fatal("Broodhatch Nantuko is still face down after the reduced turn-up")
	}
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the reduced turn-up = %d, want 0 (charged {G})", left)
	}
}

// ---- ValidCard$ predicates ------------------------------------------------

const (
	cfBear      = "Name:Test Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	cfFlashBear = "Name:Test Flash Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nK:Flash\nOracle:x\n"
	cfMutator   = "Name:Test Mutator\nManaCost:2 G\nTypes:Creature Beast\nPT:3/3\nK:Mutate:3 G\nOracle:x\n"
	cfBigBear   = "Name:Test Big Bear\nManaCost:2 G\nTypes:Creature Bear\nPT:3/3\nOracle:x\n"
	cfXBlast    = "Name:Test X Blast\nManaCost:X 1 R\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	cfShock     = "Name:Test Jolt\nManaCost:1 R\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	cfInstant   = "Name:Test Insight\nManaCost:2 U\nTypes:Instant\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
	cfSorcery   = "Name:Test Study\nManaCost:2 U\nTypes:Sorcery\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"
)

// Gonti, Canny Acquisitor: "Spells you cast but don't own cost {1} less."
func TestCostFilterGontiReducesSpellsYouDontOwn(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	theirs := cfHand(e, cfBear, t, 1)
	mine := cfHand(e, cfBear, t, 0)
	cfCorpusPerm(t, e, 0, "Gonti, Canny Acquisitor")
	cfPool(e, "G")
	if !cfModes(e, theirs)[""] {
		t.Fatal("a {1}{G} spell seat 0 does not own was not offered from {G} with Gonti in play")
	}
	if cfModes(e, mine)[""] {
		t.Fatal("seat 0's OWN {1}{G} spell was offered from {G}: YouDontOwn matched an owned card")
	}
}

// Cunning Nightbonder: "Spells with flash you cast cost {1} less."
func TestCostFilterCunningNightbonderReducesFlashSpells(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	flash := cfHand(e, cfFlashBear, t, 0)
	plain := cfHand(e, cfBear, t, 0)
	cfCorpusPerm(t, e, 0, "Cunning Nightbonder")
	cfPool(e, "G")
	if !cfModes(e, flash)[""] {
		t.Fatal("a {1}{G} flash spell was not offered from {G} with Cunning Nightbonder in play")
	}
	if cfModes(e, plain)[""] {
		t.Fatal("a {1}{G} spell WITHOUT flash was offered from {G}: hasKeywordFlash matched it")
	}
}

// Pollywog Symbiote: "Each creature spell you cast costs {1} less to cast if
// it has mutate."
func TestCostFilterPollywogSymbioteReducesMutateCreatures(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	mut := cfHand(e, cfMutator, t, 0)
	plain := cfHand(e, cfBigBear, t, 0)
	cfCorpusPerm(t, e, 0, "Pollywog Symbiote")
	cfPool(e, "GG")
	if !cfModes(e, mut)[""] {
		t.Fatal("a {2}{G} creature with mutate was not offered from {G}{G} with Pollywog Symbiote in play")
	}
	if cfModes(e, plain)[""] {
		t.Fatal("a {2}{G} creature WITHOUT mutate was offered from {G}{G}: withMutate matched it")
	}
}

// Zimone, Infinite Analyst: "The first spell you cast with {X} in its mana
// cost each turn costs {1} less to cast for each +1/+1 counter on Zimone."
func TestCostFilterZimoneReducesXCostSpells(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	xs := cfHand(e, cfXBlast, t, 0)
	plain := cfHand(e, cfShock, t, 0)
	z := cfCorpusPerm(t, e, 0, "Zimone, Infinite Analyst")
	e.emit(events.Event{Kind: events.CounterChange, Obj: z, Counter: "P1P1", Amount: 1})
	cfPool(e, "R")
	if !cfModes(e, xs)[""] {
		t.Fatal("a {X}{1}{R} spell was not offered from {R} with a one-counter Zimone in play")
	}
	if cfModes(e, plain)[""] {
		t.Fatal("a {1}{R} spell with no {X} was offered from {R}: hasXCost matched it")
	}
}

// Captain Eberhart: "Spells cast from among cards you drew this turn cost
// {1} less to cast."
func TestCostFilterCaptainEberhartReducesCardsDrawnThisTurn(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	undrawn := cfHand(e, cfBear, t, 0)
	lib := e.G.AddObject(card(t, cfBear), 0)
	lib.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{lib.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: lib.ID, From: state.ZLibrary, To: state.ZHand})
	if e.G.Obj(lib.ID).Zone != state.ZHand {
		t.Fatalf("precondition: the drawn card is in %s, want hand", e.G.Obj(lib.ID).Zone)
	}
	cfCorpusPerm(t, e, 0, "Captain Eberhart")
	cfPool(e, "G")
	if !cfModes(e, lib.ID)[""] {
		t.Fatal("a {1}{G} card drawn this turn was not offered from {G} with Captain Eberhart in play")
	}
	if cfModes(e, undrawn)[""] {
		t.Fatal("a {1}{G} card NOT drawn this turn was offered from {G}: DrawnThisTurn matched it")
	}
	// A later turn: the draw no longer counts.
	e.G.Turn++
	if cfModes(e, lib.ID)[""] {
		t.Fatal("a card drawn on an earlier turn still read DrawnThisTurn")
	}
}

// Semblance Anvil: "Spells you cast that share a card type with the exiled
// card cost {2} less to cast."
func TestCostFilterSemblanceAnvilReducesSharedCardType(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	inst := cfHand(e, cfInstant, t, 0)
	sorc := cfHand(e, cfSorcery, t, 0)
	imprinted := cfHand(e, cfInstant, t, 0)
	anvil := cfCorpusPerm(t, e, 0, "Semblance Anvil")
	e.emit(events.Event{Kind: events.MoveZone, Obj: imprinted, From: state.ZHand, To: state.ZExile, IDs: []state.ObjID{anvil}})
	e.emit(events.Event{Kind: events.Imprint, Obj: anvil, IDs: []state.ObjID{imprinted}})
	cfPool(e, "U")
	if !cfModes(e, inst)[""] {
		t.Fatal("a {2}{U} instant was not offered from {U} with an instant imprinted on Semblance Anvil")
	}
	if cfModes(e, sorc)[""] {
		t.Fatal("a {2}{U} sorcery was offered from {U}: sharesCardTypeWith Imprinted matched a type the exiled card lacks")
	}
}

const cfAdventurer = "Name:Test Adventurer\nManaCost:2 R\nTypes:Creature Giant\nPT:3/3\nAlternateMode:Adventure\nOracle:x\nALTERNATE\nName:Test Stomp\nManaCost:1 R\nTypes:Instant Adventure\nA:SP$ Draw | NumCards$ 1\nOracle:x\n"

// Beluna Grandsquall: "Permanent spells you cast that have an Adventure cost
// {1} less to cast." A permanent SPELL is priced in hand and on the stack,
// never on the battlefield, so the Permanent base must read the card's type.
func TestCostFilterBelunaReducesPermanentAdventureSpells(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	adv := cfHand(e, cfAdventurer, t, 0)
	plain := cfHand(e, cfBigBear, t, 0)
	cfCorpusPerm(t, e, 0, "Beluna Grandsquall")
	cfPool(e, "RR")
	if !cfModes(e, adv)[""] {
		t.Fatalf("the {2}{R} creature half of an Adventure card was not offered from {R}{R} with Beluna in play: %v", cfModes(e, adv))
	}
	cfPool(e, "R")
	if cfModes(e, adv)["adventure_alt"] {
		t.Fatal("the {1}{R} instant Adventure half was offered from {R}: a non-permanent spell was reduced")
	}
	cfPool(e, "GG")
	if cfModes(e, plain)[""] {
		t.Fatal("a {2}{G} creature with no Adventure was offered from {G}{G}")
	}
	cfPool(e, "RR")
	cfCast(t, e, adv, "")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the reduced cast = %d, want 0 (charged {1}{R})", left)
	}
}

// ---- MayPlaySource ---------------------------------------------------------

const cfExileBolt = "Name:Test Arc\nManaCost:2 R\nTypes:Sorcery\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"

// Urianger Augurelt: "Play Arcanum -- {T}: Until end of turn, you may play
// cards exiled with Urianger Augurelt. Spells you cast this way cost {2}
// less to cast." The may-play cast of the exiled {2}{R} card costs {R}, both
// at the offer and at the post-target reprice; the same card cast from hand
// is not reduced.
func TestCostFilterUriangerReducesSpellsCastThroughItsPermission(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	handCopy := cfHand(e, cfExileBolt, t, 0)
	ur := cfCorpusPerm(t, e, 0, "Urianger Augurelt")
	e.G.Obj(ur).SummonSick = false
	lib := e.G.AddObject(card(t, cfExileBolt), 0)
	lib.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{lib.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: lib.ID, From: state.ZLibrary, To: state.ZExile, IDs: []state.ObjID{ur}})
	if o := e.G.Obj(lib.ID); o.Zone != state.ZExile || o.ExiledWith != ur {
		t.Fatalf("precondition: exiled card zone=%s exiledWith=%d, want exile with Urianger %d", o.Zone, o.ExiledWith, ur)
	}
	// Activate Play Arcanum (the Effect ability; Draw Arcanum is the Dig).
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == ur && strings.Contains(o.Label, "Play Arcanum") {
			idx = o.Index
		}
	}
	if idx < 0 {
		for _, o := range d.Options {
			if o.Obj == ur && o.Kind != "cast" && strings.Contains(strings.ToLower(o.Label), "you may play") {
				idx = o.Index
			}
		}
	}
	if idx < 0 {
		t.Fatalf("no Play Arcanum activation offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	e.resolveTop()
	cfPool(e, "R")
	if !cfModes(e, lib.ID)["mayplay"] {
		t.Fatalf("the exiled {2}{R} card was not offered through Urianger's permission from {R}: %v", cfModes(e, lib.ID))
	}
	if cfModes(e, handCopy)[""] {
		t.Fatal("the same {2}{R} card cast from HAND was offered from {R}: Spell.MayPlaySource reduced a cast that rides no permission")
	}
	cfCast(t, e, lib.ID, "mayplay")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the may-play cast = %d, want 0 (charged {R} after the target reprice)", left)
	}
}

// cfMayPlayEffect is a freely-authored Effect carrier for the card-level
// CastSa form: its Play ability exiles nothing itself -- the test exiles a
// card with it -- and grants "you may play cards exiled with this" plus a
// {1} raise on spells cast that way.
const cfMayPlayEffect = "Name:Test Warden\nManaCost:1 W\nTypes:Creature Human\nPT:1/1\n" +
	"A:AB$ Effect | Cost$ T | StaticAbilities$ SMayPlay,SRaise | SpellDescription$ Play.\n" +
	"SVar:SMayPlay:Mode$ Continuous | MayPlay$ True | Affected$ Card.ExiledWithEffectSource | AffectedZone$ Exile | Description$ play\n" +
	"SVar:SRaise:Mode$ RaiseCost | ValidCard$ Card.CastSa Spell.MayPlaySource | Type$ Spell | Amount$ 1 | Description$ raise\n" +
	"Oracle:x\n"

// The card-level CastSa Spell.MayPlaySource raise (the Elite Spellbinder /
// Soul Partition shape): a spell cast through the Effect's own permission
// costs {1} more, at the offer and at the charge; the same card cast from
// hand does not.
func TestCostFilterCastSaMayPlaySourceRaisesOnlyPermissionCasts(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	handCopy := cfHand(e, cfExileBolt, t, 0)
	w := e.G.AddObject(card(t, cfMayPlayEffect), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: w.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.G.Obj(w.ID).SummonSick = false
	lib := e.G.AddObject(card(t, cfExileBolt), 0)
	lib.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{lib.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: lib.ID, From: state.ZLibrary, To: state.ZExile, IDs: []state.ObjID{w.ID}})
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == w.ID && o.Kind != "cast" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Test Warden activation offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	e.resolveTop()
	cfPool(e, "RCC")
	if cfModes(e, lib.ID)["mayplay"] {
		t.Fatal("the exiled {2}{R} card was offered through the permission from three mana: the {1} raise was not applied")
	}
	if !cfModes(e, handCopy)[""] {
		t.Fatal("the {2}{R} card cast from HAND was not offered from three mana: the raise reached a cast that rides no permission")
	}
	cfPool(e, "RCCC")
	if !cfModes(e, lib.ID)["mayplay"] {
		t.Fatalf("the exiled card was not offered through the permission from four mana: %v", cfModes(e, lib.ID))
	}
	cfCast(t, e, lib.ID, "mayplay")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the raised may-play cast = %d, want 0 (charged {3}{R} after the target reprice)", left)
	}
}

// cfPartition is a freely-authored carrier of the Soul Partition / Elite
// Spellbinder shape WITHOUT the MayPlayPlayer$ CardOwner rider this build
// does not register: it exiles a card from your graveyard, remembering it on
// an Effect that grants "you may play it" and raises "a spell cast this way"
// by {1} through `Card.IsRemembered+CastSa Spell.MayPlaySource`. The source's
// own memory is cleared (DBCleanup), so IsRemembered must read the Effect's
// captured set.
const cfPartition = "Name:Test Partition\nManaCost:1 W\nTypes:Creature Human\nPT:1/1\n" +
	"A:AB$ ChangeZone | Cost$ T | Origin$ Graveyard | Destination$ Exile | ValidTgts$ Card.YouOwn | TgtZone$ Graveyard | TgtPrompt$ x | RememberChanged$ True | SubAbility$ DBEffect | SpellDescription$ Exile.\n" +
	"SVar:DBEffect:DB$ Effect | Duration$ Permanent | StaticAbilities$ MayPlay,CostsMore | RememberObjects$ Remembered | ForgetOnMoved$ Exile | SubAbility$ DBCleanup\n" +
	"SVar:MayPlay:Mode$ Continuous | Affected$ Card.IsRemembered | AffectedZone$ Exile | MayPlay$ True\n" +
	"SVar:CostsMore:Mode$ RaiseCost | ValidCard$ Card.IsRemembered+CastSa Spell.MayPlaySource | Type$ Spell | Amount$ 1\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearRemembered$ True\n" +
	"Oracle:x\n"

// The Soul Partition shape end to end: the remembered card cast through the
// Effect's permission costs {1} more at the offer and at the charge.
func TestCostFilterCastSaMayPlaySourceRememberedEffectRaise(t *testing.T) {
	t.Parallel()
	e := handEngine(t)
	w := e.G.AddObject(card(t, cfPartition), 0)
	e.emit(events.Event{Kind: events.MoveZone, Obj: w.ID, From: state.ZLibrary, To: state.ZBattlefield})
	e.G.Obj(w.ID).SummonSick = false
	gy := e.G.AddObject(card(t, cfExileBolt), 0)
	gy.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, append([]state.ObjID{gy.ID}, e.G.Zone(state.ZLibrary, 0)...))
	e.emit(events.Event{Kind: events.MoveZone, Obj: gy.ID, From: state.ZLibrary, To: state.ZGraveyard})
	e.priorityRound()
	d := e.Pending()
	idx := -1
	for _, o := range d.Options {
		if o.Obj == w.ID && o.Kind != "cast" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no Test Partition activation offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	for i := 0; i < 4; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			break
		}
		pick := d.Options[0].Index
		for _, o := range d.Options {
			if o.Obj == gy.ID {
				pick = o.Index
			}
		}
		submitChoices(t, e, pick)
	}
	e.resolveTop()
	if z := e.G.Obj(gy.ID).Zone; z != state.ZExile {
		t.Fatalf("precondition: the graveyard card is in %s after the exile ability, want exile", z)
	}
	cfPool(e, "RCC")
	if cfModes(e, gy.ID)["mayplay"] {
		t.Fatal("the remembered {2}{R} card was offered through the permission from three mana: the {1} raise was not applied")
	}
	cfPool(e, "RCCC")
	if !cfModes(e, gy.ID)["mayplay"] {
		t.Fatalf("the remembered card was not offered through the permission from four mana: %v", cfModes(e, gy.ID))
	}
	cfCast(t, e, gy.ID, "mayplay")
	if left := e.G.Players[0].Pool.Total(); left != 0 {
		t.Fatalf("pool after the raised may-play cast = %d, want 0 (charged {3}{R})", left)
	}
}
