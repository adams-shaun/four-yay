package rules

// Kernel-era restoration of revealchosen_cost_test.go's
// TestRevealChosenUnlessCostParsesAndPays: a RevealChosen<Spec> UnlessCost$
// parses, and paying it settles with one public Note naming the secret
// designation (paid path only; no designation declines). The trigger's
// direct resolveTop runs as a kernel probe (probe_test.go).

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// TestRevealChosenUnlessCostParsesAndPays pins the unless-path threading: a
// RevealChosen<Spec> as an UnlessCost$ parses (never the hard-decline a
// future shape must not mis-route through), and paying it settles through the
// shared beginUnlessPayment continuation -- one public Note naming the secret
// designation, emitted on the paid path only. The gate is
// hasRevealChosenDesignation on the source: no designation declines.
func TestKr8RevealChosenUnlessCostParsesAndPays(t *testing.T) {
	t.Parallel()
	// Grammar half: both spellings parse to a RevealChosen part with no
	// generic mana and no decline.
	for _, spec := range []string{"RevealChosen<Player>", "RevealChosen<Type/creature type>"} {
		c, ok := ParseUnlessCost(spec)
		if !ok {
			t.Fatalf("ParseUnlessCost(%q) declined; a RevealChosen unless cost must parse", spec)
		}
		if c.Generic != 0 || len(c.RevealChosen) != 1 {
			t.Fatalf("ParseUnlessCost(%q) = generic %d, revealChosen %v", spec, c.Generic, c.RevealChosen)
		}
	}
	if c := ParseCost("RevealChosen<Player>"); len(c.Unknown) != 0 {
		t.Fatalf("RevealChosen<Player> is in the Unknown census: %v", c.Unknown)
	}

	// Behaviour half: a synthetic trigger whose body carries the unless cost.
	for _, tc := range []struct {
		name       string
		designated bool
		pay        bool
		wantLife   int32
		wantNote   bool
	}{
		// Unswitched orientation: paying PREVENTS the GainLife body, so a
		// paid designation leaves life unchanged; declining (or paying with
		// no designation, which declines) runs the body for +3.
		{"designated and paid", true, true, 20, true},
		{"designated but declined", true, false, 23, false},
		{"no designation declines", false, true, 23, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := stealEngine(t, 909)
			src := onBoardCard(t, e, 0, card(t,
				"Name:Designation Tester\nTypes:Creature Human\nPT:2/2\n"+
					"T:Mode$ Phase | Phase$ Upkeep | ValidPlayer$ You | Execute$ TrigUnless\n"+
					"SVar:TrigUnless:DB$ GainLife | UnlessCost$ RevealChosen<Player> | LifeAmount$ 3\n"+
					"Oracle:x\n"))
			if tc.designated {
				// A player entry in Object.Chosen is what the real Secretly$
				// True ChoosePlayer flow records; a raw Choose event cannot
				// carry a player, so the fixture sets the field the same way
				// other fixtures place permanents.
				e.G.Obj(src).Chosen = []state.Target{{Player: 0, IsPlayer: true}}
			}
			e.emit(events.Event{Kind: events.TriggerPush, Obj: src, Player: 0, Amount: 0})
			// The trigger resolves as a kernel probe, which serves an ask
			// only with no other decision outstanding: clear the fixture's
			// open priority decision first, as a passed round would.
			e.pending = nil
			e.resolveTop()

			d := e.Pending()
			if d == nil || d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
				t.Fatalf("pending = %+v, want the unless-pay ask", d)
			}
			if tc.pay {
				submitChoices(t, e, d.Options[0].Index)
			} else {
				submitChoices(t, e, d.Options[1].Index)
			}
			passUntilQuiet(t, e, 40)

			if got := e.G.Players[0].Life; got != tc.wantLife {
				t.Fatalf("life = %d, want %d", got, tc.wantLife)
			}
			notes := notesWithPrefix(e, "revealed the chosen player:")
			if tc.wantNote && len(notes) != 1 {
				t.Fatalf("reveal Notes = %+v, want exactly one", notes)
			}
			if !tc.wantNote && len(notes) != 0 {
				t.Fatalf("reveal Notes = %+v, want none", notes)
			}
		})
	}
}
