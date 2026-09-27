package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// protectorOfTheWastesEngine seats Protector of the Wastes on the battlefield
// under seat 0 and returns its ETB target SA plus three battlefield artifacts:
// two controlled by seat 0 (sameControllerA/B) and one by seat 1. The
// different-controllers constraint is then exercised across a real controller
// split. Preconditions are asserted so a vacuous fixture fails loudly rather
// than passing silently.
func protectorOfTheWastesEngine(t *testing.T) (e *Engine, sa *cards.SA, a0, a1, b0 state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	protector, ok := reg.Lookup("Protector of the Wastes")
	if !ok {
		t.Fatal("Protector of the Wastes missing from corpus")
	}
	artifact := func() *cards.Card {
		return &cards.Card{Faces: []*cards.Face{{Name: "Relic", Types: []string{"Artifact"}}}}
	}
	e = newSeats(t, 3)
	// Two artifacts under seat 0 and one under seat 1: the offer must group the
	// seat-0 pair together and the seat-1 artifact apart.
	a0 = e.G.AddObject(artifact(), 0).ID
	a1 = e.G.AddObject(artifact(), 0).ID
	b0 = e.G.AddObject(artifact(), 1).ID
	for _, id := range []state.ObjID{a0, a1, b0} {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
		if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
			t.Fatalf("fixture artifact %d zone = %v, want battlefield", id, z)
		}
	}
	protectorID := e.G.AddObject(protector, 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: protectorID, From: state.ZLibrary, To: state.ZBattlefield})
	if z := e.G.Obj(protectorID).Zone; z != state.ZBattlefield {
		t.Fatalf("fixture Protector zone = %v, want battlefield", z)
	}
	// The ETB ChangesZone trigger's Execute$ is the DB$ ChangeZone SA whose
	// ValidTgts$ Artifact,Enchantment the ask must group.
	sa = cards.ResolveSVar(protector.Faces[0].SVars, "TrigExile")
	if sa == nil {
		t.Fatal("Protector's TrigExile SVar did not resolve to a target SA")
	}
	if sa.Params["ValidTgts"] != "Artifact,Enchantment" {
		t.Fatalf("fixture precondition: TrigExile ValidTgts$ = %q, want Artifact,Enchantment", sa.Params["ValidTgts"])
	}
	if sa.Params["TargetsWithDifferentControllers"] != "True" {
		t.Fatalf("fixture precondition: TrigExile TargetsWithDifferentControllers$ = %q, want True", sa.Params["TargetsWithDifferentControllers"])
	}
	return e, sa, a0, a1, b0
}

// TestProtectorOfTheWastesDifferentControllersGroups pins the offer-time half
// of TargetsWithDifferentControllers$ on Protector of the Wastes' real corpus
// card: every target option carries the same controller-keyed Option.Group
// (the wire contract Decision.Validate enforces), so an answer reusing one
// controller is impossible to submit while one target per controller is
// accepted. It is the sibling of TestTargetsForEachPlayerUsesDecisionGroups,
// which pins the same grouping for TargetsForEachPlayer$ — both constraints
// share the one helper.
func TestProtectorOfTheWastesDifferentControllersGroups(t *testing.T) {
	e, _, a0, a1, b0 := protectorOfTheWastesEngine(t)
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the ETB trigger's target decision", d)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("target bounds = (%d, %d), want (0, 2)", d.Min, d.Max)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options = %+v, want one per battlefield artifact", d.Options)
	}
	groupOf := map[state.ObjID]string{}
	for _, o := range d.Options {
		if o.Obj != a0 && o.Obj != a1 && o.Obj != b0 {
			t.Fatalf("offered %d — only the three battlefield artifacts are targets", o.Obj)
		}
		groupOf[o.Obj] = o.Group
	}
	if groupOf[a0] == "" || groupOf[a0] != groupOf[a1] || groupOf[a0] == groupOf[b0] {
		t.Fatalf("groups = %v, want one group per controller (seat-0 pair together, seat-1 apart)", groupOf)
	}
	indexOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		return -1
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(a0), indexOf(a1)}}); err == nil {
		t.Fatal("two artifacts controlled by one player were accepted")
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(a0), indexOf(b0)}}); err != nil {
		t.Fatalf("one artifact per controller rejected: %v", err)
	}
}

// TestProtectorOfTheWastesExilesOnlyDifferentControllers drives the real card
// end to end: the ETB trigger's ChangeZone exiles the two chosen artifacts
// (one per controller) and leaves the unchosen same-controller artifact on the
// battlefield. Before the key was read, the seat-0 pair was a legal answer and
// the card exiled two permanents one player controlled.
func TestProtectorOfTheWastesExilesOnlyDifferentControllers(t *testing.T) {
	e, _, a0, a1, b0 := protectorOfTheWastesEngine(t)
	e.pending = nil
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the ETB trigger's target decision", d)
	}
	indexOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		return -1
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(a0), indexOf(b0)}}); err != nil {
		t.Fatalf("submit one-per-controller answer: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if z := e.G.Obj(a0).Zone; z != state.ZExile {
		t.Fatalf("chosen artifact %d zone = %v, want exile", a0, z)
	}
	if z := e.G.Obj(b0).Zone; z != state.ZExile {
		t.Fatalf("chosen artifact %d zone = %v, want exile", b0, z)
	}
	if z := e.G.Obj(a1).Zone; z != state.ZBattlefield {
		t.Fatalf("unchosen same-controller artifact %d zone = %v, want battlefield", a1, z)
	}
}

// TestProtectorOfTheWastesResolutionRecheckNarrowsSameController pins the
// resolution half: CR 608.2b's recheck on the real card's SA keeps one target
// per controller when the recorded set has grown to violate the constraint
// (a controller change in response). The recheck only removes; it never
// widens a list the per-target filter already narrowed, and a
// different-controller set passes through untouched.
func TestProtectorOfTheWastesResolutionRecheckNarrowsSameController(t *testing.T) {
	e, sa, a0, a1, b0 := protectorOfTheWastesEngine(t)
	// Precondition: the real SA carries the flag the recheck keys on.
	if sa.Params["TargetsWithDifferentControllers"] != "True" {
		t.Fatal("fixture precondition: the real SA does not carry TargetsWithDifferentControllers$")
	}
	same := []state.Target{{Obj: a0}, {Obj: a1}}
	got := e.legalTargets(same, sa, targetZones(sa), 0, 0, 0)
	if len(got) != 1 || got[0].Obj != a0 {
		t.Fatalf("same-controller recheck = %+v, want only the first recorded target %d", got, a0)
	}
	mixed := []state.Target{{Obj: a0}, {Obj: b0}}
	got = e.legalTargets(mixed, sa, targetZones(sa), 0, 0, 0)
	if len(got) != 2 || got[0].Obj != a0 || got[1].Obj != b0 {
		t.Fatalf("different-controller recheck = %+v, want both targets kept", got)
	}
	// The constraint must not leak onto a non-flag-bearing SA: the same
	// same-controller pair survives an ordinary Artifact,Enchantment recheck.
	plain := &cards.SA{Params: map[string]string{"ValidTgts": "Artifact,Enchantment"}}
	if got := e.legalTargets(same, plain, targetZones(plain), 0, 0, 0); len(got) != 2 {
		t.Fatalf("plain recheck = %+v, want both targets kept (no constraint)", got)
	}
}

// bearPermanent places a real corpus Runeclaw Bear (2/2, so it survives the
// 704.5f toughness SBA a bare Type-only fixture would die to) onto the
// battlefield under seat, and asserts the placement really took effect.
func bearPermanent(t *testing.T, e *Engine, seat state.PlayerID) state.ObjID {
	t.Helper()
	bear := mshCorpusCard(t, "Runeclaw Bear")
	id := e.G.AddObject(bear, seat).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZBattlefield})
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZBattlefield || o.Controller != seat {
		t.Fatalf("bear fixture %d: zone %v controller %d, want battlefield under seat %d", id, o.Zone, o.Controller, seat)
	}
	return id
}

// runAwayTogetherEngine seats Run Away Together (the real corpus card, whose
// SP carries TargetsWithDifferentControllers$ True with a MANDATORY
// TargetMin$ 2 | TargetMax$ 2) in seat 0's hand with a funded {1}{U} pool,
// then puts one Runeclaw Bear on the battlefield for each seat in
// controllers. It drives to seat 0's main phase. Preconditions are asserted:
// the card really is in hand, the bears really are battlefield permanents,
// and the pool really pays {1}{U}, so a vacuous fixture fails loudly instead
// of quietly not offering the cast.
func runAwayTogetherEngine(t *testing.T, controllers []state.PlayerID) (e *Engine, spell state.ObjID, bears []state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	runAway, ok := reg.Lookup("Run Away Together")
	if !ok {
		t.Fatal("Run Away Together missing from corpus")
	}
	sa := runAway.Faces[0].SpellAbility()
	if sa == nil || sa.Params["TargetsWithDifferentControllers"] != "True" ||
		sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("fixture precondition: Run Away Together SP = %+v, want mandatory 2 with TargetsWithDifferentControllers$", sa)
	}
	e = newSeats(t, 3)
	for _, seat := range controllers {
		bears = append(bears, bearPermanent(t, e, seat))
	}
	spell = e.G.AddObject(runAway, 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZLibrary, To: state.ZHand})
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Fatalf("fixture spell %d zone = %v, want hand", spell, z)
	}
	toMain1(t, e)
	// {1}{U}: one colourless (pays the generic) and one blue.
	addMana(t, e, 0, "UC")
	if pool := e.G.Players[0].Pool; pool[state.MU] < 1 || pool[state.MC] < 1 {
		t.Fatalf("fixture precondition: pool = %v, want at least one U and one colourless for {1}{U}", pool)
	}
	return e, spell, bears
}

// TestRunAwayTogetherMandatoryTwoSameControllerIsWithheld pins the real corpus
// card: with a mandatory two-target different-controllers SA and exactly two
// legal creatures controlled by ONE player, there is no legal answer, so
// CR 601.2c forbids announcing the spell. The offer census and the ask share
// ONE feasibility rule (rules/legal.go targetChoiceFeasible), so the cast is
// withheld at offer time rather than offered and then reversed with the
// CR 733.1 abort.
func TestRunAwayTogetherMandatoryTwoSameControllerIsWithheld(t *testing.T) {
	e, spell, bears := runAwayTogetherEngine(t, []state.PlayerID{1, 1})
	// Precondition: exactly two legal creature targets, both controlled by
	// seat 1 -- the shape that cannot satisfy the different-controller ask.
	if len(bears) != 2 {
		t.Fatalf("fixture bears = %d, want 2", len(bears))
	}
	for _, id := range bears {
		if c := e.G.Obj(id).Controller; c != 1 {
			t.Fatalf("fixture bear %d controller = %d, want 1", id, c)
		}
	}
	if castOffered(e, spell) {
		t.Fatal("Run Away Together offered with two same-controller creatures: the offer census must enforce the different-controller capacity the ask does (CR 601.2c)")
	}
	if hasNote(e, "cast aborted: no legal target") {
		t.Fatal("withheld cast still reached the target ask and aborted")
	}
	if z := e.G.Obj(spell).Zone; z != state.ZHand {
		t.Fatalf("spell zone = %v, want hand", z)
	}
}

// TestRunAwayTogetherDifferentControllersBotAnswerNeverLivelocks pins the
// positive half AND the non-livelock contract through the real bot. Seat 0
// controls one Bear and seat 1 controls two: the mandatory two-target ask has
// two distinct controller groups, so the decision (Min 2 / Max 2) IS posed
// and is answerable. The bot's own answer (botpolicy.Decide + Clamp) must
// pass Decision.Validate and Submit -- the deterministic bot re-submitting a
// rejected answer forever is exactly the livelock the capacity read prevents.
func TestRunAwayTogetherDifferentControllersBotAnswerNeverLivelocks(t *testing.T) {
	e, spell, _ := runAwayTogetherEngine(t, []state.PlayerID{0, 1, 1})
	var cast *decision.Option
	for _, o := range castOptions(t, e) {
		if o.Obj == spell {
			c := o
			cast = &c
		}
	}
	if cast == nil {
		t.Fatal("Run Away Together not offered with two distinct controllers available")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the mandatory two-target decision", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("target bounds = (%d, %d), want (2, 2) with two controller groups available", d.Min, d.Max)
	}
	groupsSeen := map[string]bool{}
	for _, o := range d.Options {
		groupsSeen[o.Group] = true
	}
	if len(groupsSeen) < 2 {
		t.Fatalf("distinct groups = %d (%v), want >= 2", len(groupsSeen), groupsSeen)
	}
	in := newTestBot(31).answer(e, d)
	if err := d.Validate(in); err != nil {
		t.Fatalf("the bot's own answer failed Validate: %v (intent %+v)", err, in)
	}
	if err := e.Submit(in); err != nil {
		t.Fatalf("the bot's own answer was rejected by Submit: %v", err)
	}
}

// TestKitsuneMandatoryTwoOneControllerFizzles pins the TRIGGER-path ask
// (askTarget) on Kitsune, Dragon's Daughter's real corpus SA: its
// exchange-control trigger is a mandatory TargetMin$ 2 | TargetMax$ 2
// TargetsWithDifferentControllers$ ask, and with only one controller
// represented there is no legal set, so the ability fizzles ("countered: no
// legal targets") instead of posing an unsatisfiable decision. The ask is
// driven directly (askTarget, the same entry the trigger drain uses) with the
// two same-controller creatures on the battlefield -- the exact board the
// pre-fix code turned into a Min 2 / Max 2 decision whose two options shared
// one Option.Group.
func TestKitsuneMandatoryTwoOneControllerFizzles(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	kitsune, ok := reg.Lookup("Kitsune, Dragon's Daughter")
	if !ok {
		t.Fatal("Kitsune, Dragon's Daughter missing from corpus")
	}
	sa := cards.ResolveSVar(kitsune.Faces[0].SVars, "TrigExchangeControl")
	if sa == nil {
		t.Fatal("Kitsune TrigExchangeControl SVar did not resolve")
	}
	if sa.Params["TargetsWithDifferentControllers"] != "True" ||
		sa.Params["TargetMin"] != "2" || sa.Params["TargetMax"] != "2" {
		t.Fatalf("fixture precondition: Kitsune SA = %+v, want mandatory 2 with TargetsWithDifferentControllers$", sa)
	}
	e := newSeats(t, 3)
	// Two creatures under ONE controller (seat 1). Precondition: both really
	// are legal `Creature.Other` targets, so the fizzle is the controller
	// constraint and not an empty candidate set.
	bears := []state.ObjID{bearPermanent(t, e, 1), bearPermanent(t, e, 1)}
	for _, id := range bears {
		if c := e.G.Obj(id).Controller; c != 1 {
			t.Fatalf("fixture bear %d controller = %d, want 1", id, c)
		}
	}
	// Kitsune's own source, under seat 0 (its ValidTgts$ Creature.Other
	// excludes it, so the two bears are the whole candidate set).
	kitsuneID := e.G.AddObject(kitsune, 0).ID
	e.emit(events.Event{Kind: events.MoveZone, Obj: kitsuneID, From: state.ZLibrary, To: state.ZBattlefield})
	if z := e.G.Obj(kitsuneID).Zone; z != state.ZBattlefield {
		t.Fatalf("Kitsune zone = %v, want battlefield", z)
	}
	e.pending = nil
	// source = Kitsune's own object, so `Creature.Other` excludes it and the
	// two same-controller bears are the whole candidate set (the board the
	// pre-fix code turned into an unsatisfiable Min 2 decision). The fizzle
	// moves the SOURCE, which in real trigger resolution is the ability stack
	// object, not this permanent -- so the zone is deliberately not asserted
	// on this direct-ask harness call.
	e.askTarget(0, kitsuneID, sa)
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		t.Fatalf("an unsatisfiable target decision was posed: %+v", d)
	}
	if !hasEventText(e, "countered: no legal targets") {
		t.Fatal("Kitsune's mandatory ask did not fizzle with one controller represented")
	}
}

// TestMysteriousStrangerOneEachWithoutForEachPlayer pins the OneEach
// spelling on a TargetsWithDifferentControllers$ SA that does NOT carry
// TargetsForEachPlayer$ (the corpus's one such line: Mysterious Stranger's
// "for each graveyard with an instant or sorcery card in it, exile target
// instant or sorcery card from that graveyard"). OneEach is the
// per-controller grammar, so it must resolve to the distinct-controller
// count on EITHER flag; before the class fix the respell was gated on
// TargetsForEachPlayer$ and this card asked for ONE target (Min 1 / Max 1)
// instead of one per represented player.
func TestMysteriousStrangerOneEachWithoutForEachPlayer(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	stranger, ok := reg.Lookup("Mysterious Stranger")
	if !ok {
		t.Fatal("Mysterious Stranger missing from corpus")
	}
	sa := cards.ResolveSVar(stranger.Faces[0].SVars, "TrigChangeZone")
	if sa == nil {
		t.Fatal("Mysterious Stranger TrigChangeZone SVar did not resolve")
	}
	// Precondition: the fixture is the shape under test -- OneEach with
	// different-controllers and WITHOUT the TargetsForEachPlayer spelling.
	if sa.Params["TargetsWithDifferentControllers"] != "True" ||
		sa.Params["TargetMin"] != "OneEach" || sa.Params["TargetMax"] != "OneEach" {
		t.Fatalf("fixture precondition: Mysterious Stranger SA = %+v, want OneEach with TargetsWithDifferentControllers$", sa)
	}
	if _, present := sa.Params["TargetsForEachPlayer"]; present {
		t.Fatal("fixture precondition: the SA unexpectedly carries TargetsForEachPlayer$, so it is not the orphan-OneEach shape")
	}
	e := newSeats(t, 3)
	// One instant in seat 1's graveyard and one in seat 2's: two distinct
	// controllers, so OneEach must ask for two.
	grave := func(seat state.PlayerID) state.ObjID {
		id := e.G.AddObject(card(t, "Name:Zap\nManaCost:R\nTypes:Instant\nOracle:x\n"), seat).ID
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZLibrary, To: state.ZGraveyard})
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("fixture instant %d zone = %v, want graveyard", id, z)
		}
		return id
	}
	a, b := grave(1), grave(2)
	e.pending = nil
	e.askTarget(0, 0, sa)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("pending = %+v, want the OneEach graveyard target decision", d)
	}
	if d.Min != 2 || d.Max != 2 {
		t.Fatalf("target bounds = (%d, %d), want (2, 2): OneEach is the distinct-controller count", d.Min, d.Max)
	}
	groupOf := map[state.ObjID]string{}
	for _, o := range d.Options {
		groupOf[o.Obj] = o.Group
	}
	if groupOf[a] == "" || groupOf[a] == groupOf[b] {
		t.Fatalf("groups = %v, want one group per controller", groupOf)
	}
	indexOf := func(id state.ObjID) int {
		for _, o := range d.Options {
			if o.Obj == id {
				return o.Index
			}
		}
		return -1
	}
	if err := d.Validate(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{indexOf(a), indexOf(b)}}); err != nil {
		t.Fatalf("one graveyard card per player rejected: %v", err)
	}
}

// hasEventText reports whether ANY event carries substr in its Text. The
// fizzle path records "countered: no legal targets" on a MoveZone, not a
// Note, so hasNote (which reads Note kinds only) would miss it.
func hasEventText(e *Engine, substr string) bool {
	for _, ev := range e.L.Events {
		if strings.Contains(ev.Text, substr) {
			return true
		}
	}
	return false
}
