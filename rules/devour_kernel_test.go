package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

func TestGorgerWurmDevourAsksAndCountsTheSacrificed(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Gorger Wurm"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	fodder := devourFodder(t, e, "Grizzly Bears", "Grizzly Bears")

	d := kr5EnterDevourer(t, e, "Gorger Wurm")
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("on the wurm's entry: %+v, want a KChoose sacrifice ask", d)
	}
	if d.ResumeKind != "sacrifice" || d.Min != 0 || d.Max != 2 || d.Player != 0 {
		t.Fatalf("sacrifice ask: resume %q range %d..%d player %d, want sacrifice 0..2 player 0 (Optional$, 2 eligible)",
			d.ResumeKind, d.Min, d.Max, d.Player)
	}
	if len(d.Options) != len(fodder) {
		t.Fatalf("ask has %d options, want one per battlefield creature (%d)", len(d.Options), len(fodder))
	}
	for i, o := range d.Options {
		if o.Obj != fodder[i] {
			t.Fatalf("option %d = obj %d, want battlefield creature %d", i, o.Obj, fodder[i])
		}
	}

	// Sacrifice BOTH, in the reverse of zone order, so the answer -- not a
	// deterministic first-N pick -- is what lands.
	submitChoices(t, e, d.Options[1].Index, d.Options[0].Index)
	if got := e.G.Obj(idOf(t, e, "Gorger Wurm")).Counter("P1P1"); got != 2 {
		t.Fatalf("Gorger Wurm entered with %d P1P1, want 2 (one per devoured creature, Devour 1)", got)
	}
	for _, id := range fodder {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("answered creature %d not in the graveyard: %+v", id, o)
		}
	}
}

// TestGorgerWurmDevourDeclineEntersWithoutCounters pins the Optional half:
// answering nothing is legal and leaves the wurm a plain 5/5.
func TestGorgerWurmDevourDeclineEntersWithoutCounters(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Gorger Wurm"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	fodder := devourFodder(t, e, "Grizzly Bears")

	d := kr5EnterDevourer(t, e, "Gorger Wurm")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("on the wurm's entry: %+v, want the sacrifice ask", d)
	}
	submitChoices(t, e)
	if got := e.G.Obj(idOf(t, e, "Gorger Wurm")).Counter("P1P1"); got != 0 {
		t.Fatalf("declined wurm entered with %d P1P1, want 0", got)
	}
	if o := e.G.Obj(fodder[0]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("decline left the creature the battlefield: %+v", o)
	}
}

// TestThunderThrashElderDevourThreeCountsSixPerCreature pins the Times.N
// multiplier: Devour 3 with two devoured creatures enters with six counters.
func TestThunderThrashElderDevourThreeCountsSixPerCreature(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Thunder-Thrash Elder"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	fodder := devourFodder(t, e, "Grizzly Bears", "Grizzly Bears")

	d := kr5EnterDevourer(t, e, "Thunder-Thrash Elder")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Min != 0 || d.Max != 2 {
		t.Fatalf("sacrifice ask: %+v, want sacrifice 0..2", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	if got := e.G.Obj(idOf(t, e, "Thunder-Thrash Elder")).Counter("P1P1"); got != 6 {
		t.Fatalf("Thunder-Thrash Elder entered with %d P1P1, want 6 (2 creatures x Devour 3)", got)
	}
	_ = fodder
}

// TestThromokDevourXSquaresTheCount pins the X amount (count-plus-svar-operand,
// round 3): `Times.X` NOW resolves the SVar operand, and Forge's own script
// is `SVar:X:Count$RememberedSize` -- so the expansion's
// Count$RememberedSize/Times.X reads n², which is the card oracle: "This
// creature enters with X +1/+1 counters on it for each of those creatures"
// = n per devoured creature (X = the number devoured). The old
// pin (2 for 2 devoured) held only while the operand parser left Times.X
// unresolved -- a silent zero contribution, the defect this ticket fixes.
func TestThromokDevourXSquaresTheCount(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Thromok the Insatiable"),
		corpusAlternativeCard(t, "Grizzly Bears"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	devourFodder(t, e, "Grizzly Bears", "Grizzly Bears")

	d := kr5EnterDevourer(t, e, "Thromok the Insatiable")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("sacrifice ask: %+v, want the KChoose sacrifice ask", d)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	if got := e.G.Obj(idOf(t, e, "Thromok the Insatiable")).Counter("P1P1"); got != 4 {
		t.Fatalf("Thromok entered with %d P1P1, want 4 (Devour X, 2 devoured: X per creature = n²)", got)
	}
}

// TestFeastingHobbitDevourFiltersFoods pins the typed valid: only Foods are
// offered and counted (SacValid$ Food.Other / Count$Valid Food.YouCtrl+Other);
// a non-Food creature on the same battlefield is not sacrificed.
func TestFeastingHobbitDevourFiltersFoods(t *testing.T) {
	t.Parallel()
	e := handEngine(t,
		corpusAlternativeCard(t, "Feasting Hobbit"),
		corpusAlternativeCard(t, "Gingerbrute"),
		corpusAlternativeCard(t, "Grizzly Bears"))
	fodder := devourFodder(t, e, "Gingerbrute", "Grizzly Bears")

	d := kr5EnterDevourer(t, e, "Feasting Hobbit")
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" || d.Min != 0 || d.Max != 1 {
		t.Fatalf("sacrifice ask: %+v, want sacrifice 0..1 (only the Food is eligible)", d)
	}
	if len(d.Options) != 1 || d.Options[0].Obj != fodder[0] {
		t.Fatalf("ask options %+v, want only the Food (Gingerbrute)", d.Options)
	}
	submitChoices(t, e, d.Options[0].Index)
	if got := e.G.Obj(idOf(t, e, "Feasting Hobbit")).Counter("P1P1"); got != 3 {
		t.Fatalf("Feasting Hobbit entered with %d P1P1, want 3 (1 Food x Devour 3)", got)
	}
	if o := e.G.Obj(fodder[0]); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("the Food is not in the graveyard: %+v", o)
	}
	if o := e.G.Obj(fodder[1]); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("the non-Food creature left the battlefield: %+v", o)
	}
}

// kr5EnterDevourer is enterDevourer under the resolution kernel: the
// devourer's hand->battlefield entry runs as a kernel probe from a quiet
// engine, so the ETB replacement's sacrifice ask is posed and its answer
// re-executes the entry with the choice served.
func kr5EnterDevourer(t *testing.T, e *Engine, name string) *decision.Decision {
	t.Helper()
	id := searchMoveByName(t, e, name, state.ZHand)
	e.pending = nil
	e.probe(func() {
		e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZBattlefield})
	})
	return e.Pending()
}
