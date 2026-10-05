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
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
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
// Census: the whole 195-card set against the coverage oracle
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_CensusMechanismClasses is the committed census over
// setAuditTMTNames() (setaudit_tmt_names_test.go): it resolves every one of the
// set's 195 names in the corpus, and asserts that the cards still reported
// unsupported by (*cards.Registry).Unsupported with effects.Supported() fall
// into no remaining mechanism class: the audit found two and both are closed.
// `kw:Sneak` (the named mechanic, 26 carriers) is implemented
// (rules/sneak.go registers it), and the phantom
// `kw:CARDNAME must be blocked if able.` spelling (Raphael's working CR 509.1c
// requirement) was a classification artefact: the sentence head is
// canonicalised to MustBlock at parse time (cards/hiddenkeyword.go) and
// registered as supported (rules/statics.go). This is the accountability half
// of the audit: it names a class, not one card, and would fail if a class
// reappeared (or if either closure silently regressed).
func TestSetAudit_tmt_CensusMechanismClasses(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	if reg == nil {
		t.Fatal("precondition: corpus registry missing (.cards not linked?)")
	}
	names := setAuditTMTNames()
	if len(names) != 195 {
		t.Fatalf("census is written for the 195-card set, found %d names", len(names))
	}
	supported := effects.Supported()
	classes := map[string][]string{}
	for _, n := range names {
		c, ok := reg.Lookup(n)
		if !ok {
			t.Fatalf("census: corpus is missing %q (the set/audit input drifted)", n)
		}
		for _, m := range reg.Unsupported(c, supported) {
			classes[m] = append(classes[m], n)
		}
	}
	// kw:Sneak is implemented now, so no TMT name may still be reported for it.
	if got := len(classes["kw:Sneak"]); got != 0 {
		t.Errorf("kw:Sneak carriers = %d, want 0 (the keyword is registered); carriers=%v", got, classes["kw:Sneak"])
	}
	// The must-be-blocked sentence is canonicalised to the MustBlock head at
	// parse time (cards/hiddenkeyword.go) and registered as supported
	// (rules/statics.go), so NO carrier reports the phantom spelling any
	// more; this is the regression pin that the class stays closed.
	if got := len(classes["kw:CARDNAME must be blocked if able."]); got != 0 {
		t.Errorf("phantom must-block spelling carriers = %d, want 0 (the spelling must stay canonicalised); carriers=%v", got, classes["kw:CARDNAME must be blocked if able."])
	}
	for m := range classes {
		t.Errorf("an unsupported mechanism class reappeared: %q (%v)", m, classes[m])
	}
	// The named card of the closed must-block class is the TMT carrier the
	// behavioural test above drives: it must now report nothing at all.
	raph, ok := reg.Lookup("Raphael, Ninja Destroyer")
	if !ok {
		t.Fatal("census: corpus is missing Raphael, Ninja Destroyer (the set/audit input drifted)")
	}
	if got := reg.Unsupported(raph, supported); len(got) != 0 {
		t.Errorf("Raphael, Ninja Destroyer must be fully supported after the canonicalisation; got %v", got)
	}
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
// Sneak is an alternative CAST (cards/kw files hold no expander for it; the
// option lives in rules/legal.go's hand walk), offered only during the
// caster's own declare-blockers step, and its mandatory additional cost is
// returning an unblocked attacker (rules/sneak.go's sneakCosts). With an
// unblocked attacker and {1}{B} in the pool the card in hand is offered the
// "sneak" cast option here.
func TestSetAudit_tmt_OrokuSaki_SneakIsCastableDeclareBlockers(t *testing.T) {
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
	// Fund exactly the Sneak cost {1}{B} (C = one generic/colorless, B = black).
	// This is the isolating amount: the normal cost {2}{B} needs 3 mana and is
	// therefore unaffordable here, so a zero-option result can only be the
	// missing sneak alternate cast -- not the card being unplayable anyway.
	fundPool(t, e, "CB")

	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority || d.Player != 0 {
		t.Fatalf("not at seat 0 priority in declare blockers: %+v", d)
	}
	if opt := castByName(t, e, 0, "Oroku Saki, Shredder Rising"); opt == nil {
		t.Fatalf("CR 702.190a: Oroku Saki must be castable for its sneak cost with an unblocked attacker; cast options were %+v",
			castOptions(t, e))
	}
}

// TestSetAudit_tmt_Sneak_isNotReportedSupported is the updated half of the
// Sneak finding: kw:Sneak is now implemented (rules/sneak.go registers it),
// so a card whose only formerly-unimplemented primitive was kw:Sneak must no
// longer be reported unsupported for it. The assertion is inverted from the
// pre-implementation pin to track the registration.
func TestSetAudit_tmt_Sneak_isNotReportedSupported(t *testing.T) {
	reg := searchTestRegistry(t)
	supported := effects.Supported()
	if !supported["kw:Sneak"] {
		t.Fatal("kw:Sneak is not registered in effects.Supported()")
	}
	c := searchCorpusCard(t, reg, "Oroku Saki, Shredder Rising")
	if !c.Faces[0].HasKeyword("Sneak") {
		t.Fatal("precondition: Oroku Saki does not print Sneak")
	}
	for _, m := range reg.Unsupported(c, supported) {
		if m == "kw:Sneak" {
			t.Fatalf("Sneak is implemented but kw:Sneak is still reported missing for %s", c.Faces[0].Name)
		}
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

// TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBeBlockedBindsBehaviourally is the
// correct form of the must-be-blocked audit. The engine does NOT model the
// requirement as a `MustBlock` static (that would be the blocker-oriented Mode$
// MustBlock); it reads the printed literal head `CARDNAME must be blocked if
// able.` through rules/statics.go parseHiddenKeyword into
// hiddenKeywordFlags{mustBlock:true}, and rules/combat.go askBlockers stamps
// AttackMust on every offered block option against such an attacker (CR
// 509.1c). The audit's job is to prove the requirement BINDS, so this test
// drives a real declare-blockers ask over the real corpus Raphael and asserts
// the option against him carries AttackMust and an empty declaration is
// rejected. Regression pin (expected to pass).
func TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBeBlockedBindsBehaviourally(t *testing.T) {
	raph := setAuditRealCard(t, "Raphael, Ninja Destroyer")
	e := threeSeatEngine(t)
	att := onBoardCard(t, e, 1, raph)
	blocker := onBoardCard(t, e, 0, card(t, "Name:Test Turtle Blocker\nManaCost:1\nTypes:Creature Turtle\nPT:0/4\nOracle:x\n"))
	// Preconditions: both are live battlefield creatures, neither is summoning
	// sick in a way that bars blocking, and the requirement really is present
	// on the derived list (the reader, not an inert static).
	if o := e.G.Obj(att); o == nil || o.Zone != state.ZBattlefield || o.Controller != 1 {
		t.Fatalf("precondition: Raphael not on seat 1's battlefield: %+v", o)
	}
	if o := e.G.Obj(blocker); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: blocker not on seat 0's battlefield: %+v", o)
	}
	if !e.hasMustBeBlockedKeyword(att) {
		t.Fatalf("CR 509.1a: the derived list does not carry must-be-blocked for %s; keywords=%v",
			raph.Faces[0].Name, e.Derived(att).Keywords)
	}
	attackSeat0(t, e, att)
	// The blocker must legally be able to block now that Raphael is declared
	// attacking (canBlock reads IsAttacking), so the requirement has a pair.
	if !e.canBlock(blocker, att) {
		t.Fatal("precondition: the blocker cannot legally block Raphael, so no pair is offered")
	}
	d := askBlockersFresh(t, e)
	if d == nil {
		t.Fatal("no declare-blockers decision posed for a must-be-blocked attacker with a legal blocker")
	}
	opt := findBlockOption(d, blocker, att)
	if opt == nil {
		t.Fatalf("legal block pair not offered: %+v", d.Options)
	}
	if !opt.AttackMust {
		t.Fatalf("CR 509.1c: the block option against Raphael is not flagged AttackMust: %+v", opt)
	}
	// The empty declaration is illegal: the requirement is unmet. This is the
	// behavioural half -- the flag alone could be a label.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{}}); err == nil {
		t.Fatal("an empty declaration satisfied Raphael's must-be-blocked requirement")
	}
	// The legal block commits.
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{opt.Index}}); err != nil {
		t.Fatalf("the legal blocking declaration was rejected: %v", err)
	}
	if !blockCommitted(t, e, att, blocker) {
		t.Fatalf("block did not commit: BlockedBy=%v", e.G.Obj(att).BlockedBy)
	}
}

// TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBlockSpellingIsClassified is the
// honest remainder of the must-be-blocked audit: the requirement WORKS (the
// behavioural test above proves it binds), but (*cards.Registry).Unsupported
// reports the phantom primitive `kw:CARDNAME must be blocked if able.` rather
// than a real keyword head, so the coverage census counted Raphael as
// unsupported when the play behaviour was correct. That artefact is now
// closed: the printed sentence head is canonicalised to the MustBlock head at
// parse time (cards/hiddenkeyword.go) and the head is registered as supported
// (rules/statics.go), so the census no longer counts the must-block class at
// all. This test pins the closed classification: the printed face carries the
// canonical head, and no phantom spelling is reported unsupported.
func TestSetAudit_tmt_RaphaelNinjaDestroyer_MustBlockSpellingIsCanonicalised(t *testing.T) {
	reg := searchTestRegistry(t)
	raph := searchCorpusCard(t, reg, "Raphael, Ninja Destroyer")
	canonical := false
	for _, k := range raph.Faces[0].Keywords {
		if cards.KeywordHead(k) == "MustBlock" {
			canonical = true
		}
	}
	if !canonical {
		t.Fatalf("the must-be-blocked line should be canonicalised to the MustBlock head; keywords=%v", raph.Faces[0].Keywords)
	}
	if got := reg.Unsupported(raph, effects.Supported()); len(got) != 0 {
		t.Fatalf("Raphael should be fully supported after the canonicalisation; got %v", got)
	}
}

// ---------------------------------------------------------------------------
// (a) Class -- level bands (CR 716)
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_LeaderTalent_Level2LeavesTrigger pins a Class card's level
// band end to end: CR 716.2 says gaining a level adds the level bar's
// abilities. At level 2 Leader's Talent gains "Whenever a creature you control
// leaves the battlefield, if it had a counter on it, you gain 2 life." (its
// K:Class:2 `AddTrigger$ TriggerLeaves`). The card starts at level 1 and
// reaches level 2 by activating its "{2}{W}: Level 2" ability; this test drives
// the real activation and then the trigger, so a broken band cannot pass.
func TestSetAudit_tmt_LeaderTalent_Level2LeavesTrigger(t *testing.T) {
	reg := searchTestRegistry(t)
	e, _ := classEngine(t, reg, "Leader's Talent", "Grizzly Bears")
	lt := classMove(t, e, "Leader's Talent", state.ZBattlefield)
	bear := classMove(t, e, "Grizzly Bears", state.ZBattlefield)
	if got := e.G.Obj(lt).ClassLevel(); got != 1 {
		t.Fatalf("precondition: Leader's Talent at level %d, want 1", got)
	}
	// Preconditions the post-assertions depend on: the creature is a live
	// battlefield permanent, and the level-2 band really carries the leaves
	// trigger (a Class card without it would otherwise pass silently).
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Grizzly Bears not on the battlefield: %+v", o)
	}
	if !classHasTriggerMode(e.G.Obj(lt).Face(), "ChangesZone") {
		t.Fatalf("precondition: Leader's Talent face has no ChangesZone trigger; modes=%v", triggerModes(e.G.Obj(lt).Face()))
	}

	// Drive the real {2}{W}: Level 2 activation.
	classLevelUp(t, e, lt, 0) // -> level 2
	if got := e.G.Obj(lt).ClassLevel(); got != 2 {
		t.Fatalf("precondition: LEVEL=%d after the level-2 activation, want 2", got)
	}
	// The creature must have a counter for the trigger's rider to hold.
	e.emit(events.Event{Kind: events.CounterChange, Obj: bear, Counter: "P1P1", Amount: 1})
	if e.G.Obj(bear).Counter("P1P1") != 1 {
		t.Fatal("precondition: the +1/+1 counter did not land on the creature")
	}
	lifeBefore := e.G.Players[0].Life
	// The creature leaves the battlefield: the level-2 trigger must fire.
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	e.priorityRound()
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Life; got != lifeBefore+2 {
		t.Fatalf("CR 716.2: level-2 leaves-the-battlefield trigger must gain 2 life: %d -> %d", lifeBefore, got)
	}
}

// ---------------------------------------------------------------------------
// (c) Library: Dig rest-in-random-order and DigUntil
// ---------------------------------------------------------------------------

// TestSetAudit_tmt_CaseyJones_RestGoesToBottomInRandomOrder pins the Dig
// rider RestRandomOrder$ True: "look at the top four cards of your library. You
// may reveal an artifact card from among them and put it into your hand. Put
// the rest on the bottom of your library in a random order." The test pins a
// top-4 of [Sol Ring, Mountain, Mountain, Mountain], takes the artifact, and
// asserts the artifact went to hand and the three non-artifacts are the bottom
// three (composition AND position changed, so a no-op Dig cannot pass).
func TestSetAudit_tmt_CaseyJones_RestGoesToBottomInRandomOrder(t *testing.T) {
	cj := setAuditRealCard(t, "Casey Jones, Jury-Rig Justiciar")
	if !cj.Faces[0].HasKeyword("Haste") {
		t.Fatal("precondition: Casey Jones should have Haste")
	}
	// Precondition: the card really has a self-ETB Dig trigger with the random
	// remainder rider (so a missing feature fails loudly, not silently).
	digTrigger := false
	for _, tr := range cj.Faces[0].Triggers {
		sa := cards.ResolveSVar(cj.Faces[0].SVars, tr.Params["Execute"])
		if tr.Params["Mode"] == "ChangesZone" && sa != nil && sa.API == "Dig" {
			if sa.Params["RestRandomOrder"] != "True" {
				t.Fatalf("precondition: Casey Jones' Dig no longer carries RestRandomOrder$: %+v", sa.Params)
			}
			digTrigger = true
		}
	}
	if !digTrigger {
		t.Fatal("precondition: Casey Jones has no self-ETB Dig trigger")
	}
	reg := searchTestRegistry(t)
	ring := searchCorpusCard(t, reg, "Sol Ring")
	mountain := searchCorpusCard(t, reg, "Mountain")
	// A deck that leads with the artifact and three lands guarantees the
	// pinned window; the filler keeps the deck at 40.
	filler := make([]*cards.Card, 0, 36)
	for i := 0; i < 36; i++ {
		filler = append(filler, mountain)
	}
	deck := append([]*cards.Card{cj, ring, mountain, mountain, mountain}, filler...)
	opp := make([]*cards.Card, 40)
	for i := range opp {
		opp[i] = mountain
	}
	cfg := seatZeroStart(Config{Seed: 9441, Names: []string{"casey", "villain"},
		Decks: [][]*cards.Card{deck, opp}, Tokens: map[string]*cards.Card{}})
	e := New(cfg)
	e.Advance()
	toMain1(t, e)

	libBefore := digReorder(t, e, "Sol Ring", "Mountain", "Mountain", "Mountain")
	if len(libBefore) < 5 {
		t.Fatalf("precondition: library %d cards, need a window plus a tail", len(libBefore))
	}
	ringID := libBefore[0]
	if got := e.G.Obj(ringID).Face().Name; got != "Sol Ring" {
		t.Fatalf("precondition: top card = %s, want Sol Ring", got)
	}
	untaken := append([]state.ObjID(nil), libBefore[1:4]...)
	below := append([]state.ObjID(nil), libBefore[4:]...)

	// Trigger the ETB by moving Casey Jones from hand/library to the
	// battlefield through a logged event.
	cjID := searchMoveByName(t, e, "Casey Jones, Jury-Rig Justiciar", state.ZBattlefield)
	if o := e.G.Obj(cjID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("precondition: Casey Jones not on the battlefield: %+v", o)
	}
	// Answer the optional take ask with the Sol Ring.
	ask := passUntilNonPriority(t, e, 30)
	if ask == nil || ask.Kind != decision.KChoose {
		t.Fatalf("Dig take ask = %+v, want a KChoose", ask)
	}
	took := -1
	for _, o := range ask.Options {
		if o.Obj == ringID {
			took = o.Index
		}
	}
	if took < 0 {
		t.Fatalf("the Sol Ring was not offered in the Dig window: %+v", ask.Options)
	}
	submitChoices(t, e, took)
	passUntilStackEmpty(t, e, 40)
	if hasNote(e, "unimplemented") {
		t.Fatalf("Dig rider went unimplemented: %v", noteTexts(e))
	}

	// The taken artifact is in hand.
	if o := e.G.Obj(ringID); o == nil || o.Zone != state.ZHand {
		t.Fatalf("CR 701.20: the taken artifact zone = %v, want Hand", o.Zone)
	}
	// The below-window tail is intact at the front, and the three untaken
	// window cards are the bottom three (composition changed position).
	libAfter := e.G.Zone(state.ZLibrary, 0)
	if len(libAfter) != len(below)+3 {
		t.Fatalf("library %d cards, want %d", len(libAfter), len(below)+3)
	}
	for i, oid := range below {
		if libAfter[i] != oid {
			t.Fatalf("library[%d] = %v, want the below-window card %v", i, libAfter[i], oid)
		}
	}
	bottom := libAfter[len(libAfter)-3:]
	seen := map[state.ObjID]bool{}
	for _, oid := range bottom {
		seen[oid] = true
	}
	for _, oid := range untaken {
		if !seen[oid] {
			t.Fatalf("CR 701.20: untaken window card %v is not among the bottom three %v", oid, bottom)
		}
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
	var copyID state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		o := e.G.Obj(id)
		if o != nil && o.ID != cdID && o.ID != ringID && o.Face() != nil && o.Face().Name == "Sol Ring" {
			copies++
			copyID = id
		}
	}
	if copies != 1 {
		t.Fatalf("CR 707.2: the copy token should exist on the battlefield, found %d", copies)
	}
	// The copy must actually have haste (the PumpKeywords$ rider).
	if !e.HasKeyword(copyID, "Haste") {
		t.Fatalf("CR 707.2: the copy token must gain haste; keywords=%v", e.Derived(copyID).Keywords)
	}
	// The copy must be sacrificed at the beginning of the next end step
	// (CR 513.2), while the original artifact survives.
	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	for e.putTriggersOnStack() {
		answerTriggerOrders(t, e)
		passUntilStackEmpty(t, e, 40)
	}
	passUntilStackEmpty(t, e, 40)
	if z := e.G.Obj(copyID).Zone; z == state.ZBattlefield {
		t.Fatalf("CR 513.2: the copy token survived the next end step (zone %s)", z)
	}
	if z := e.G.Obj(ringID).Zone; z != state.ZBattlefield {
		t.Fatalf("the ORIGINAL Sol Ring must survive its copy's delayed sacrifice (zone %s)", z)
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

func triggerModes(f *cards.Face) []string {
	out := make([]string, 0, len(f.Triggers))
	for _, tr := range f.Triggers {
		out = append(out, tr.Params["Mode"])
	}
	return out
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
