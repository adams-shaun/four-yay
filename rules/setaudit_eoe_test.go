package rules

// Set audit for Edge of Eternities (eoe), 266 cards. An AUDIT ticket: its
// product is failing tests plus follow-up tickets, not fixes. Each guarded
// test names one defect and cites the CR rule; the unguarded tests are
// regression coverage for behaviour verified CORRECT during the audit.
//
// The guards are deliberate: a guarded test FAILS with GORGE_SET_AUDIT=1 and
// SKIPS otherwise, so the committed file never breaks the ordinary gates.

import (
	"os"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// eoeGuard skips a known-defect test unless GORGE_SET_AUDIT is set.
func eoeGuard(t *testing.T, defect, followUp string) {
	t.Helper()
	if os.Getenv("GORGE_SET_AUDIT") == "" {
		t.Skipf("set-audit finding (eoe): %s. Follow-up: %s", defect, followUp)
	}
}

// passToAsk drives priority submissions until a non-priority decision is
// pending (or nil). It is the harness the mid-resolution asks (a spell's
// choice, a replacement's pick) need: casting leaves the spell on the stack,
// and the ask only appears once both seats pass.
func passToAsk(t *testing.T, e *Engine, limit int) *decision.Decision {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := e.Pending()
		if d == nil {
			return nil
		}
		if d.Kind != decision.KPriority {
			return d
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	return e.Pending()
}

// ---------------------------------------------------------------------------
// (a) Tapestry Warden — a Station permanent is charged for the TAPPED
// creature's POWER (CR 702.150a: "put a number of charge counters equal to
// its power"). Tapestry Warden's second static replaces that with the
// creature's TOUGHNESS ("Each creature you control with toughness greater
// than its power stations permanents using its toughness rather than its
// power."). The engine never reads the `stat:TapPowerValue` static, so a
// 0/4 wall charges ZERO.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_TapestryWarden_StationUsesToughness(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	wall := card(t, "Name:Big Wall\nManaCost:2\nTypes:Creature Wall\nPT:0/4\nOracle:x\n")
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Hearthhull, the Worldseed"),
		lookup(t, reg, "Tapestry Warden"),
		wall,
	}, nil)
	hearth := moveByName(t, e, 0, "Hearthhull, the Worldseed", state.ZBattlefield)
	moveByName(t, e, 0, "Tapestry Warden", state.ZBattlefield)
	wallID := moveByName(t, e, 0, "Big Wall", state.ZBattlefield)

	// Precondition: the tapped creature is power < toughness, and its two
	// values differ, or the assertion below could not distinguish them.
	if p, tough := e.Power(wallID), e.Toughness(wallID); p != 0 || tough != 4 {
		t.Fatalf("precondition: wall is %d/%d, want 0/4", p, tough)
	}

	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	idx := findStationOption(d, hearth)
	if idx < 0 {
		t.Fatalf("no Station option for Hearthhull: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{idx}}); err != nil {
		t.Fatalf("submit station: %v", err)
	}
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("no tap pick after Station (got %+v)", d)
	}
	pick := -1
	for _, o := range d.Options {
		if o.Obj == wallID {
			pick = o.Index
		}
	}
	if pick < 0 {
		t.Fatalf("the 0/4 wall was not offered as a Station payer: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{pick}}); err != nil {
		t.Fatalf("submit tap pick: %v", err)
	}
	// CR 702.150a with Tapestry Warden's replacement: the wall's TOUGHNESS.
	if got := e.G.Obj(hearth).Counter("CHARGE"); got != 4 {
		t.Fatalf("Hearthhull has %d CHARGE counters after stationing with the 0/4 wall, "+
			"want 4 (its toughness; Tapestry Warden's static)", got)
	}
}

// ---------------------------------------------------------------------------
// (a) Mutinous Massacre — CR 608.2d's odd/even resolution choice controls
// which creature mana values its spell destroys.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_MutinousMassacre_ChooseEvenOdd(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, lookup(t, reg, "Mutinous Massacre"))
	// CR 202.3: mana value 2 (even) and mana value 3 (odd).
	even := card(t, "Name:Even Bear\nManaCost:2\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	odd := card(t, "Name:Odd Bear\nManaCost:3\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	evenID := placeNamedOnBattlefield(t, e, 0, even)
	oddID := placeNamedOnBattlefield(t, e, 0, odd)

	// Precondition: the two creatures' mana values differ in parity.
	if even.Faces[0].Cmc()%2 != 0 || odd.Faces[0].Cmc()%2 != 1 {
		t.Fatalf("precondition: cmcs %d/%d are not even/odd", even.Faces[0].Cmc(), odd.Faces[0].Cmc())
	}

	addMana(t, e, 0, "BBBBRRCCC")
	castNamed(t, e, "Mutinous Massacre")

	// Precondition: the cast really started (the spell reached the stack).
	spellOnStack(t, e, "Mutinous Massacre")
	// Let the spell resolve (pass priority), stopping at the first ask or
	// once the stack is empty.
	var d *decision.Decision
	for i := 0; i < 6; i++ {
		d = e.Pending()
		if d == nil || d.Kind != decision.KPriority {
			break
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0}}); err != nil {
			t.Fatalf("pass priority: %v", err)
		}
	}
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		// Name the engine's actual stand-in in the failure, so the ticket's
		// evidence points at the unimplemented API rather than a bare nil.
		var notes []string
		for _, ev := range e.L.Events {
			if ev.Kind == events.Note {
				notes = append(notes, ev.Text)
			}
		}
		t.Fatalf("Mutinous Massacre posed no odd/even choice (pending kind %v; notes %v)",
			kindOf(d), notes)
	}
	// Answer "odd": the mana-value-3 creature dies, the even one survives.
	oddOpt := -1
	for _, o := range d.Options {
		if o.Kind == "odd" || o.Label == "odd" {
			oddOpt = o.Index
		}
	}
	if oddOpt < 0 {
		t.Fatalf("the odd/even ask has no odd option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{oddOpt}}); err != nil {
		t.Fatalf("submit odd: %v", err)
	}
	passUntilStackEmpty(t, e, 40)
	if o := e.G.Obj(oddID); o.Zone == state.ZBattlefield {
		t.Fatalf("the odd-mana-value creature survived Mutinous Massacre")
	}
	if o := e.G.Obj(evenID); o.Zone != state.ZBattlefield {
		t.Fatalf("the even-mana-value creature died to the odd choice")
	}
}

// ---------------------------------------------------------------------------
// (a) The Endstone — "At the beginning of your end step, your life total
// becomes half your starting life total, rounded up." (CR 119.5's "becomes"
// is a life-loss/life-gain to the number.) `api:SetLife` is not registered,
// so the life total never moves.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_TheEndstone_EndStepSetsHalfStartingLife(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{lookup(t, reg, "The Endstone")}, nil)
	moveByName(t, e, 0, "The Endstone", state.ZBattlefield)
	starting := e.G.Players[0].Life
	// Precondition: the seat starts above half its life, so the set is a
	// real change (20 -> 10).
	if starting <= starting/2 {
		t.Fatalf("precondition: life %d is not above its half", starting)
	}
	want := starting/2 + starting%2

	driveToStepAll(t, e, e.G.Turn, 0, state.StepEnd)
	passUntilStackEmpty(t, e, 40)
	if got := e.G.Players[0].Life; got != want {
		t.Fatalf("The Endstone's end step left life at %d, want %d (half of %d rounded up)",
			got, want, starting)
	}
}

// ---------------------------------------------------------------------------
// (b) Scout for Survivors — "Return up to three target creature cards with
// total mana value 3 or less from your graveyard to the battlefield." (CR
// 601.2c's target legality, per CR 202.3's mana-value sum.) The engine now
// reads TargetMin$/TargetMax$/ValidTgts$ and `MaxTotalTargetCMC$`, so a set
// of three mana-value-3 creatures (total 9) is not a legal answer under the
// MV-3 cap. This was the set-audit finding that the MaxTotalTargetCMC$ ticket
// closed, so the test is unguarded and runs in every gate.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_ScoutForSurvivors_TotalManaValueCap(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	bear := card(t, "Name:Big Bear\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e := handEngine(t, lookup(t, reg, "Scout for Survivors"), bear, bear, bear)
	// Move the three bears to the graveyard.
	for _, id := range append([]state.ObjID{}, e.G.Zone(state.ZHand, 0)...) {
		if ob := e.G.Obj(id); ob != nil && ob.Face() != nil && ob.Face().Name == "Big Bear" {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
		}
	}
	if n := len(e.G.Zone(state.ZGraveyard, 0)); n != 3 {
		t.Fatalf("precondition: graveyard has %d cards, want 3", n)
	}
	// Precondition: the three targets' total mana value exceeds the cap.
	if total := 3 * bear.Faces[0].Cmc(); total <= 3 {
		t.Fatalf("precondition: the three targets total mana value %d, not above the cap 3", total)
	}

	addMana(t, e, 0, "WW1")
	castNamed(t, e, "Scout for Survivors")
	d := passToAsk(t, e, 40)
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 3 {
		t.Fatalf("Scout for Survivors' target ask = %+v, want the three graveyard creatures", d)
	}
	if !d.HasBudget() || d.MaxSum != 3 {
		t.Fatalf("target ask budget = (%v, %d), want a total mana-value cap of 3", d.HasBudget(), d.MaxSum)
	}
	botAnswer := newTestBot(1).answer(e, d)
	if err := d.Validate(botAnswer); err != nil {
		t.Fatalf("bot answer %v violates the target decision: %v", botAnswer.Choices, err)
	}
	// A set whose total mana value exceeds 3 is rejected by the same decision
	// budget the bot policy observes above.
	err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{0, 1, 2}})
	if err != nil {
		return // rejected: correct.
	}
	passUntilStackEmpty(t, e, 40)
	got := 0
	for _, o := range e.G.Zone(state.ZBattlefield, 0) {
		if ob := e.G.Obj(o); ob != nil && ob.Face() != nil && ob.Face().Name == "Big Bear" {
			got++
		}
	}
	if got > 1 {
		t.Fatalf("Scout for Survivors returned %d mana-value-3 creatures (total %d) "+
			"under a total-mana-value cap of 3", got, got*3)
	}
}

// ---------------------------------------------------------------------------
// (a) The Dominion Bracelet — the equipped creature gains "{15}, Exile
// CARDNAME: You control target opponent during their next turn." The granted
// activated ability is never OFFERED (the granted-ability machinery works for
// a plain cost, so the ability's `Exile<1/OriginalHost/...>` / `ReduceCost$`
// shape is the blocker), and `api:ControlPlayer` is unregistered besides.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_TheDominionBracelet_GrantedControlAbilityOffered(t *testing.T) {
	eoeGuard(t, "The Dominion Bracelet's granted '{15}, Exile this Equipment: control target "+
		"opponent' ability is never offered on the equipped creature",
		"Offer granted AddAbility$ abilities whose cost is Exile<OriginalHost> (api:ControlPlayer)")
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "The Dominion Bracelet"),
		lookup(t, reg, "Tapestry Warden"),
	}, nil)
	eq := moveByName(t, e, 0, "The Dominion Bracelet", state.ZBattlefield)
	cr := moveByName(t, e, 0, "Tapestry Warden", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: eq, IDs: []state.ObjID{cr}})
	e.G.Obj(cr).SummonSick = false

	// Precondition: the Equipment really is attached to the creature (the
	// static's Affected$ is Creature.EquippedBy), or the grant has nothing to
	// target and the assertion below would prove nothing.
	if e.G.Obj(eq).AttachedTo != cr {
		t.Fatalf("precondition: Equipment attached to %d, want the creature %d", e.G.Obj(eq).AttachedTo, cr)
	}

	addMana(t, e, 0, "CCCCCCCCCCCCCCC")
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision (got %+v)", d)
	}
	for _, o := range d.Options {
		if o.Kind == "ability" && o.Obj == cr {
			return // the granted ability is offered: correct.
		}
	}
	t.Fatalf("the equipped creature was never offered The Dominion Bracelet's granted "+
		"control ability: %+v", d.Options)
}

// placeNamedOnBattlefield puts a fresh copy of c on seat p's battlefield and
// returns its id.
func placeNamedOnBattlefield(t *testing.T, e *Engine, p state.PlayerID, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, p)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, p, append(e.G.Zone(state.ZBattlefield, p), o.ID))
	e.staticEpoch, e.activeEpoch = -1, -1
	return o.ID
}

// ---------------------------------------------------------------------------
// Regression coverage: behaviour verified CORRECT during the audit. These
// stay UNGUARDED -- they must pass in the ordinary suite.
// ---------------------------------------------------------------------------

// TestSetAudit_eoe_LostInSpace_OwnerChoosesTopOrBottom: CR 401.4 + the card's
// "its owner puts it on their choice of the top or bottom" pose a real ask to
// the TARGET's owner, not the caster.
func TestSetAudit_eoe_LostInSpace_OwnerChoosesTopOrBottom(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, lookup(t, reg, "Lost in Space"))
	bear := card(t, "Name:Big Bear\nManaCost:2 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
	e.G.SetZone(state.ZHand, 1, nil)
	opp := e.G.AddObject(bear, 1)
	opp.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 1, []state.ObjID{opp.ID})
	addMana(t, e, 0, "UUU3")
	castNamed(t, e, "Lost in Space")
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the target ask, got %+v", d)
	}
	submitChoices(t, e, 0)
	d = passToAsk(t, e, 40)
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("Lost in Space did not ask the owner for a library position: %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("the top/bottom ask went to seat %d, want the card's OWNER (seat 1)", d.Player)
	}
	bottom := -1
	for _, o := range d.Options {
		if o.Kind == "bottom" {
			bottom = o.Index
		}
	}
	if bottom < 0 {
		t.Fatalf("the ask offers no bottom option: %+v", d.Options)
	}
	if len(e.G.Zone(state.ZLibrary, 1)) == 0 {
		t.Fatal("precondition: owner's library is empty")
	}
	// Precondition: the card is not already the bottom card, or the
	// assertion below could not distinguish a real move.
	if lib := e.G.Zone(state.ZLibrary, 1); lib[len(lib)-1] == opp.ID {
		t.Fatal("precondition: the target card already is the bottom of the library")
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{bottom}}); err != nil {
		t.Fatalf("submit bottom: %v", err)
	}
	// The placement happens synchronously in the ChangeZone resolution; Lost
	// in Space's Surveil 1 sub-ability then asks an arrange, which this test
	// does not need to answer.
	lib := e.G.Zone(state.ZLibrary, 1)
	if len(lib) == 0 || lib[len(lib)-1] != opp.ID {
		t.Fatalf("the owner's bottom choice did not put the card on the bottom of their library")
	}
}

// TestSetAudit_eoe_FamishedWorldsire_DevourLand: Devour with a typed operand
// (`K:Devour:3:Land`) offers only LANDS and enters with three counters per
// land sacrificed (CR 702.83).
func TestSetAudit_eoe_FamishedWorldsire_DevourLand(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Famished Worldsire"),
		corpusAlternativeCard(t, "Forest"),
		corpusAlternativeCard(t, "Forest"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	for _, name := range []string{"Forest", "Forest", "Grizzly Bears"} {
		id := searchMoveByName(t, e, name, state.ZHand)
		placeOnBattlefield(t, e, id)
	}
	ws := searchMoveByName(t, e, "Famished Worldsire", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: ws, From: state.ZHand, To: state.ZBattlefield})
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Min != 0 || d.Max != 2 {
		t.Fatalf("Devour land ask = %+v, want sacrifice 0..2 (only the two Forests)", d)
	}
	for _, o := range d.Options {
		if name := e.G.Obj(o.Obj).Face().Name; name != "Forest" {
			t.Fatalf("Devour land offered a non-land (%s)", name)
		}
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	if got := e.G.Obj(ws).Counter("P1P1"); got != 6 {
		t.Fatalf("Famished Worldsire entered with %d P1P1, want 6 (2 lands x Devour 3)", got)
	}
}

// TestSetAudit_eoe_Hearthhull_StationCreatureAtEight: a Spacecraft is an
// artifact creature only at its printed threshold ("It's an artifact creature
// at 8+"), verified on both sides of the boundary.
func TestSetAudit_eoe_Hearthhull_StationCreatureAtEight(t *testing.T) {
	t.Parallel()
	e := handEngine(t, corpusAlternativeCard(t, "Hearthhull, the Worldseed"))
	h := searchMoveByName(t, e, "Hearthhull, the Worldseed", state.ZHand)
	placeOnBattlefield(t, e, h)
	if hasTypeName(e.Derived(h).Types, "Creature") {
		t.Fatal("precondition: Hearthhull starts as a creature before any charge counter")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: h, Counter: "CHARGE", Amount: 7})
	if hasTypeName(e.Derived(h).Types, "Creature") {
		t.Fatal("Hearthhull is a creature at 7 charge counters, want artifact only (threshold 8)")
	}
	e.emit(events.Event{Kind: events.CounterChange, Obj: h, Counter: "CHARGE", Amount: 1})
	if !hasTypeName(e.Derived(h).Types, "Creature") {
		t.Fatalf("Hearthhull is not a creature at 8 charge counters: %v", e.Derived(h).Types)
	}
	for _, want := range []string{"Flying", "Vigilance", "Haste"} {
		if !hasKeywordName(e.Derived(h).Keywords, want) {
			t.Fatalf("Hearthhull at 8 lacks %s: %v", want, e.Derived(h).Keywords)
		}
	}
}

// TestSetAudit_eoe_CountVoid_TokenDeparture: the Void condition's first half
// (CR 700-ish ability word, "a nonland permanent left the battlefield this
// turn") counts a departing nonland TOKEN, and the `Count$Void.T.F` argument
// form selects the true/false value.
func TestSetAudit_eoe_CountVoid_TokenDeparture(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	if n := evalHead(t, e, 0, 0, "Count$Void.10.2"); n != 2 {
		t.Fatalf("Count$Void.10.2 with no departure = %d, want 2 (the false value)", n)
	}
	bear := onBoardCard(t, e, 0, corpusCard(t, "Grizzly Bears"))
	// Precondition: the departing permanent is a nonland.
	if !hasTypeName(e.Derived(bear).Types, "Creature") {
		t.Fatal("precondition: the fixture permanent is not a creature")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: bear, From: state.ZBattlefield, To: state.ZGraveyard})
	if n := evalHead(t, e, 0, 0, "Count$Void.1.0"); n != 1 {
		t.Fatalf("Count$Void.1.0 after a nonland creature left = %d, want 1", n)
	}
	if n := evalHead(t, e, 0, 0, "Count$Void.10.2"); n != 10 {
		t.Fatalf("Count$Void.10.2 after a nonland creature left = %d, want 10 (the true value)", n)
	}
}

// ---------------------------------------------------------------------------
// (a) Mightform Harmonizer — Landfall "double the power of target creature
// you control until end of turn." Forge writes the doubling as
// `DB$ Pump | NumAtt$ Double`; the "Double" count operation is read nowhere
// (`/usr/bin/grep -rn '"Double"' effects rules cards` has no non-test hit),
// so `Num` degrades the unresolvable amount to 0 and the pump adds +0/+0.
// ---------------------------------------------------------------------------

func TestSetAudit_eoe_MightformHarmonizer_DoublePower(t *testing.T) {
	eoeGuard(t, "the 'Double' Pump amount (NumAtt$ Double / NumDef$ Double) is unmodelled, "+
		"so Mightform Harmonizer's Landfall doubling adds +0/+0",
		"Implement the Double count operation for Pump/Animate P-T (36 corpus carriers)")
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Mightform Harmonizer"),
		lookup(t, reg, "Grizzly Bears"),
		lookup(t, reg, "Forest"),
	}, nil)
	moveByName(t, e, 0, "Mightform Harmonizer", state.ZBattlefield)
	bear := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)

	// Precondition: the pump target's power is worth doubling (2 -> 4) and
	// the base is not already the doubled value.
	if p := e.Power(bear); p != 2 {
		t.Fatalf("precondition: bear power %d, want 2", p)
	}

	moveByName(t, e, 0, "Forest", state.ZBattlefield)
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Mightform Harmonizer's Landfall did not ask for a target: %+v", d)
	}
	picked := false
	for _, o := range d.Options {
		if o.Obj == bear {
			submitChoices(t, e, o.Index)
			picked = true
		}
	}
	if !picked {
		t.Fatalf("the bear was not a legal Landfall target: %+v", d.Options)
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.Power(bear); got != 4 {
		t.Fatalf("the doubled Landfall pump left the bear at power %d, want 4 (2 doubled)", got)
	}
}

// TestSetAudit_eoe_SunstarExpansionist_LandfallPump: the numeric sibling
// (`NumAtt$ +1`) DOES work, so the Double failure above is the "Double"
// operation and not Landfall itself.
func TestSetAudit_eoe_SunstarExpansionist_LandfallPump(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := corpusEngine(t, reg, []*cards.Card{
		lookup(t, reg, "Sunstar Expansionist"),
		lookup(t, reg, "Forest"),
	}, nil)
	moveByName(t, e, 0, "Sunstar Expansionist", state.ZBattlefield)
	passUntilStackEmpty(t, e, 20)
	sea := idOf(t, e, "Sunstar Expansionist")
	if p := e.Power(sea); p != 2 {
		t.Fatalf("precondition: Sunstar power %d, want 2", p)
	}
	moveByName(t, e, 0, "Forest", state.ZBattlefield)
	e.putTriggersOnStack()
	passUntilStackEmpty(t, e, 20)
	if got := e.Power(sea); got != 3 {
		t.Fatalf("Sunstar Expansionist's +1/+0 Landfall pump left power %d, want 3", got)
	}
}

// TestSetAudit_eoe_LightstallInquisitor_HiddenOpponentChoice: "each opponent
// exiles a card from their hand" (CR 701.13) is a HIDDEN choice made by that
// opponent, not by the caster.
func TestSetAudit_eoe_LightstallInquisitor_HiddenOpponentChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := handEngine(t, lookup(t, reg, "Lightstall Inquisitor"))
	for _, n := range []string{"Grizzly Bears", "Forest"} {
		o := e.G.AddObject(lookup(t, reg, n), 1)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 1, append(e.G.Zone(state.ZHand, 1), o.ID))
	}
	li := searchMoveByName(t, e, "Lightstall Inquisitor", state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: li, From: state.ZHand, To: state.ZBattlefield})
	d := passToAsk(t, e, 40)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "hand_move" {
		t.Fatalf("Lightstall Inquisitor did not pose the hidden hand-exile ask: %+v", d)
	}
	if d.Player != 1 {
		t.Fatalf("the hidden exile ask went to seat %d, want the exiling OPPONENT (seat 1)", d.Player)
	}
	submitChoices(t, e, d.Options[0].Index)
	passUntilStackEmpty(t, e, 40)
	if n := len(e.G.Zone(state.ZExile, 1)); n != 1 {
		t.Fatalf("opponent exile count = %d, want 1", n)
	}
}

// kindOf names a decision's kind for a failure message, tolerating nil.
func kindOf(d *decision.Decision) string {
	if d == nil {
		return "<nil>"
	}
	return string(d.Kind)
}

func hasTypeName(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

func hasKeywordName(kws []string, want string) bool {
	for _, k := range kws {
		if k == want {
			return true
		}
	}
	return false
}
