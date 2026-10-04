package rules

// Kernel-era restoration of TestReplacementOrderAnswerRunsBodyChainOutsideParkedResolution
// (replacement_order_parked_body_test.go): a CR 616.1 damage-replacement order
// choice posed while Zap resolves; the chosen body's SubAbility$ rider must
// run and the amounts must compose in the answered order. The legacy
// e.resume / contChain internals the old test also inspected no longer exist.

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

func TestReplacementOrderAnswerRunsBodyChainOutsideParkedResolutionKernel(t *testing.T) {
	t.Parallel()
	for _, first := range []string{"Doubler", "Amplifier"} {
		t.Run(first+"_first", func(t *testing.T) {
			e, cfg, _ := newFixtureDeck(t, 91, parkedOrderZap, parkedOrderDoubler, parkedOrderAmplifier)
			doubler := putCreature(t, e, 0, parkedOrderDoubler)
			amplifier := putCreature(t, e, 0, parkedOrderAmplifier)
			addMana(t, e, 0, "R")
			life0, life1 := e.G.Players[0].Life, e.G.Players[1].Life
			castCardNow(t, e, "Zap")
			for i := 0; i < 16; i++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no pending decision while resolving Zap")
				}
				if d.Kind == decision.KReplacement {
					break
				}
				if d.Kind != decision.KPriority {
					t.Fatalf("unexpected %s decision %q before the order choice", d.Kind, d.Prompt)
				}
				submitChoices(t, e, passIndex(t, d))
			}
			pick := doubler
			if first == "Amplifier" {
				pick = amplifier
			}
			pickOption(t, e, pick)
			if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
				t.Fatalf("after the order answer pending = %+v, want priority", d)
			}
			want := int32(3) // (1*2)+1
			if first == "Amplifier" {
				want = 4 // (1+1)*2
			}
			if got := life1 - e.G.Players[1].Life; got != want {
				t.Fatalf("opponent lost %d life, want %d", got, want)
			}
			if got := e.G.Players[0].Life - life0; got != 1 {
				t.Fatalf("seat 0 gained %d life, want 1: the Amplifier body's SubAbility$ rider was skipped", got)
			}
			if o := e.G.Obj(spellID(e, "Zap")); o == nil || o.Zone == state.ZStack {
				t.Fatal("Zap is still on the stack after its damage settled")
			}
			replayCheck(t, e, cfg)
		})
	}
}
