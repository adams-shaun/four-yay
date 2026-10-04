package rules

// Restores effects/look_test.go on the kernel: every looker-scoped effect
// records its look as exactly one Secret Note scoped to the entitled looker
// (a library-top look carrying its Text clause), the Dig look carries the
// whole window, and Slayer's Bounty's RevealType$ Creature look shows only
// the creature cards. (Liar's Pendulum's may-reveal leaf is
// TestMayRevealHandAskSuspendsAndDeclineRevealsNothing /
// TestMayRevealHandAcceptEmitsThePublicReveal.)

import (
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// kr2AnswerAll answers every remaining non-priority decision: targets with
// seat tgt, anything else with its first Min options (at least one).
func kr2AnswerAll(t *testing.T, e *Engine, d *decision.Decision, tgt state.PlayerID) {
	t.Helper()
	for i := 0; d != nil && i < 20; i++ {
		if d.Kind == decision.KTarget {
			d = kr2Answer(t, e, d, kr2PlayerIdx(t, d, tgt))
			continue
		}
		n := d.Min
		if n < 1 && len(d.Options) > 0 && d.Kind != decision.KArrange {
			n = 1
		}
		if d.Kind == decision.KArrange {
			n = d.Max
		}
		picks := make([]int, 0, n)
		for j := 0; j < n && j < len(d.Options); j++ {
			picks = append(picks, d.Options[j].Index)
		}
		d = kr2Answer(t, e, d, picks...)
	}
	if d != nil {
		t.Fatalf("decisions never settled: %+v", d)
	}
}

func TestEveryLookerScopedEffectRecordsItsLook(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, line string
		hand       bool
		window     int // library cards the look must carry (0: not checked)
	}{
		{"RevealHand Look$", "A:SP$ RevealHand | ValidTgts$ Player | Look$ True", true, 0},
		{"Dig", "A:SP$ Dig | Defined$ You | DigNum$ 3 | ChangeNum$ 1 | ChangeValid$ Land", false, 3},
		{"RearrangeTopOfLibrary", "A:SP$ RearrangeTopOfLibrary | Defined$ You | NumCards$ 2", false, 0},
		{"Scry", "A:SP$ Scry | Defined$ You | ScryNum$ 2", false, 0},
		{"Surveil", "A:SP$ Surveil | Defined$ You | Amount$ 2", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2Engine(t, 2)
			var hand []state.ObjID
			for _, n := range []string{"Bear", "Isle", "Bolt"} {
				hand = append(hand, kr2Put(t, e, 1, kr2Src(t, "Name:"+n+"\nTypes:Sorcery\nOracle:x\n"), state.ZHand, false))
			}
			window := append([]state.ObjID(nil), e.G.Zone(state.ZLibrary, 0)...)
			spell := kr2Put(t, e, 0, kr2Sorcery(t, "Looker", tc.line), state.ZHand, false)
			from := len(e.L.Events)
			d := kr2Cast(t, e, 0, spell)
			if d == nil {
				t.Fatal("no ask posed alongside the look")
			}
			kr2AnswerAll(t, e, d, 1)
			// A library look over an arrange ask carries its cards in the
			// decision, so its Note may carry no ids: count every Secret Note.
			var notes []events.Event
			for _, ev := range kr2Events(e, from, events.Note) {
				if ev.Secret {
					notes = append(notes, ev)
				}
			}
			if len(notes) != 1 {
				t.Fatalf("got %d Secret look Notes (%+v), want exactly one", len(notes), notes)
			}
			n := notes[0]
			if n.Player != 0 {
				t.Fatalf("look Note Player = %d, want the entitled looker (seat 0)", n.Player)
			}
			if n.Text == "" && n.From == state.ZLibrary {
				t.Fatalf("a library-top look must carry its Text clause, got %+v", n)
			}
			if tc.hand && !slices.Equal(n.IDs, hand) {
				t.Fatalf("look ids = %v, want the target's whole hand %v", n.IDs, hand)
			}
			if tc.window > 0 && !slices.Equal(n.IDs, window[:tc.window]) {
				t.Fatalf("look ids = %v, want the whole window %v (every card looked at, not only the eligible ones)", n.IDs, window[:tc.window])
			}
		})
	}
}

func TestSlayersBountyLookShowsOnlyCreatureCards(t *testing.T) {
	t.Parallel()
	e := kr2Engine(t, 2)
	bear := kr2Put(t, e, 1, kr2Src(t, "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"), state.ZHand, false)
	kr2Put(t, e, 1, kr2Src(t, "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"), state.ZHand, false)
	kr2Put(t, e, 1, kr2Src(t, "Name:Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n"), state.ZHand, false)
	bounty := kr2Put(t, e, 0, kr2Corpus(t, "Slayer's Bounty"), state.ZHand, false)
	from := len(e.L.Events)
	d := kr2Cast(t, e, 0, bounty)
	for i := 0; d != nil && d.Kind == decision.KTarget && i < 3; i++ {
		d = kr2Answer(t, e, d, kr2PlayerIdx(t, d, 1))
	}
	d = kr2Want(t, d, "look_ack")
	if d.Player != 0 || !strings.Contains(d.Prompt, "Bear") {
		t.Fatalf("ack = %+v, want a look_ack for seat 0 naming the creature card", d)
	}
	if d = kr2Answer(t, e, d, 0); d != nil {
		t.Fatalf("unexpected ask after the ack: %+v", d)
	}
	notes := kr2SecretLooks(e, from)
	if len(notes) != 1 {
		t.Fatalf("got %d Secret look Notes (%+v), want exactly one", len(notes), notes)
	}
	if n := notes[0]; n.Player != 0 || n.From != state.ZHand || !slices.Equal(n.IDs, []state.ObjID{bear}) {
		t.Fatalf("look Note = %+v, want a Secret hand look for seat 0 carrying only the creature %d", n, bear)
	}
}
