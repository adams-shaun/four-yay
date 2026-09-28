package rules

// One test per card for the targeting/type-choice/zone-pump misc census
// batch (task inbox-paramcensus-targeting-and-type-choice-misc). The census
// ratchet's five cleared entries each get a test proving the param's actual
// behaviour end to end:
//
//	Angelic Skirmisher          Pump.KWChoice            -- a mid-resolution
//	                             KModes keyword ask, the answer granted
//	                             until EOT
//	Cavern of Souls             Mana.AddsNoCounter       -- mana produced by
//	                             the ability marks the spell it pays for
//	                             can't-be-countered
//	Planetary Annihilation      ChooseCard.Reveal        -- each answered
//	                             choice is revealed to every seat
//	Snapcaster Mage             Pump.PumpZone            -- the graveyard
//	                             grant is a real AffectedZone-scoped
//	                             Flashback until EOT
//	Steel Leaf Champion         CantBlockBy.ValidAttacker -- the static
//	                             scopes its blocker restriction to its own
//	                             attacker
//
// The brief's two other cards (Adaptive Automaton's ChooseType.Type, Delver
// of Secrets' PeekAndReveal.PeekAmount) turned out to be already-read at
// current main -- the census never listed them -- but their tests still pin
// the behaviours, and Adaptive Automaton's test exposed and now covers the
// adjacent AddType$ ChosenType resolution (rules/layers.go).
//
// Deliberately inline fixtures, never corpus .txt (the licensing rule), and
// deliberately only the targeted runs the brief's test budget names.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// ---------------------------------------------------------------------------
// Steel Leaf Champion: CantBlockBy.ValidAttacker
// ---------------------------------------------------------------------------

// TestSteelLeafChampionCantBeBlockedBySmallPower pins the corpus's own
// CantBlockBy grammar: the static's ValidAttacker$ scopes WHICH attacker the
// restriction restricts (Self = itself), so only small-power blockers are
// barred from IT, and a vanilla attacker of the same shape is blockable as
// ever. The historical ValidCard$ spelling stays as the fallback
// (TestCantBlockByRemovesOnlyMatchingPairs pins it), and the blocker-side
// shape (ValidBlocker$ Creature.Self, "can block only creatures with
// flying") is pinned too, since the same read serves it.
func TestSteelLeafChampionCantBeBlockedBySmallPower(t *testing.T) {
	t.Parallel()
	leafCard := card(t, "Name:Steel Leaf Champion\nManaCost:G G G\nTypes:Creature Elf Knight\nPT:5/4\n"+
		"S:Mode$ CantBlockBy | ValidAttacker$ Creature.Self | ValidBlocker$ Creature.powerLE2\nOracle:x\n")
	vanillaCard := card(t, "Name:Vanilla\nManaCost:G G G\nTypes:Creature Elf Knight\nPT:5/4\nOracle:x\n")
	smallCard := card(t, "Name:Small\nManaCost:W\nTypes:Creature Human\nPT:1/1\nOracle:x\n")
	bigCard := card(t, "Name:Big\nManaCost:2 W\nTypes:Creature Human\nPT:3/3\nOracle:x\n")

	e := handEngine(t)
	small := e.G.AddObject(smallCard, 0)
	small.Zone = state.ZBattlefield
	big := e.G.AddObject(bigCard, 0)
	big.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{small.ID, big.ID})
	leaf := e.G.AddObject(leafCard, 1)
	leaf.Zone = state.ZBattlefield
	vanilla := e.G.AddObject(vanillaCard, 1)
	vanilla.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{leaf.ID, vanilla.ID})

	if !e.blockRestricted(small.ID, leaf.ID) {
		t.Fatal("the 1/1 should be barred from blocking Steel Leaf Champion")
	}
	if e.blockRestricted(big.ID, leaf.ID) {
		t.Fatal("a power-3 creature should still be able to block Steel Leaf Champion")
	}
	if e.blockRestricted(small.ID, vanilla.ID) {
		t.Fatal("ValidAttacker$ Creature.Self must scope the restriction to the static's own attacker")
	}

	// The blocker-side shape: the static sits on the blocker and bars it from
	// blocking attackers matching ValidAttacker$.
	onlyFlying := card(t, "Name:Sentinel\nManaCost:1 W\nTypes:Creature Human\nPT:1/4\n"+
		"S:Mode$ CantBlockBy | ValidAttacker$ Creature.withoutFlying | ValidBlocker$ Creature.Self\nOracle:x\n")
	flyer := card(t, "Name:Flyer\nManaCost:1 U\nTypes:Creature Bird\nPT:2/1\nK:Flying\nOracle:x\n")
	sentinel := e.G.AddObject(onlyFlying, 0)
	sentinel.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{small.ID, big.ID, sentinel.ID})
	f := e.G.AddObject(flyer, 1)
	f.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{leaf.ID, vanilla.ID, f.ID})

	if !e.blockRestricted(sentinel.ID, vanilla.ID) {
		t.Fatal("the flying-only sentinel must be barred from blocking a ground attacker")
	}
	if e.blockRestricted(sentinel.ID, f.ID) {
		t.Fatal("the flying-only sentinel may block a flyer")
	}
}

// ---------------------------------------------------------------------------
// Angelic Skirmisher: Pump.KWChoice
// ---------------------------------------------------------------------------

// TestAngelicSkirmisherChoosesCombatKeyword drives the combat-begin trigger
// end to end: the KModes ask offers the three script candidates, the answer
// (Vigilance) is recorded as ModeChosen, every creature the angel's
// controller gains it until end of turn (layer-6 grant), the unchosen
// candidates stay off, and end-of-turn cleanup drops the grant.
func TestAngelicSkirmisherChoosesCombatKeyword(t *testing.T) {
	t.Parallel()
	angelic := "Name:Angelic Skirmisher\nManaCost:4 W W\nTypes:Creature Angel\nPT:4/4\nK:Flying\n" +
		"T:Mode$ Phase | Phase$ BeginCombat | TriggerZones$ Battlefield | Execute$ TrigChoose\n" +
		"SVar:TrigChoose:DB$ Pump | Defined$ Valid Creature.YouCtrl | KWChoice$ First Strike,Vigilance,Lifelink\n" +
		"Oracle:x\n"
	bear := "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 6201, angelic, bear)
	putCreature(t, e, 0, bear)
	putCreature(t, e, 0, angelic)

	// Drive the combat-begin transition the way delverFixture drives the
	// upkeep one: a manual StepChange into the registered phase, then the
	// trigger drain and the resolution, which suspends on the modes ask.
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepBeginCombat})
	e.putTriggersOnStack()
	if len(e.G.Stack) != 1 {
		t.Fatalf("stack = %v, want exactly the Angelic combat trigger", e.G.Stack)
	}
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "modes" || len(d.Options) != 3 {
		t.Fatalf("pending = %+v, want the KWChoice KModes ask over three candidates", d)
	}
	vigilance := -1
	for _, o := range d.Options {
		if o.Label == "Vigilance" {
			vigilance = o.Index
		}
	}
	if vigilance < 0 {
		t.Fatalf("Vigilance not offered: %+v", d.Options)
	}
	submitChoices(t, e, vigilance)

	bearID := findByName(e, "Bear", 0)
	angelID := findByName(e, "Angelic Skirmisher", 0)
	if !e.HasKeyword(bearID, "Vigilance") || !e.HasKeyword(angelID, "Vigilance") {
		t.Fatalf("creatures you control did not gain Vigilance: bear=%v angel=%v",
			e.Derived(bearID).Keywords, e.Derived(angelID).Keywords)
	}
	if e.HasKeyword(bearID, "First Strike") || e.HasKeyword(bearID, "Lifelink") {
		t.Fatal("an unchosen candidate was granted anyway")
	}
	modes := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.ModeChosen && strings.Contains(ev.Text, "Vigilance") {
			modes = true
		}
	}
	if !modes {
		t.Fatal("the answered keyword choice was not recorded as ModeChosen")
	}

	// Until end of turn: the grant is dropped by end-of-turn cleanup.
	e.EndOfTurnCleanup()
	if e.HasKeyword(bearID, "Vigilance") {
		t.Fatal("the combat keyword grant outlived the turn")
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// Planetary Annihilation: ChooseCard.Reveal
// ---------------------------------------------------------------------------

// etbSeed returns the first seed at or after want whose toss starts seat 0
// for EXACTLY the decks etbConfig builds from s0/s1 (etbConfig has no
// seatZeroStart of its own; the toss draw precedes the deck shuffles, so the
// parity is deck-independent, but the helper recomputes the real decks to
// stay honest).
func etbSeed(t *testing.T, want uint64, s0, s1 []string) uint64 {
	t.Helper()
	build := func(srcs []string) []*cards.Card {
		out := make([]*cards.Card, 0, 40)
		for _, s := range srcs {
			out = append(out, card(t, s))
		}
		return append(out, mountainDeck(t, 40-len(out))...)
	}
	for seed := want; ; seed++ {
		if e := New(Config{Seed: seed, Names: []string{"a", "b"},
			Decks:  [][]*cards.Card{build(s0), build(s1)},
			Tokens: map[string]*cards.Card{}}); e.G.Active == 0 {
			return seed
		}
	}
}

// TestPlanetaryAnnihilationRevealsKeptLands casts the real shape end to end:
// each player's answered land choice is followed by one public ids-Note (the
// effReveal reveal payload: empty Text, view.Describe renders it), emitted
// BEFORE the next chooser picks, and the non-chosen lands are then
// sacrificed by the chained SacrificeAll.
func TestPlanetaryAnnihilationRevealsKeptLands(t *testing.T) {
	t.Parallel()
	pa := "Name:Planetary Annihilation\nManaCost:3 R R\nTypes:Sorcery\n" +
		"A:SP$ ChooseCard | Defined$ Player | Choices$ Land | ControlledByPlayer$ Chooser | Amount$ 6 | Mandatory$ True | Reveal$ True | SubAbility$ DBSac\n" +
		"SVar:DBSac:DB$ SacrificeAll | ValidCards$ Land.nonChosenCard | SubAbility$ DBDamageAll\n" +
		"SVar:DBDamageAll:DB$ DamageAll | ValidCards$ Creature | NumDmg$ 6\nOracle:x\n"
	land := "Name:Plains\nManaCost:no cost\nTypes:Basic Land Plains\nOracle:x\n"
	e, cfg, _ := etbConfig(t, etbSeed(t, 6202, []string{pa, land, land}, []string{land, land}), []string{pa, land, land}, []string{land, land})
	l0a, l0b := findByName(e, "Plains", 0), find2ByName(t, e, "Plains", 0)
	l1a, l1b := findByName(e, "Plains", 1), find2ByName(t, e, "Plains", 1)
	putCreature(t, e, 0, land)
	putCreature(t, e, 0, land)
	putCreature(t, e, 1, land)
	putCreature(t, e, 1, land)
	addMana(t, e, 0, "RRRRR")
	castFirst(t, e, "cast")
	passPriority(t, e)
	passPriority(t, e)

	// Seat 0 chooses its two battlefield lands (Amount$ 6 clamps to the pool).
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" || d.Min != 2 || d.Max != 2 {
		t.Fatalf("first choice = %+v, want the clamped two-land keep ask", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	reveals := revealNotes(e)
	if len(reveals) != 1 || reveals[0].Player != 0 || !sameIDs(reveals[0].IDs, []state.ObjID{l0a, l0b}) {
		t.Fatalf("after seat 0's answer the log = %+v, want one public reveal of seat 0's lands", revealNotes(e))
	}

	// Seat 1's ask comes next, and its answer is revealed too.
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "choice" || d.Player != 1 {
		t.Fatalf("second choice = %+v, want seat 1's keep ask", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	reveals = revealNotes(e)
	if len(reveals) != 2 || reveals[1].Player != 1 || !sameIDs(reveals[1].IDs, []state.ObjID{l1a, l1b}) {
		t.Fatalf("after seat 1's answer the log = %+v, want one public reveal of seat 1's lands", revealNotes(e))
	}
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(findByName(e, "Planetary Annihilation", 0)); o.Zone != state.ZGraveyard {
		t.Fatalf("spell went to %s, want graveyard", o.Zone)
	}
	if e.G.Obj(l0a).Zone != state.ZBattlefield || e.G.Obj(l1a).Zone != state.ZBattlefield {
		t.Fatal("the chosen keeps were sacrificed instead of kept")
	}
	replayCheck(t, e, cfg)
}

func sameIDs(a, b []state.ObjID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// find2ByName returns the SECOND object named name owned by p (the fixture
// lands share a name; findByName returns the first).
func find2ByName(t *testing.T, e *Engine, name string, p state.PlayerID) state.ObjID {
	t.Helper()
	n := 0
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.Owner != p {
			continue
		}
		if o.Face() != nil && o.Face().Name == name {
			n++
			if n == 2 {
				return o.ID
			}
		}
	}
	t.Fatalf("no second %q for seat %d", name, p)
	return 0
}

// ---------------------------------------------------------------------------
// Snapcaster Mage: Pump.PumpZone
// ---------------------------------------------------------------------------

// TestSnapcasterMageGrantsGraveyardFlashback drives the ETB trigger end to
// end: the target ask picks a graveyard instant, and after resolution the
// card in the GRAVEYARD carries the granted Flashback (a real AffectedZone-
// scoped layer-6 grant), the flashback cast option is offered at its mana
// cost, and the cast resolves into exile (CR 702.34a).
func TestSnapcasterMageGrantsGraveyardFlashback(t *testing.T) {
	t.Parallel()
	snap := "Name:Snapcaster\nManaCost:1 U\nTypes:Creature Human Wizard\nPT:2/1\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigFlashback\n" +
		"SVar:TrigFlashback:DB$ Pump | ValidTgts$ Instant.YouCtrl,Sorcery.YouCtrl | TgtZone$ Graveyard | TgtPrompt$ Select target instant or sorcery card | KW$ Flashback | PumpZone$ Graveyard\nOracle:x\n"
	inst := "Name:Pyro\nManaCost:1 R\nTypes:Instant\nA:SP$ DealDamage | ValidTgts$ Any | NumDmg$ 1\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 6203, snap, inst)
	addToGraveyard(t, e, 0, inst)
	putCreature(t, e, 0, snap)
	e.Advance()

	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Options[0].Obj == 0 {
		t.Fatalf("pending = %+v, want the ETB target ask over the graveyard instant", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	pyro := findByName(e, "Pyro", 0)
	if e.G.Obj(pyro).Zone != state.ZGraveyard || !e.HasKeyword(pyro, "Flashback") {
		t.Fatalf("graveyard card: zone=%s flashack=%v", e.G.Obj(pyro).Zone, e.Derived(pyro).Keywords)
	}
	replayCheck(t, e, cfg)

	// The granted flashback is castable at the card's own mana cost and the
	// cast exiles the spell (CR 702.34b).
	addMana(t, e, 0, "UR")
	var fb *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Mode == "flashback" && o.Obj == pyro {
			fb = &o
		}
	}
	if fb == nil {
		t.Fatalf("flashback not offered: %+v", castOptions(t, e))
	}
	submitChoices(t, e, fb.Index)
	// The flashbacked spell's own SP target ask comes next (Pyro deals
	// damage), then the stack drains through the cast.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("after the flashback cast: %+v, want Pyro's target ask", d)
	}
	submitChoices(t, e, 0)
	passUntilStackEmpty(t, e, 20)
	if e.G.Obj(pyro).Zone != state.ZExile {
		t.Fatalf("flashbacked spell went to %s, want exile", e.G.Obj(pyro).Zone)
	}
}

// ---------------------------------------------------------------------------
// Cavern of Souls: Mana.AddsNoCounter
// ---------------------------------------------------------------------------

// TestCavernOfSoulsChosenCreatureSpellCantBeCountered plays the land, picks
// the type, funds a creature cast from the AddsNoCounter$ ability (its
// restricted colourless mana), and proves the cast: the pay-time CastInfo
// carries the ncount flag, a counterspell on it leaves the spell on the
// stack (one can't-be-countered Note), and the spell resolves. The control
// half casts the same creature on ordinary mana and it counters cleanly.
func TestCavernOfSoulsChosenCreatureSpellCantBeCountered(t *testing.T) {
	t.Parallel()
	cavern := "Name:Cavern\nManaCost:no cost\nTypes:Land\nK:ETBReplacement:Other:ChooseCT\n" +
		"SVar:ChooseCT:DB$ ChooseType | Defined$ You | Type$ Creature\n" +
		"A:AB$ Mana | Cost$ T | Produced$ C\n" +
		"A:AB$ Mana | Cost$ T | Produced$ Any | RestrictValid$ Spell.Creature+ChosenType | AddsNoCounter$ True\nOracle:x\n"
	grunt := "Name:Grunt\nManaCost:1\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"
	counter := "Name:Nullify\nManaCost:U U\nTypes:Instant\nA:SP$ Counter | ValidTgts$ Spell\nOracle:x\n"
	e, cfg, find := etbConfig(t, etbSeed(t, 6204, []string{cavern, grunt}, []string{counter}), []string{cavern, grunt}, []string{counter})
	cv, gruntID := find("Cavern", 0), find("Grunt", 0)

	// Play the land and record the chosen type.
	var play *decision.Option
	for _, o := range e.Pending().Options {
		if o.Kind == "play_land" && o.Obj == cv {
			play = &o
		}
	}
	if play == nil {
		t.Fatalf("no play_land option for the cavern: %+v", e.Pending().Options)
	}
	submitChoices(t, e, play.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || d.Options[0].Kind != "type" {
		t.Fatalf("type choice = %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)

	// Fund the cast from Cavern's AddsNoCounter$ ability, twice at priority
	// (the offer gate is pool-only: the cast is offered only once the pool
	// already pays, so the restricted units are produced BEFORE the cast).
	activateCavernNC := func() {
		submitChoices(t, e, activateOption(t, e, cv))
		d := e.Pending()
		idx := -1
		for _, o := range d.Options {
			if o.Kind == "mana" && o.Ability == 1 {
				idx = o.Index
			}
		}
		if idx < 0 {
			t.Fatalf("the AddsNoCounter$ ability was not offered: %+v", d.Options)
		}
		submitChoices(t, e, idx)
		// Produced$ Any asks its colour (mana_activation's askManaColor); the
		// answer's colour is the restricted batch's own slot.
		d = e.Pending()
		if d == nil || d.Kind != decision.KChoose || len(d.Options) != 5 {
			t.Fatalf("the any-colour ask = %+v", d)
		}
		submitChoices(t, e, 0)
	}
	activateCavernNC()
	castFirst(t, e, "cast")
	if o := e.G.Obj(gruntID); o.Zone != state.ZStack || o.CastFlags&state.FlagNoCounter == 0 {
		t.Fatalf("after payment: grunt %s flags %d, want the stack and FlagNoCounter",
			o.Zone, o.CastFlags)
	}
	ncast := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == gruntID && strings.Contains(ev.Counter, "ncount") {
			ncast = true
		}
	}
	if !ncast {
		t.Fatal("the pay-time CastInfo did not carry the ncount flag")
	}
	if len(e.G.Players[0].RestrictedMana) != 0 || e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("the restricted batch was not consumed: pool=%v restricted=%v",
			e.G.Players[0].Pool, e.G.Players[0].RestrictedMana)
	}

	// The counterspell still targets it, but the removal never happens. The
	// Counter effect is resolved directly (the established cast_test.go
	// pattern — TestFlashbackedSpellCounteredGoesToExile) against the stack
	// object, Controller 1.
	effects.Resolve(e, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: gruntID}}, TargetsOffered: true},
		card(t, counter).Faces[0].SpellAbility())
	passUntilStackEmpty(t, e, 20)
	if o := e.G.Obj(gruntID); o.Zone != state.ZBattlefield {
		t.Fatalf("the protected spell was countered: %s", o.Zone)
	}
	prevented := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "can't be countered" && ev.Obj == gruntID {
			prevented = true
		}
	}
	if !prevented {
		t.Fatal("no can't-be-countered record in the log")
	}
	replayCheck(t, e, cfg)

	// Control: the same creature cast on ordinary mana is countered cleanly.
	e2, cfg2, find2 := etbConfig(t, etbSeed(t, 6205, []string{grunt}, []string{counter}), []string{grunt}, []string{counter})
	g2 := find2("Grunt", 0)
	addMana(t, e2, 0, "C")
	castFirst(t, e2, "cast")
	effects.Resolve(e2, &effects.Ctx{Controller: 1, Targets: []state.Target{{Obj: g2}}, TargetsOffered: true},
		card(t, counter).Faces[0].SpellAbility())
	passUntilStackEmpty(t, e2, 20)
	if o := e2.G.Obj(g2); o.Zone != state.ZGraveyard {
		t.Fatalf("control spell went to %s, want the graveyard (countered)", o.Zone)
	}
	for _, ev := range e2.L.Events {
		if ev.Kind == events.CastInfo && strings.Contains(ev.Counter, "ncount") {
			t.Fatal("ordinary mana produced the ncount flag")
		}
	}
	replayCheck(t, e2, cfg2)
}

// ---------------------------------------------------------------------------
// Adaptive Automaton: ChooseType.Type (+ the AddType$ ChosenType static)
// ---------------------------------------------------------------------------

// TestAdaptiveAutomatonIsTheChosenType casts the automaton with a Goblin on
// the battlefield, answers the as-enters type choice Goblin, and proves both
// statics: the automaton IS the chosen type (the resolved AddType$ grant —
// never a literal "ChosenType" type word), and the other creature you
// control of the chosen type gets +1/+1.
func TestAdaptiveAutomatonIsTheChosenType(t *testing.T) {
	t.Parallel()
	auto := "Name:Adaptive Automaton\nManaCost:3\nTypes:Artifact Creature Construct\nPT:2/2\n" +
		"K:ETBReplacement:Other:ChooseCT\nSVar:ChooseCT:DB$ ChooseType | Type$ Creature\n" +
		"S:Mode$ Continuous | Affected$ Card.Self | AddType$ ChosenType\n" +
		"S:Mode$ Continuous | Affected$ Creature.ChosenType+Other+YouCtrl | AddPower$ 1 | AddToughness$ 1\nOracle:x\n"
	gob := "Name:Scrapper\nManaCost:1 R\nTypes:Creature Goblin\nPT:2/2\nOracle:x\n"
	e, cfg, _ := newFixtureDeck(t, 6206, auto, gob)
	putCreature(t, e, 0, gob)
	addMana(t, e, 0, "CCC")
	castFirst(t, e, "cast")
	d := passUntilNonPriority(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "etb" || d.Options[0].Kind != "type" {
		t.Fatalf("type choice = %+v", d)
	}
	idx := -1
	for _, o := range d.Options {
		if o.Label == "Goblin" {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("Goblin not offered: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	autoID := findByName(e, "Adaptive Automaton", 0)
	gobID := findByName(e, "Scrapper", 0)
	isGoblin := false
	for _, typ := range e.Derived(autoID).Types {
		if typ == "Goblin" {
			isGoblin = true
		}
		if typ == "ChosenType" {
			t.Fatal("the unresolved AddType$ value leaked as a literal type word")
		}
	}
	if !isGoblin {
		t.Fatalf("automaton types = %v, want Goblin among them", e.Derived(autoID).Types)
	}
	if e.Power(gobID) != 3 || e.Toughness(gobID) != 3 {
		t.Fatalf("the chosen-type lord did not apply: %d/%d", e.Power(gobID), e.Toughness(gobID))
	}
	replayCheck(t, e, cfg)
}

// ---------------------------------------------------------------------------
// Delver of Secrets: PeekAndReveal.PeekAmount
// ---------------------------------------------------------------------------

// TestDelverPeekWindowIsExactlyPeekAmount pins the peek WINDOW: with TWO
// cards on top of the library the upkeep peek looks at exactly the top
// PeekAmount$ (1) card — the reveal ask names only the top card, never the
// one under it — and answering yes with an instant on top reveals it and
// transforms Delver (the RememberRevealed$ capture feeding the chained
// SetState).
func TestDelverPeekWindowIsExactlyPeekAmount(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	delver, ok := reg.Lookup("Delver of Secrets")
	if !ok {
		t.Fatal("corpus has no Delver of Secrets")
	}
	bolt, ok := reg.Lookup("Lightning Bolt")
	if !ok {
		t.Fatal("corpus has no Lightning Bolt")
	}
	island, ok := reg.Lookup("Island")
	if !ok {
		t.Fatal("corpus has no Island")
	}
	e := layerEngine(t)
	o := e.G.AddObject(delver, state.PlayerID(0))
	o.Zone = state.ZBattlefield
	e.G.Clock++
	o.Timestamp = e.G.Clock
	e.G.SetZone(state.ZBattlefield, 0, append(e.G.Zone(state.ZBattlefield, 0), o.ID))
	top := e.G.AddObject(bolt, state.PlayerID(0))
	top.Zone = state.ZLibrary
	under := e.G.AddObject(island, state.PlayerID(0))
	under.Zone = state.ZLibrary
	e.G.SetZone(state.ZLibrary, 0, []state.ObjID{top.ID, under.ID})
	e.emit(events.Event{Kind: events.StepChange, Step: state.StepUpkeep})
	e.putTriggersOnStack()
	e.resolveTop()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "reveal_optional" {
		t.Fatalf("pending = %+v, want the peek's reveal_optional ask", d)
	}
	if len(d.Options) != 2 || d.Options[0].Obj != top.ID {
		t.Fatalf("peek window = %+v, want exactly the top card (PeekAmount$ 1)", d.Options)
	}
	if !strings.Contains(d.Prompt, "Lightning Bolt") || strings.Contains(d.Prompt, "Island") {
		t.Fatalf("the peek ask leaked the second library card: %q", d.Prompt)
	}
	submitChoices(t, e, 0)
	if o := e.G.Obj(o.ID); o.Face() == nil || o.Face().Name != "Insectile Aberration" {
		t.Fatalf("the revealed instant did not transform Delver: %+v", o.Face())
	}
}
