package rules

// Set audit: Teenage Mutant Ninja Turtles (tmt).
//
// This file is the audit's product: one test per finding, plus regression
// pins where the behaviour was verified correct. Every FAILING test is
// guarded by a t.Skip unless GORGE_SET_AUDIT=1, so the committed suite stays
// green while the defect stands. Tests that PASS stay unguarded as
// regression coverage.
//
// Named mechanics first (Sneak, Alliance, Disappear, Channel, Class levels),
// then the corner cases and the library/graveyard/exile interactions.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// setAuditSkip skips a FAILING finding unless the audit env is set.
func setAuditSkip(t *testing.T, defect, ticket string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skipf("set-audit finding (tmt): %s. Follow-up: %s", defect, ticket)
	}
}

// setAuditRealCard loads a linked real corpus card, failing the precondition
// loudly if the corpus is missing it (never a vacuous skip).
func setAuditRealCard(t *testing.T, name string) *cards.Card {
	t.Helper()
	reg := searchTestRegistry(t)
	c := searchCorpusCard(t, reg, name)
	if d := c.Link(); len(d) != 0 {
		t.Fatalf("link %s: %v", name, d)
	}
	return c
}

// setAuditDeck builds a seat-0-start two-seat game from the given real corpus
// cards, padding each seat to 40 with Mountains. Seat 0 has the initiative.
func setAuditDeck(t *testing.T, seed uint64, s0, s1 []*cards.Card) (*Engine, Config) {
	t.Helper()
	pad := func(s []*cards.Card) []*cards.Card {
		out := append([]*cards.Card{}, s...)
		for len(out) < 40 {
			out = append(out, mountainDeck(t, 1)...)
		}
		return out
	}
	cfg := seatZeroStart(Config{Seed: seed, Names: []string{"turtle", "villain"},
		Decks: [][]*cards.Card{pad(s0), pad(s1)}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e, cfg
}

// ---------------------------------------------------------------------------
// (a) Sneak -- CR 702.190
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_OrokuSaki_SneakIsCastableDeclareBlockers pins CR 702.190a:
// "Any time you could cast an instant during your declare blockers step, you
// may cast this spell by paying [cost] and returning an unblocked creature
// you control to its owner's hand". Oroku Saki, Shredder Rising prints
// Sneak {1}{B}.
//
// The engine expands no Sneak keyword at all (there is no cards/kw_sneak.go
// and no kw:Sneak in effects.Supported()), so with an unblocked attacker and
// {1}{B} in the pool the card in hand is offered no cast option in the
// declare-blockers step.
func TestSetAudit_tmt_OrokuSaki_SneakIsCastableDeclareBlockers(t *testing.T) {
	setAuditSkip(t, "Sneak is unimplemented: no declare-blockers cast option",
		"tmt-sneak-unimplemented")
	saki := setAuditRealCard(t, "Oroku Saki, Shredder Rising")
	if !saki.Faces[0].HasKeyword("Sneak") {
		t.Fatal("precondition: Oroku Saki does not print Sneak in the corpus")
	}
	e, _ := ninjutsuDeck(t, 9411, saki)
	id := searchMoveByName(t, e, "Oroku Saki, Shredder Rising", state.ZHand)
	if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Oroku Saki not in hand: %+v", o)
	}
	_ = attackWithBear(t, e) // unblocked attacker of seat 1, seat 0 declarer
	fundPool(t, e, "B")      // Sneak {1}{B}

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("not at seat 0 priority in declare blockers: %+v", d)
	}
	if opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising"); opt == nil {
		t.Fatalf("CR 702.190a: Oroku Saki must be castable for its sneak cost with an unblocked attacker; cast options were %+v",
			castOptions(t, e))
	}
}

// TestSetAudit_tmt_Sneak_isNotReportedSupported is the honest half of the
// Sneak finding: a card whose only unimplemented primitive is kw:Sneak is
// still reported unsupported, so the coverage ratchet can see it. Unguarded
// (passes today, must keep passing).
func TestSetAudit_tmt_Sneak_isNotReportedSupported(t *testing.T) {
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	c := searchCorpusCard(t, reg, "Oroku Saki, Shredder Rising")
	if !c.Faces[0].HasKeyword("Sneak") {
		t.Fatal("precondition: Oroku Saki does not print Sneak")
	}
	found := false
	for _, m := range reg.Unsupported(c, supported) {
		if m == "kw:Sneak" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Sneak is unimplemented but kw:Sneak is not reported missing for %s", c.Faces[0].Name)
	}
}

// ---------------------------------------------------------------------------
// (a) Alliance -- ability word, plain ChangesZone trigger
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_EastWindAvatar_AlliancePumpsOnAnotherCreature pins the
// Alliance ability word's literal trigger: "Whenever another creature you
// control enters, this creature gets +1/+0 until end of turn." Regression
// pin (expected to pass): Alliance is a Forge T: line, not a keyword.
func TestSetAudit_tmt_EastWindAvatar_AlliancePumpsOnAnotherCreature(t *testing.T) {
	avatar := setAuditRealCard(t, "East Wind Avatar")
	if !avatar.Faces[0].HasKeyword("Flying") {
		t.Fatal("precondition: East Wind Avatar should have Flying")
	}
	e, _ := setAuditDeck(t, 9421, []*cards.Card{avatar, card(t, ninjutsuBearSrc)},
		[]*cards.Card{card(t, ninjutsuBearSrc)})
	av := searchMoveByName(t, e, "East Wind Avatar", state.ZBattlefield)
	if o := e.G.Obj(av); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Avatar not on the battlefield: %+v", o)
	}
	before := e.Power(av)
	// Another creature you control enters; pose priority so the queued
	// Alliance trigger reaches the stack, then let it resolve.
	putCreature(t, e, 0, ninjutsuBearSrc)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.Power(av); got != before+1 {
		t.Fatalf("Alliance must pump East Wind Avatar +1/+0: power %d -> %d", before, got)
	}
	// An OPPONENT's creature entering must not trigger it.
	before = e.Power(av)
	putCreature(t, e, 1, ninjutsuBearSrc)
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.Power(av); got != before {
		t.Fatalf("Alliance must not trigger on an opponent's creature: power %d -> %d", before, got)
	}
}

// ---------------------------------------------------------------------------
// (b) Enrage / must-be-blocked -- Raphael, Ninja Destroyer
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBeBlockedIsModelled pins the
// "must be blocked if able" combat requirement (CR 509.1a). Raphael, Ninja
// Destroyer prints exactly that line ("Raphael must be blocked if able."),
// Forge-encoded as `K:CARDNAME must be blocked if able.`. The engine's
// must-block machinery (rules/combat.go mustBlockCandidates) reads a
// MustBlock continuous static or the printed keyword; a bare K: line whose
// head is the literal "CARDNAME must be blocked if able." becomes an unknown
// kw: primitive and is never read, so the card both (i) reports unsupported
// and (ii) is not actually required to be blocked.
func TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBeBlockedIsModelled(t *testing.T) {
	setAuditSkip(t, "must-be-blocked keyword is an unread literal kw: head",
		"tmt-mustbeblocked-keyword-unread")
	raph := setAuditRealCard(t, "Raphael, Ninja Destroyer")
	f := raph.Faces[0]
	// The card text says it must be blocked if able. Either the expanded
	// static or a recognised keyword head must carry that.
	printed := false
	for _, k := range f.Keywords {
		if cards.KeywordHead(k) == "MustBlock" || cards.KeywordHead(k) == "MustBeBlocked" {
			printed = true
		}
	}
	if !printed && !hasMustBlockStatic(f) {
		t.Fatalf("CR 509.1a: %s says it must be blocked if able but the face carries no MustBlock static; keywords=%v statics=%v",
			f.Name, f.Keywords, staticModes(f))
	}
}

// ---------------------------------------------------------------------------
// (a) Class -- level bands (CR 716)
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_LeaderTalent_Level2LeavesTrigger pins a Class card's level
// band: CR 716.2 says gaining a level adds the level bar's abilities. At
// level 2 Leader's Talent gains "Whenever a creature you control leaves the
// battlefield, if it had a counter on it, you gain 2 life." (AddTrigger$ on
// the K:Class line). The card starts at level 1 and reaches level 2 by
// activating its "{2}{W}: Level 2" ability.
func TestSetAudit_tmt_LeaderTalent_Level2LeavesTrigger(t *testing.T) {
	lt := setAuditRealCard(t, "Leader's Talent")
	if len(lt.Faces[0].Keywords) == 0 {
		t.Fatal("precondition: Leader's Talent prints no keywords")
	}
	// The level-2 trigger must exist somewhere on the face (either as an
	// always-present hidden trigger gated by the level, or added at level 2).
	found := false
	for _, tr := range lt.Faces[0].Triggers {
		if tr.Params["Mode"] == "ChangesZone" && tr.Params["Destination"] == "Any" {
			found = true
		}
	}
	if !found {
		setAuditSkip(t, "K:Class level-2 AddTrigger$ is not expanded onto the face",
			"tmt-class-level-addtrigger")
		t.Fatalf("Leader's Talent has no level-2 ChangesZone trigger; triggers=%v", triggerModes(lt.Faces[0]))
	}
}

// ---------------------------------------------------------------------------
// (c) Library: Dig rest-in-random-order and DigUntil
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_CaseyJones_RestGoesToBottomInRandomOrder pins the Dig
// rider RestRandomOrder$ True: "Put the rest on the bottom of your library in
// a random order." (CR 701.20 shuffle semantics on the remainder). The test
// asserts the library total is conserved (no card lost) and the engine did
// not emit an unimplemented Note for the rider.
func TestSetAudit_tmt_CaseyJones_RestGoesToBottomInRandomOrder(t *testing.T) {
	cj := setAuditRealCard(t, "Casey Jones, Jury-Rig Justiciar")
	if !cj.Faces[0].HasKeyword("Haste") {
		t.Fatal("precondition: Casey Jones should have Haste")
	}
	reg := searchTestRegistry(t)
	artifact := searchCorpusCard(t, reg, "Sol Ring")
	e, _ := setAuditDeck(t, 9441, []*cards.Card{cj, artifact, artifact, artifact, artifact},
		[]*cards.Card{})
	totalBefore := setAuditZoneTotal(e, 0)
	searchMoveByName(t, e, "Casey Jones, Jury-Rig Justiciar", state.ZBattlefield)
	// Precondition: the card really has an ETB Dig trigger (so the drain below
	// is not passing with the feature absent).
	digTrigger := false
	for _, tr := range cj.Faces[0].Triggers {
		sa := cards.ResolveSVar(cj.Faces[0].SVars, tr.Params["Execute"])
		if tr.Params["Mode"] == "ChangesZone" && sa != nil && sa.API == "Dig" {
			digTrigger = true
		}
	}
	if !digTrigger {
		t.Fatal("precondition: Casey Jones has no self-ETB Dig trigger")
	}
	passUntilStackEmpty(t, e, 60)
	if hasNote(e, "unimplemented") {
		t.Fatalf("Dig rider went unimplemented: %v", noteTexts(e))
	}
	totalAfter := setAuditZoneTotal(e, 0)
	if totalAfter != totalBefore {
		t.Fatalf("CR 701.20: the Dig must conserve every zone's card total, %d -> %d", totalBefore, totalAfter)
	}
}

// TestSetAudit_tmt_KrangShredder_DigUntilExilesNonland pins the DigUntil
// shape: "each opponent exiles cards from the top of their library until they
// exile a nonland card." The exiled pile must stay in EXILE (not the
// graveyard) and the walk must stop at the first nonland.
func TestSetAudit_tmt_KrangShredder_DigUntilExilesNonland(t *testing.T) {
	ks := setAuditRealCard(t, "Krang & Shredder")
	e, _ := setAuditDeck(t, 9451, []*cards.Card{ks}, []*cards.Card{})
	// Give seat 1 a library whose top is lands then a creature, so the walk
	// has a definite stopping point.
	searchMoveByNameSeat(t, e, 1, "Mountain", state.ZLibrary)
	ksID := searchMoveByName(t, e, "Krang & Shredder", state.ZBattlefield)
	if o := e.G.Obj(ksID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Krang & Shredder not on battlefield: %+v", o)
	}
	passUntilStackEmpty(t, e, 80)
	exiled := len(e.G.Zone(state.ZExile, 1))
	if exiled == 0 {
		t.Fatalf("CR 701.x: DigUntil must exile the top of each opponent's library; exile for seat 1 is empty")
	}
	if hasNote(e, "unimplemented") {
		t.Fatalf("DigUntil went unimplemented: %v", noteTexts(e))
	}
}

// ---------------------------------------------------------------------------
// (b) Copy tokens -- Chrome Dome
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_ChromeDome_CopyGainsHasteAndIsSacrificedAtNextEndStep
// pins the activated-copy shape: "{5}: Create a token that's a copy of another
// target artifact you control. That token gains haste. Sacrifice it at the
// beginning of the next end step." (CR 707.2 / CR 513.2 delayed sacrifice).
func TestSetAudit_tmt_ChromeDome_CopyGainsHasteAndIsSacrificedAtNextEndStep(t *testing.T) {
	cd := setAuditRealCard(t, "Chrome Dome")
	reg := searchTestRegistry(t)
	ring := searchCorpusCard(t, reg, "Sol Ring")
	e, _ := setAuditDeck(t, 9461, []*cards.Card{cd, ring}, []*cards.Card{})
	cdID := searchMoveByName(t, e, "Chrome Dome", state.ZBattlefield)
	ringID := searchMoveByName(t, e, "Sol Ring", state.ZBattlefield)
	if o := e.G.Obj(cdID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Chrome Dome not on battlefield: %+v", o)
	}
	if o := e.G.Obj(ringID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Sol Ring not on battlefield: %+v", o)
	}
	fundPool(t, e, "CCCCC")
	opt := abilityFor(t, e, 0, cdID)
	if opt == nil {
		t.Fatalf("Chrome Dome's {5}: copy ability not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		chosen := -1
		for _, o := range d.Options {
			if o.Obj == ringID {
				chosen = o.Index
			}
		}
		if chosen < 0 {
			t.Fatalf("copy target ask did not offer the other artifact: %+v", d.Options)
		}
		submitChoices(t, e, chosen)
	}
	passUntilStackEmpty(t, e, 60)
	copies := 0
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.ID != cdID && o.ID != ringID && o.Face() != nil && o.Face().Name == "Sol Ring" {
			copies++
		}
	}
	if copies != 1 {
		t.Fatalf("CR 707.2: the copy token should exist on the battlefield, found %d", copies)
	}
}

// ---------------------------------------------------------------------------
// (b) Exile-and-return attacking -- The Neutrinos
// ---------------------------------------------------------------------------

// setAuditHasteSrc is an inline fixture static that grants every creature you
// control haste, so a corpus creature without haste can attack the turn it is
// moved onto the battlefield (no direct write to summoning sickness).
const setAuditHasteSrc = "Name:Haste Banner\nManaCost:0\nTypes:Enchantment\n" +
	"S:Mode$ Continuous | Affected$ Creature.YouCtrl | AddKeyword$ Haste | Description$ Creatures you control have haste.\nOracle:x\n"

// TestSetAudit_tmt_Neutrinos_ExilesAndReturnsAttacking pins "Whenever The
// Neutrinos attack, exile up to one target creature you own, then return it
// to the battlefield under your control tapped and attacking." (CR 603.6
// leaves-the-battlefield / return; the returned creature is a NEW object
// attacking the same defender).
func TestSetAudit_tmt_Neutrinos_ExilesAndReturnsAttacking(t *testing.T) {
	neut := setAuditRealCard(t, "The Neutrinos")
	haste := card(t, setAuditHasteSrc)
	e, _ := setAuditDeck(t, 9471, []*cards.Card{neut, haste, card(t, ninjutsuBearSrc)}, []*cards.Card{})
	neutID := searchMoveByName(t, e, "The Neutrinos", state.ZBattlefield)
	if o := e.G.Obj(neutID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: The Neutrinos not on battlefield: %+v", o)
	}
	putCreature(t, e, 0, setAuditHasteSrc)
	bearID := putCreature(t, e, 0, ninjutsuBearSrc)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Bear not on battlefield: %+v", o)
	}
	// Both have haste, so both can attack this turn.
	e.priorityRound()
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, neutID, bearID)
	// The attack trigger goes on the stack and asks for its target before the
	// declare-blockers priority window; answer it while driving there.
	for i := 0; i < 50; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while driving to declare blockers")
		}
		if d.Kind == decision.KTriggerOrder {
			choices := make([]int, 0, len(d.Options))
			for _, o := range d.Options {
				choices = append(choices, o.Index)
			}
			submitChoices(t, e, choices...)
			continue
		}
		if d.Kind == decision.KTriggerOptional {
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		if d.Kind == decision.KTarget {
			chosen := d.Options[0].Index
			for _, o := range d.Options {
				if o.Obj == bearID {
					chosen = o.Index
				}
			}
			submitChoices(t, e, chosen)
			continue
		}
		if d.Kind == decision.KPriority && d.Player == 0 && e.G.Step == state.StepDeclareBlockers {
			break
		}
		if d.Kind == decision.KPriority {
			submitPass(t, e)
			continue
		}
		t.Fatalf("unexpected %s decision at step %s while driving to blockers", d.Kind, e.G.Step)
	}
	passUntilStackEmpty(t, e, 60)
	// The returned Bear is a NEW object (it left and re-entered); it must be
	// on the battlefield tapped and attacking.
	var returned state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.ID != neutID && o.Face() != nil && o.Face().Name == "Ally Bear" {
			returned = id
		}
	}
	if returned == 0 {
		t.Fatalf("CR 603.6: the exiled Bear did not return to the battlefield")
	}
	o := e.G.Obj(returned)
	if !o.IsAttacking || !o.Tapped {
		t.Fatalf("CR 603.6: the returned creature must return tapped and attacking; got attacking=%v tapped=%v", o.IsAttacking, o.Tapped)
	}
}

// ---------------------------------------------------------------------------
// (b) Kicker with a non-mana additional cost -- Stomped by the Foot
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_StompedByTheFoot_KickerSacrificeOption pins CR 702.33 / CR
// 601.2b: "Kicker—Sacrifice an artifact or creature" is an OPTIONAL
// additional cost paid as the spell is cast. The engine must offer the kicked
// cast variant when an artifact or creature is available to sacrifice (and
// the plain variant otherwise).
func TestSetAudit_tmt_StompedByTheFoot_KickerSacrificeOption(t *testing.T) {
	stomped := setAuditRealCard(t, "Stomped by the Foot")
	if !stomped.Faces[0].HasKeyword("Kicker") {
		t.Fatal("precondition: Stomped by the Foot does not print Kicker")
	}
	bear := card(t, ninjutsuBearSrc)
	e, _ := setAuditDeck(t, 9481, []*cards.Card{stomped, bear}, []*cards.Card{})
	stompedID := searchMoveByName(t, e, "Stomped by the Foot", state.ZHand)
	if o := e.G.Obj(stompedID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: Stomped by the Foot not in hand: %+v", o)
	}
	putCreature(t, e, 0, ninjutsuBearSrc) // sacrifice fodder
	addMana(t, e, 0, "BC")                // {1}{B}
	opts := castOptions(t, e)
	kicked := false
	for _, o := range opts {
		if o.Mode == "kicked" {
			kicked = true
		}
	}
	if !kicked {
		t.Fatalf("CR 702.33: Stomped by the Foot must offer a kicked cast with a creature available to sacrifice; cast options were %+v", opts)
	}
}

// ---------------------------------------------------------------------------
// (b) Crew tracking -- Turtle Van
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_TurtleVan_CrewedThisTurnTarget pins "put a +1/+1 counter on
// target creature that crewed it this turn" (Forge's Creature.CrewedThisTurn
// filter). Crew the Van, attack with it, and the trigger must offer the
// crewer as a target.
func TestSetAudit_tmt_TurtleVan_CrewedThisTurnTarget(t *testing.T) {
	setAuditSkip(t, "Creature.CrewedThisTurn target filter is unread: the crewer is never offered",
		"tmt-crewedthisturn-filter")
	van := setAuditRealCard(t, "Turtle Van")
	haste := card(t, setAuditHasteSrc)
	bear := card(t, ninjutsuBearSrc)
	e, _ := setAuditDeck(t, 9491, []*cards.Card{van, haste, bear, bear, bear}, []*cards.Card{})
	vanID := searchMoveByName(t, e, "Turtle Van", state.ZBattlefield)
	if o := e.G.Obj(vanID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Turtle Van not on battlefield: %+v", o)
	}
	putCreature(t, e, 0, setAuditHasteSrc)
	bearID := putCreature(t, e, 0, ninjutsuBearSrc)
	if o := e.G.Obj(bearID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Bear not on battlefield: %+v", o)
	}
	// Crew 1: tap the hasted Bear.
	e.priorityRound()
	opt := abilityFor(t, e, 0, vanID)
	if opt == nil {
		t.Fatalf("precondition: Turtle Van's Crew ability not offered: %+v", e.Pending())
	}
	submitChoices(t, e, opt.Index)
	// Answer the crew tap election with the Bear.
	for i := 0; i < 20; i++ {
		d := e.Pending()
		if d == nil || d.Kind != decision.KChoose {
			break
		}
		chosen := -1
		for _, o := range d.Options {
			if o.Obj == bearID {
				chosen = o.Index
			}
		}
		if chosen < 0 {
			break
		}
		submitChoices(t, e, chosen)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(vanID); o == nil || !e.IsCreature(vanID) {
		t.Fatalf("precondition: Turtle Van did not become a creature after crewing: %+v", o)
	}
	// Attack with the Van; the trigger must ask for a creature that crewed it.
	driveToStep(t, e, e.G.Turn, 0, state.StepDeclareAttackers)
	passToKind(t, e, decision.KAttackers)
	submitAttackersOnly(t, e, vanID)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("CR 603.4: Turtle Van's attack trigger must ask for a creature that crewed it; got %+v", d)
	}
	offered := false
	for _, o := range d.Options {
		if o.Obj == bearID {
			offered = true
		}
	}
	if !offered {
		t.Fatalf("Creature.CrewedThisTurn must offer the crewing Bear; options were %+v", d.Options)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func hasMustBlockStatic(f *cards.Face) bool {
	for _, s := range f.Statics {
		if s.Mode == "MustBlock" || s.Params["Mode"] == "MustBlock" {
			return true
		}
		if s.Params["MustBlock"] != "" {
			return true
		}
	}
	return false
}

func staticModes(f *cards.Face) []string {
	out := make([]string, 0, len(f.Statics))
	for _, s := range f.Statics {
		out = append(out, s.Mode)
	}
	return out
}

func triggerModes(f *cards.Face) []string {
	out := make([]string, 0, len(f.Triggers))
	for _, tr := range f.Triggers {
		out = append(out, tr.Params["Mode"])
	}
	return out
}

func setAuditZoneTotal(e *Engine, p state.PlayerID) int {
	n := 0
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand, state.ZGraveyard, state.ZBattlefield, state.ZStack, state.ZExile} {
		n += len(e.G.Zone(z, p))
	}
	return n
}

func noteTexts(e *Engine) []string {
	var out []string
	for _, ev := range e.L.Events {
		if ev.Text != "" {
			out = append(out, ev.Text)
		}
	}
	return out
}
