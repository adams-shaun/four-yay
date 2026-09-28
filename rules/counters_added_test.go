package rules

import (
	"fmt"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCountersAddedThisTurn(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	tarfire := onBoardCard(t, e, 0, yourCountersCard(t, "Lasting Tarfire"))
	creature := onBoardCard(t, e, 0, yourCountersCard(t, "Wakka, Devoted Guardian"))
	other := onBoardCard(t, e, 1, yourCountersCard(t, "Wakka, Devoted Guardian"))
	// This creature belongs to seat 0 but receives a seat-1-caused counter.
	// It distinguishes the actor's Player spelling (any player) from You.
	crossActor := onBoardCard(t, e, 0, yourCountersCard(t, "Wakka, Devoted Guardian"))
	if e.G.Obj(tarfire).Zone != state.ZBattlefield || e.G.Obj(creature).Zone != state.ZBattlefield {
		t.Fatal("test precondition: sources are not on the battlefield")
	}
	body := svarBodyOf(t, e.G.Obj(tarfire).Face(), "X")
	if body != "Count$CountersAddedThisTurn Any You Creature" {
		t.Fatalf("test precondition: Lasting Tarfire X = %q", body)
	}
	ctx := &effects.Ctx{Controller: 0, Source: tarfire}
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 0 {
		t.Fatalf("baseline = %d (ok %v), want evaluated zero", n, ok)
	}

	// Publish the real adder role, as cost/turn-based placement sites do.
	prev := e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: creature, Counter: "P1P1", Amount: 2})
	e.SetCounterAdder(prev)
	if n, ok := effects.EvalCountOK(e, ctx, body); !ok || n != 2 {
		t.Fatalf("matching placement = %d (ok %v), want 2", n, ok)
	}
	// A placement by seat 1 and a non-creature placement are isolated.
	prev = e.SetCounterAdder(1)
	e.emit(events.Event{Kind: events.CounterChange, Obj: other, Counter: "P1P1", Amount: 7})
	e.emit(events.Event{Kind: events.CounterChange, Obj: crossActor, Counter: "P1P1", Amount: 5})
	e.SetCounterAdder(prev)
	prev = e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: tarfire, Counter: "LORE", Amount: 3})
	e.SetCounterAdder(prev)
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 2 {
		t.Fatalf("isolated placements changed You Creature count to %d", n)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn LORE You Card.Self"); !ok || n != 3 {
		t.Fatalf("Card.Self LORE count = %d (ok %v), want 3", n, ok)
	}
	// Player includes the seat-1 adder while You does not; both placements
	// landed on seat-0 creatures, so this is also the Creature.YouCtrl form.
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 Player Permanent.YouCtrl"); !ok || n != 7 {
		t.Fatalf("Player Permanent.YouCtrl count = %d (ok %v), want 7", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 Player Creature.YouCtrl"); !ok || n != 7 {
		t.Fatalf("Player Creature.YouCtrl count = %d (ok %v), want 7", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 You Creature.YouCtrl"); !ok || n != 2 {
		t.Fatalf("You Creature.YouCtrl count = %d (ok %v), want 2", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn Any You Creature"); !ok || n != 2 {
		t.Fatalf("Any count = %d (ok %v), want 2", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 You Creature"); !ok || n != 2 {
		t.Fatalf("P1P1 matching creature count = %d (ok %v), want 2", n, ok)
	}

	// Summon Brynhildr's grammar: the real compiled SVar, evaluated with a
	// LORE placement on the effect source itself. A different kind names the
	// same source and reads zero (the two compared values differ).
	bryn := onBoardCard(t, e, 0, yourCountersCard(t, "Summon Brynhildr"))
	brynBody := svarBodyOf(t, e.G.Obj(bryn).Face(), "X")
	if brynBody != "Count$CountersAddedThisTurn LORE You Card.EffectSource" {
		t.Fatalf("test precondition: Summon Brynhildr X = %q", brynBody)
	}
	brynCtx := &effects.Ctx{Controller: 0, Source: bryn}
	if n, ok := effects.EvalCountOK(e, brynCtx, brynBody); !ok || n != 0 {
		t.Fatalf("Card.EffectSource baseline = %d (ok %v), want evaluated zero", n, ok)
	}
	prev = e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: bryn, Counter: "LORE", Amount: 1})
	e.SetCounterAdder(prev)
	if n, ok := effects.EvalCountOK(e, brynCtx, brynBody); !ok || n != 1 {
		t.Fatalf("Card.EffectSource LORE count = %d (ok %v), want 1", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, brynCtx, "Count$CountersAddedThisTurn P1P1 You Card.EffectSource"); !ok || n != 0 {
		t.Fatalf("Card.EffectSource kind isolation = %d (ok %v), want 0", n, ok)
	}

	// The ledger reads the PLACEMENT-TIME snapshot, not the live object:
	// transfer the counted creature to seat 1, then move it off the
	// battlefield -- both specs keep reading what the object WAS when the
	// counter was put on it. The Any-Creature body now reads 3 (Wakka's
	// P1P1 2 plus Brynhildr's LORE 1 -- the Saga is an Enchantment Creature),
	// and that 3 must survive both live-state changes untouched.
	e.emit(events.Event{Kind: events.ControlChange, Obj: creature, Player: 1})
	if live := e.G.Obj(creature); live == nil || live.Controller != 1 || live.Zone != state.ZBattlefield {
		t.Fatalf("test precondition: creature not transferred to seat 1 on the battlefield (controller %v zone %v)", live.Controller, live.Zone)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 Player Permanent.YouCtrl"); !ok || n != 7 {
		t.Fatalf("post-control-transfer Player YouCtrl count = %d (ok %v), want 7 from the placement-time snapshot", n, ok)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: creature, From: state.ZBattlefield, To: state.ZGraveyard})
	if live := e.G.Obj(creature); live == nil || live.Zone != state.ZGraveyard {
		t.Fatalf("test precondition: creature not moved to the graveyard (zone %v)", live.Zone)
	}
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 3 {
		t.Fatalf("post-zone-move Any You Creature count = %d, want 3 from the pre-event snapshots", n)
	}

	clone := e.Clone()
	if n, _ := effects.EvalCountOK(clone, ctx, body); n != 3 {
		t.Fatalf("clone count = %d, want 3", n)
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: 2})
	if n, _ := effects.EvalCountOK(e, ctx, body); n != 0 {
		t.Fatalf("reset count = %d, want 0", n)
	}
	if n, _ := effects.EvalCountOK(clone, ctx, body); n != 3 {
		t.Fatalf("clone changed when original reset: %d", n)
	}

	// Case-insensitive kind read (the same read the sibling removed head
	// takes), on the fresh turn so every arithmetic above is untouched: a
	// lower-case ledger entry of the kind sums into the upper-case query,
	// and the kinds stay isolated (the pre-reset LORE entry is gone).
	prev = e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: tarfire, Counter: "p1p1", Amount: 4})
	e.SetCounterAdder(prev)
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 You Card.Self"); !ok || n != 4 {
		t.Fatalf("case-insensitive kind read = %d (ok %v), want 4", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn LORE You Card.Self"); !ok || n != 0 {
		t.Fatalf("post-reset kind isolation = %d (ok %v), want 0", n, ok)
	}
}

func TestLastingTarfire(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	tarfire := onBoardCard(t, e, 0, yourCountersCard(t, "Lasting Tarfire"))
	creature := onBoardCard(t, e, 0, yourCountersCard(t, "Wakka, Devoted Guardian"))
	face := e.G.Obj(tarfire).Face()
	trig := face.Triggers[0]
	if trig.Mode != "Phase" || trig.Params["Phase"] != "End of Turn" || trig.Params["CheckSVar"] != "X" || trig.Params["Execute"] != "TrigDamage" {
		t.Fatalf("test precondition: Lasting Tarfire trigger = %+v", trig)
	}
	// The compiled Execute body is the claimed 2-damage-to-each-opponent
	// rider -- without it the gate below would prove nothing about WHICH
	// effect the CheckSVar arms.
	if body := svarBodyOf(t, face, "TrigDamage"); body != "DB$ DealDamage | NumDmg$ 2 | Defined$ Opponent" {
		t.Fatalf("test precondition: Lasting Tarfire TrigDamage = %q", body)
	}
	prev := e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: creature, Counter: "P1P1", Amount: 1})
	e.SetCounterAdder(prev)
	if n, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0, Source: tarfire}, "Count$CountersAddedThisTurn Any You Creature"); !ok || n != 1 {
		t.Fatalf("matching trigger count = %d (ok %v), want 1", n, ok)
	}
	for i := range e.G.Players {
		e.G.Players[i].Life = 20
	}
	e.pending = nil
	e.setStep(state.StepEnd)
	e.priorityRound()
	if e.Pending() != nil {
		choices := make([]int, len(e.Pending().Options))
		for i, opt := range e.Pending().Options {
			choices[i] = opt.Index
		}
		submitChoices(t, e, choices...)
	}
	passUntilStackEmpty(t, e, 80)
	if e.G.Players[1].Life != 18 {
		t.Fatalf("Lasting Tarfire opponent life = %d, want 18", e.G.Players[1].Life)
	}

	// Control: the same compiled trigger with no matching placement must not
	// manufacture a damage trigger.
	e2 := layerEngine(t)
	onBoardCard(t, e2, 0, yourCountersCard(t, "Lasting Tarfire"))
	e2.pending = nil
	e2.setStep(state.StepEnd)
	e2.priorityRound()
	if e2.G.Players[1].Life != 20 {
		t.Fatalf("nonmatching Lasting Tarfire control life = %d, want 20", e2.G.Players[1].Life)
	}
}

// TestCountersAddedThisTurnIgnoresInternalStatusMarkers pins the emit-gate
// marker exclusion over BOTH attribution routes. The Deathtouched half is
// the real resolution path: a fight trigger resolves with the aura's
// controller as actionCause, and the fighter's deathtouch hit marks the
// SURVIVING victim (effects/damage.go) -- a positive CounterChange the
// unguarded ledger attributed to seat 0 and counted. The Shield half is the
// published-adder route a regeneration grant uses. A status marker is not a
// counter a player put.
func TestCountersAddedThisTurnIgnoresInternalStatusMarkers(t *testing.T) {
	const auraSrc = "Name:Deathblessing\nManaCost:G\nTypes:Enchantment Aura\nK:Enchant:Creature.YouCtrl\n" +
		"S:Mode$ Continuous | Affected$ Creature.EnchantedBy | AddKeyword$ Deathtouch | Description$ x\n" +
		"T:Mode$ ChangesZone | Origin$ Any | Destination$ Battlefield | ValidCard$ Card.Self | Execute$ TrigFight | TriggerDescription$ x\n" +
		"SVar:TrigFight:DB$ Fight | Defined$ Enchanted | ValidTgts$ Creature.YouDontCtrl | TargetMin$ 0 | TargetMax$ 1\n"
	e, cfg, aura := newFixtureDeck(t, 8, auraSrc,
		"Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:1/3\nOracle:x\n")
	ox := putCreature(t, e, 0, "Name:Ox\nManaCost:1 G\nTypes:Creature Ox\nPT:1/3\nOracle:x\n")
	bear := putToken(t, e, 1, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/5\nOracle:x\n", state.ZBattlefield)
	addMana(t, e, 0, "G")
	opt := castOptionFor(t, e, aura)
	submitChoices(t, e, opt.Index)
	if d := e.Pending(); d == nil || d.Kind != decision.KTarget || d.Options[0].Obj != ox {
		t.Fatalf("aura cast target ask = %+v", e.Pending())
	}
	submitChoices(t, e, 0)
	fightDrain(t, e, false)
	// Precondition: the resolving fight emitted the Deathtouched mark on the
	// victim (effects/damage.go's rider) and the mark did its CR 704.5g work
	// -- one point of fighter deathtouch damage destroyed the 2/5 Bear, so
	// the exact positive marker event the emit gate saw is read from the
	// log (the live token has ceased; the mark lasts only as long as the
	// damage it accompanied).
	marked := false
	for _, ev := range e.L.Events {
		if ev.Kind == events.CounterChange && ev.Obj == bear && ev.Counter == "Deathtouched" && ev.Amount == 1 {
			marked = true
		}
	}
	if !marked {
		t.Fatal("test precondition: the resolving deathtouch fight emitted no Deathtouched marker")
	}
	if o := e.G.Obj(bear); o == nil || o.Zone == state.ZBattlefield {
		t.Fatal("test precondition: the marked Bear did not die to the deathtouch SBA")
	}
	ctx := &effects.Ctx{Controller: 0, Source: ox}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn Any You Creature"); !ok || n != 0 {
		t.Fatalf("resolving deathtouch marker counted = %d (ok %v), want 0 (a status marker is not a placement)", n, ok)
	}
	// The Shield half: a published-adder regeneration marker never counts.
	prev := e.SetCounterAdder(0)
	e.emit(events.Event{Kind: events.CounterChange, Obj: ox, Counter: "Shield", Amount: 1})
	e.SetCounterAdder(prev)
	if e.G.Obj(ox).Counter("Shield") != 1 {
		t.Fatal("test precondition: the Shield marker did not land")
	}
	if n, _ := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn Any You Creature"); n != 0 {
		t.Fatalf("regeneration Shield marker counted = %d, want 0", n)
	}
	replayCheck(t, e, cfg)
}

// TestCountersAddedThisTurnLedgerReplays pins the ledger's rebuild
// discipline on a REAL intent-driven game: a bot-driven repo-deck game is
// stopped at its first non-marker positive object-counter placement, and the
// recorded (Config, Log) is re-run through the package-local replayFor
// helper (the same verified rebuild replay.ReplayTo serves undo, DVR and
// restart from). The rebuilt engine must hold the identical ledger -- same
// entry count, same count-head answers -- since every placement's adder is
// re-derived by the re-executed driving code, never carried by the event.
func TestCountersAddedThisTurnLedgerReplays(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck := testutil.RepoDeck(t, reg, "avengers-assemble")
	cfg := Config{Seed: 3, Names: []string{"a", "b"}, Decks: [][]*cards.Card{deck, deck}, Tokens: reg.Tokens}
	e := New(cfg)
	b := newTestBot(3)
	e.Advance()
	found := false
	for n := 0; !e.G.Over && e.Pending() != nil && n < 40000; n++ {
		before := len(e.L.Events)
		if err := e.Submit(b.answer(e, e.Pending())); err != nil {
			t.Fatalf("intent %d rejected: %v", n, err)
		}
		for _, ev := range e.L.Events[before:] {
			if ev.Kind == events.CounterChange && ev.Obj != 0 && ev.Amount > 0 && !state.InternalCounterMarker(ev.Counter) {
				found = true
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("test precondition: no positive object-counter placement within the intent cap")
	}
	if len(e.counterAddsThisTurn) == 0 {
		t.Fatal("test precondition: the original engine's ledger is empty at the placement")
	}
	src := e.G.Obj(e.G.Zone(state.ZBattlefield, 0)[0])
	if src == nil {
		t.Fatal("test precondition: seat 0 has no battlefield object to anchor the count ctx")
	}
	heads := []string{
		"Count$CountersAddedThisTurn Any You Creature",
		"Count$CountersAddedThisTurn P1P1 Player Permanent.YouCtrl",
		"Count$CountersAddedThisTurn LORE You Card.Self",
		"Count$CountersAddedThisTurn Any Player Card.Self",
	}
	live := map[string]int32{}
	for _, p := range []state.PlayerID{0, 1} {
		c := &effects.Ctx{Controller: p, Source: src.ID}
		for _, body := range heads {
			n, ok := effects.EvalCountOK(e, c, body)
			if !ok {
				t.Fatalf("head %q unresolvable for seat %d", body, p)
			}
			live[fmt.Sprintf("%d|%s", p, body)] = n
		}
	}
	re, err := replayFor(cfg, e.L)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if got := len(re.counterAddsThisTurn); got != len(e.counterAddsThisTurn) {
		t.Fatalf("replayed ledger entries = %d, want %d", got, len(e.counterAddsThisTurn))
	}
	for _, p := range []state.PlayerID{0, 1} {
		c := &effects.Ctx{Controller: p, Source: src.ID}
		if o := re.G.Obj(src.ID); o == nil {
			t.Fatalf("test precondition: replay engine lost object %d", src.ID)
		}
		for _, body := range heads {
			n, ok := effects.EvalCountOK(re, c, body)
			if !ok {
				t.Fatalf("replayed head %q unresolvable for seat %d", body, p)
			}
			if want := live[fmt.Sprintf("%d|%s", p, body)]; n != want {
				t.Fatalf("replayed %q for seat %d = %d, original %d", body, p, n, want)
			}
		}
	}
}

// TestCountersAddedThisTurnMalformedStaysUnresolvable pins the fail-closed
// read: a malformed (two-part) body stays unresolvable -- CheckSVar
// distinguishes that from an evaluated zero. This is a CONVENTION pin, not a
// fix-sensitive test: an unknown head is unresolvable with or without this
// ticket's dispatch arm (the sibling CountersRemovedThisTurn behaves
// identically), so it does not belong in the failure proof. The
// fix-sensitive proof for the dispatch arm is
// TestCountersAddedThisTurn/TestLastingTarfire (plus the Brynhildr
// EffectSource and replay pins above), and for the emit-gate marker
// exclusion TestCountersAddedThisTurnIgnoresInternalStatusMarkers.
func TestCountersAddedThisTurnMalformedStaysUnresolvable(t *testing.T) {
	t.Parallel()
	e := layerEngine(t)
	tarfire := onBoardCard(t, e, 0, yourCountersCard(t, "Lasting Tarfire"))
	ctx := &effects.Ctx{Controller: 0, Source: tarfire}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn Any You"); ok || n != 0 {
		t.Fatalf("malformed count = %d (ok %v), want unresolvable", n, ok)
	}
	if n, ok := effects.EvalCountOK(e, ctx, "Count$CountersAddedThisTurn P1P1 You Creature Extra"); ok || n != 0 {
		t.Fatalf("over-long count = %d (ok %v), want unresolvable", n, ok)
	}
	// Every field is a closed measured grammar. An unknown kind, player
	// qualifier, or object filter must not be laundered into an evaluated
	// zero: CheckSVar gives that result a different meaning.
	for _, body := range []string{
		"Count$CountersAddedThisTurn AGE You Creature",
		"Count$CountersAddedThisTurn Any You.UnknownQualifier Creature",
		"Count$CountersAddedThisTurn Any You NotAForgeObjectSpec",
	} {
		if n, ok := effects.EvalCountOK(e, ctx, body); ok || n != 0 {
			t.Fatalf("unsupported count %q = %d (ok %v), want unresolvable", body, n, ok)
		}
	}
}
