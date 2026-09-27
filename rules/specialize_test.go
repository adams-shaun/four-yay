package rules

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Specialize (Battle for Baldur's Gate Commander / Bloomburrow Commander) is
// a no-stack special action: pay the printed K:Specialize cost on your own
// front-face permanent and it becomes one of the card's five specialization
// faces. The parser now splits SPECIALIZE:<COLOR> boundaries into real faces,
// which is what makes the front-face name resolvable and the per-face triggers
// real; these tests drive the action and the "When this creature specializes"
// Mode$ Specializes trigger through the engine.

// specializeEngine places card on seat 0's battlefield (front face) in a
// Main1 priority window. lands is how many Basic Mountains seat 0 controls, so
// an IsPresent$ Land.YouCtrl gate can be made to bind or not.
func specializeEngine(t *testing.T, c *cards.Card, lands int) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t)
	id := onBoardCard(t, e, 0, c)
	mountain := card(t, "Name:Mountain\nTypes:Basic Land Mountain\nOracle:x\n")
	for i := 0; i < lands; i++ {
		onBoardCard(t, e, 0, mountain)
	}
	return e, id
}

// specializeOption returns the offered specialize option for id at face
// faceIdx, if any.
func specializeOption(e *Engine, p state.PlayerID, id state.ObjID, faceIdx int) (decision.Option, bool) {
	for _, o := range e.legalActions(p) {
		if o.Kind == "specialize" && o.Obj == id && o.Mode == strconv.Itoa(faceIdx) {
			return o, true
		}
	}
	return decision.Option{}, false
}

// TestSpecializeSpecialActionChangesFaceAndMatchesTrigger is the inline-shape
// unit pin: the option exists on the front face, the submitted action changes
// FaceIdx through an event-sourced Specialize transition, and the registered
// matcher accepts the specializing object's own event.
func TestSpecializeSpecialActionChangesFaceAndMatchesTrigger(t *testing.T) {
	c, diags := cards.ParseBytes("specialize.txt", []byte(`Name:Front
AlternateMode:Specialize
Types:Creature Druid
K:Specialize:0
SPECIALIZE:WHITE
Name:White Form
ManaCost:W
Types:Creature
T:Mode$ Specializes | Execute$ Trig
SVar:Trig:DB$ PutCounter | CounterType$ P1P1 | CounterNum$ 1 | Defined$ Self
`))
	if len(diags) != 0 {
		t.Fatalf("parse diags: %+v", diags)
	}
	e := handEngine(t)
	for p := state.PlayerID(0); p < 2; p++ {
		e.G.SetZone(state.ZBattlefield, p, nil)
	}
	o := e.G.AddObject(c, 0)
	o.Zone = state.ZBattlefield
	e.G.SetZone(state.ZBattlefield, 0, []state.ObjID{o.ID})
	if o.FaceIdx != 0 || o.Zone != state.ZBattlefield {
		t.Fatalf("bad setup: face=%d zone=%s", o.FaceIdx, o.Zone)
	}
	if _, ok := specializeOption(e, 0, o.ID, 1); !ok {
		t.Fatalf("free specialize option not offered: %+v", e.legalActions(0))
	}
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("priority decision missing: %+v", d)
	}
	idx := -1
	for _, op := range d.Options {
		if op.Kind == "specialize" && op.Obj == o.ID && op.Mode == "1" {
			idx = op.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no face-1 specialize option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if o.FaceIdx != 1 {
		t.Fatalf("FaceIdx=%d, want 1", o.FaceIdx)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.Specialize && ev.Obj == o.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("specialize transition was not event sourced")
	}
	tr := o.Face().Triggers[0]
	if !trigMatchers["Specializes"](e, tr, o.ID, events.Event{Kind: events.Specialize, Obj: o.ID}, nil) {
		t.Fatal("Mode$ Specializes matcher did not match its own object's transition")
	}
}

// TestSpecializeLukaminaGateBindsOnLands is the gate-binding pin: the real
// corpus Lukamina's `IsPresent$ Land.YouCtrl | PresentCompare$ GE6` gate must
// WITHHOLD the offer at five lands and grant it at six. The two boards differ
// by exactly the counted value, so a build that ignored the gate cannot pass
// both halves.
func TestSpecializeLukaminaGateBindsOnLands(t *testing.T) {
	card := corpusCard(t, "Lukamina, Moon Druid")
	if len(card.Faces) != 6 {
		t.Fatalf("precondition: Lukamina compiles to %d faces, want 6", len(card.Faces))
	}

	below, belowID := specializeEngine(t, card, 5)
	addMana(t, below, 0, "CCC")
	if _, ok := specializeOption(below, 0, belowID, 2); ok {
		t.Fatal("precondition violated: specialize offered with only five lands (gate counts GE6)")
	}

	at, atID := specializeEngine(t, card, 6)
	addMana(t, at, 0, "CCC")
	if _, ok := specializeOption(at, 0, atID, 2); !ok {
		t.Fatalf("specialize not offered with six lands: %+v", at.legalActions(0))
	}
}

// TestSpecializeShadowheartSVarGateBinds is the second gate grammar: the
// real corpus Shadowheart's `CheckSVar$ X | SVarCompare$ LE13` gate (X is
// PlayerCountPlayers$LowestLifeTotal) must WITHHOLD the offer while every
// player is above 13 life and grant it once the lowest life total is 13 or
// less. The two boards differ by exactly the compared value, so a build that
// ignored the CheckSVar$ half of the grammar cannot pass both halves.
func TestSpecializeShadowheartSVarGateBinds(t *testing.T) {
	card := corpusCard(t, "Shadowheart, Sharran Cleric")
	if len(card.Faces) != 6 {
		t.Fatalf("precondition: Shadowheart compiles to %d faces, want 6", len(card.Faces))
	}
	if got := card.Faces[0].SVars["X"]; got != "PlayerCountPlayers$LowestLifeTotal" {
		t.Fatalf("precondition: front-face SVar X = %q, want the LowestLifeTotal count", got)
	}

	above, aboveID := specializeEngine(t, card, 0)
	above.G.Players[0].Life = 20
	above.G.Players[1].Life = 15
	addMana(t, above, 0, "CC")
	if _, ok := specializeOption(above, 0, aboveID, 1); ok {
		t.Fatal("precondition violated: specialize offered while the lowest life total is 15 (gate counts LE13)")
	}

	at, atID := specializeEngine(t, card, 0)
	at.G.Players[0].Life = 20
	at.G.Players[1].Life = 13
	addMana(t, at, 0, "CC")
	if _, ok := specializeOption(at, 0, atID, 1); !ok {
		t.Fatalf("specialize not offered with the lowest life total at 13: %+v", at.legalActions(0))
	}
}

// TestSpecializeLukaminaCorpusEndToEnd drives the real corpus card the whole
// way: front face enters, the specialization to the Crocodile form replaces
// the face (and its Mode$ Specializes trigger taps an opponent's nonland
// permanent), then the Crocodile death trigger runs the real compiled Mode$
// Unspecialize body, flips the card back to the front face, and its chained
// DBReturn returns it to the battlefield tapped.
func TestSpecializeLukaminaCorpusEndToEnd(t *testing.T) {
	lukamina := corpusCard(t, "Lukamina, Moon Druid")
	if got := len(lukamina.Faces); got != 6 {
		t.Fatalf("precondition: Lukamina faces = %d, want 6", got)
	}
	if lukamina.Faces[0].Name != "Lukamina, Moon Druid" || lukamina.Faces[2].Name != "Lukamina, Crocodile Form" {
		t.Fatalf("precondition: face names = %q / %q", lukamina.Faces[0].Name, lukamina.Faces[2].Name)
	}
	e, id := specializeEngine(t, lukamina, 6)
	o := e.G.Obj(id)
	if o.Zone != state.ZBattlefield || o.FaceIdx != 0 {
		t.Fatalf("precondition: zone=%s face=%d", o.Zone, o.FaceIdx)
	}
	// An opponent's nonland permanent for the Crocodile form's tap trigger.
	bears := corpusCard(t, "Grizzly Bears")
	victim := onBoardCard(t, e, 1, bears)
	if e.G.Obj(victim).Zone != state.ZBattlefield || e.G.Obj(victim).Tapped {
		t.Fatal("precondition: victim not an untapped battlefield permanent")
	}

	addMana(t, e, 0, "CCC") // pay {3}
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision after funding: %+v", d)
	}
	idx := -1
	for _, op := range d.Options {
		if op.Kind == "specialize" && op.Obj == id && op.Mode == "2" {
			idx = op.Index
		}
	}
	if idx < 0 {
		t.Fatalf("crocodile specialize option not offered: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if o.FaceIdx != 2 {
		t.Fatalf("FaceIdx = %d, want 2 (Crocodile Form)", o.FaceIdx)
	}
	if o.Face().Name != "Lukamina, Crocodile Form" {
		t.Fatalf("face = %q, want Crocodile Form", o.Face().Name)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after paying {3} = %d, want 0", e.G.Players[0].Pool.Total())
	}
	// The Crocodile form's "When this creature specializes" trigger is on the
	// new face; it must have been queued by the Specialize event.
	queued := false
	for _, pt := range e.PendingTriggers() {
		if pt.Source == id {
			queued = true
		}
	}
	if !queued && len(e.G.Stack) == 0 && !e.G.Obj(victim).Tapped {
		t.Fatalf("Crocodile 'specializes' trigger neither queued nor resolved (stack %v, triggers %v)",
			e.G.Stack, e.PendingTriggers())
	}
	// Resolve the trigger; its first option is the opponent's permanent.
	if len(e.G.Stack) == 0 {
		e.priorityRound()
	}
	resolveStack(t, e)
	if !e.G.Obj(victim).Tapped {
		t.Fatalf("Crocodile specialize trigger did not tap the opponent's permanent")
	}

	// Precondition for the return half: the Crocodile face really is the one
	// wearing the Unspecialize death trigger, so a build that flipped from the
	// wrong face could not reach it.
	if got := o.FaceIdx; got != 2 {
		t.Fatalf("precondition: card face before dying = %d, want 2 (Crocodile Form)", got)
	}

	// Death: the Crocodile death trigger runs the real compiled
	// `Mode$ Unspecialize` SA body, which flips the card back to its FRONT
	// face (Lukamina, Moon Druid) and then runs its chained DBReturn
	// (`ChangeZone | ConditionDefined$ Remembered | ConditionPresent$ Card |
	// Origin$ Graveyard | Destination$ Battlefield | Tapped$ True |
	// Defined$ Remembered`) to return the card tapped. The SetState remembers
	// the changed object (`RememberChanged$ True`), which IS the resolving
	// source; the plain-Remembered group now keeps that explicitly-remembered
	// source occurrence (effects/context.go rememberedWithSource == the one
	// resolution rule shared with rememberedLKIGroup), so the gate holds and
	// the return runs.
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if got := e.G.Obj(id).Zone; got != state.ZGraveyard {
		t.Fatalf("precondition: card zone after dying = %s, want Graveyard", got)
	}
	e.priorityRound()
	resolveStack(t, e)
	o = e.G.Obj(id)
	// The compiled Mode$ Unspecialize SA ran: it emits the "flips to face 0"
	// Note and a FlipFace, so the card wears the front face (NOT the
	// next-face walk, which from Crocodile would land on Scorpion).
	sawNote, sawFlip := false, false
	for _, ev := range e.L.Events {
		if ev.Obj != id {
			continue
		}
		if ev.Kind == events.Note && ev.Text == "flips to face 0 (Unspecialize)" {
			sawNote = true
		}
		if ev.Kind == events.FlipFace && ev.Amount == 0 {
			sawFlip = true
		}
	}
	if !sawNote || !sawFlip {
		t.Fatalf("the compiled Mode$ Unspecialize SA did not run (note=%v flip=%v)", sawNote, sawFlip)
	}
	if o.FaceIdx != 0 || o.Face().Name != "Lukamina, Moon Druid" {
		t.Fatalf("after unspecialize face = %d %q, want the front face", o.FaceIdx, o.Face().Name)
	}
	// The DBReturn half: the card must be back on the battlefield, tapped.
	if o.Zone != state.ZBattlefield {
		t.Fatalf("after DBReturn zone = %s, want Battlefield (graveyard return did not run)", o.Zone)
	}
	if !o.Tapped {
		t.Fatalf("after DBReturn the card is untapped, want tapped (Tapped$ True)")
	}
	// And the return must be event-sourced: a graveyard-to-battlefield
	// MoveZone for this id must be in the log.
	sawReturn := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZGraveyard && ev.To == state.ZBattlefield {
			sawReturn = true
		}
	}
	if !sawReturn {
		t.Fatal("DBReturn did not emit a Graveyard->Battlefield MoveZone for the card")
	}
}

// TestSpecializePlainCostVhalChoosesFace pins the 15 plain-cost files: Vhal
// offers one option per specialization face and the chosen one wins.
func TestSpecializePlainCostVhalChoosesFace(t *testing.T) {
	vhal := corpusCard(t, "Vhal, Eager Scholar")
	if len(vhal.Faces) != 6 {
		t.Fatalf("precondition: Vhal faces = %d, want 6", len(vhal.Faces))
	}
	e, id := specializeEngine(t, vhal, 0)
	addMana(t, e, 0, "CCCCC") // K:Specialize:5
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision: %+v", d)
	}
	offered := 0
	idx := -1
	for _, op := range d.Options {
		if op.Kind == "specialize" && op.Obj == id {
			offered++
			if op.Mode == "4" { // RED, Vhal, Scholar of Elements
				idx = op.Index
			}
		}
	}
	if offered != 5 {
		t.Fatalf("Vhal specialize options = %d, want one per five faces", offered)
	}
	if idx < 0 {
		t.Fatalf("no face-4 option: %+v", d.Options)
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: 0, Choices: []int{idx}}); err != nil {
		t.Fatal(err)
	}
	if got := e.G.Obj(id).Face().Name; got != "Vhal, Scholar of Elements" {
		t.Fatalf("Vhal face = %q, want Scholar of Elements (face 4)", got)
	}
}

// TestSpecializeUnsupportedRiderOffersNoOption pins the loud set: Imoen's
// ReduceCost$ X and Karlach's AdditionalActivationZone$ Graveyard are out of
// scope, so neither offers a specialize option (a silent merged-again or an
// ignored rider would offer one).
func TestSpecializeUnsupportedRiderOffersNoOption(t *testing.T) {
	for _, name := range []string{"Imoen, Trickster Friend", "Karlach, Raging Tiefling"} {
		c := corpusCard(t, name)
		if len(c.Faces) != 6 {
			t.Fatalf("precondition: %s faces = %d, want 6", name, len(c.Faces))
		}
		e, id := specializeEngine(t, c, 0)
		addMana(t, e, 0, "CCCCCCC")
		for _, o := range e.legalActions(0) {
			if o.Kind == "specialize" && o.Obj == id {
				t.Fatalf("%s offered a specialize option despite its unsupported rider: %+v", name, o)
			}
		}
	}
}

// TestSpecializeStaleGuardRejectsWhenGateBinds is the standing one-home
// livelock pin's negative half: a hand-crafted specialize option for a
// permanent whose gate is false is rejected by rules/priority_guard.go's
// priorityOptionStale, which reads the same specializeLegal predicate the
// offer walk does. A guard that disagreed with the gate would accept it.
func TestSpecializeStaleGuardRejectsWhenGateBinds(t *testing.T) {
	lukamina := corpusCard(t, "Lukamina, Moon Druid")
	e, id := specializeEngine(t, lukamina, 5)
	addMana(t, e, 0, "CCC")
	stale := decision.Option{Kind: "specialize", Obj: id, Mode: "2"}
	if msg := e.priorityOptionStale(0, stale); msg == "" {
		t.Fatal("stale guard accepted a specialize on a board where the gate is false")
	}
}

// TestSpecializeBotAnswerPassesValidator is the standing one-home livelock
// pin's positive half: on a gate-holding board the engine offers the option,
// the stale guard accepts exactly that option, and the bot's OWN answer --
// which it builds from the offered list -- clears both the guard and Submit.
func TestSpecializeBotAnswerPassesValidator(t *testing.T) {
	lukamina := corpusCard(t, "Lukamina, Moon Druid")
	e, id := specializeEngine(t, lukamina, 6)
	addMana(t, e, 0, "CCC")
	e.priorityRound()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("no priority decision: %+v", d)
	}
	opt, ok := specializeOption(e, 0, id, 2)
	if !ok {
		t.Fatalf("gate-holding board did not offer specialize: %+v", d.Options)
	}
	if msg := e.priorityOptionStale(0, opt); msg != "" {
		t.Fatalf("stale guard rejected an option the walk offered: %s", msg)
	}
	bot := newTestBot(0).answer(e, d)
	picked := -1
	for _, c := range bot.Choices {
		if d.Options[c].Kind == "specialize" {
			picked = c
		}
	}
	if picked < 0 {
		t.Fatalf("bot did not answer with the offered specialize: %+v", bot)
	}
	wantFace := d.Options[picked].Mode
	if err := e.Submit(bot); err != nil {
		t.Fatalf("engine rejected the bot's specialize answer: %v", err)
	}
	if got := strconv.Itoa(int(e.G.Obj(id).FaceIdx)); got != wantFace {
		t.Fatalf("bot answer specialized to face %d, want %s", e.G.Obj(id).FaceIdx, wantFace)
	}
}
