package rules

// Restores effects/namecard_choosefromdefined_test.go on the kernel:
// ChooseFromDefinedCards$ is strict -- a ValidCards$ filter the defined
// cards do not satisfy poses no name ask (the universe-totality fallback must
// not resurrect them) -- and an unrecognised Defined spelling fails closed
// rather than falling back to the SA source's own name. Each case carries a
// control proving the same wiring does ask; the control also carries
// effects/namecard_test.go's leaf (a universe-backed NameCard asks, and the
// answer is recorded as the Choose name event ChosenName folds).

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// kr2NameEngine is kr2Engine with the corpus as the name universe.
func kr2NameEngine(t *testing.T) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	return kr2EngineWith(t, 2, func(c *Config) { c.NameUniverse = reg.Universe() })
}

func TestNameCardChooseFromDefinedIsStrict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, valid string
		ask         bool
	}{{"control", "Card", true}, {"strict", "Card.nonLand", false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2NameEngine(t)
			kr2Put(t, e, 1, kr2Corpus(t, "Forest"), state.ZHand, false)
			spell := kr2Put(t, e, 0, kr2Sorcery(t, "Name Reader",
				"A:SP$ RevealHand | Defined$ Opponent | RememberRevealed$ True | SubAbility$ DBName",
				"SVar:DBName:DB$ NameCard | ValidCards$ "+tc.valid+" | ChooseFromDefinedCards$ Remembered"), state.ZHand, false)
			d := kr2Cast(t, e, 0, spell)
			if !tc.ask {
				if d != nil {
					t.Fatalf("strict: posed %+v -- the totality fallback resurrected the land", d)
				}
				if got := e.G.Obj(spell).ChosenName; got != "" {
					t.Fatalf("strict: chose %q with no eligible name", got)
				}
				return
			}
			d = kr2Want(t, d, "name")
			if labelIndex(d, "Forest") < 0 || len(d.Options) != 1 {
				t.Fatalf("control: options = %+v, want only the remembered Forest", d.Options)
			}
		})
	}
}

func TestNameCardChooseFromDefinedUnknownSelectorFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, line string
		ask        bool
	}{
		{"control", "A:SP$ RevealHand | Defined$ Opponent | RememberRevealed$ True | SubAbility$ DBName\nSVar:DBName:DB$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ Remembered", true},
		{"unknown selector", "A:SP$ NameCard | ValidCards$ Card.nonLand | ChooseFromDefinedCards$ NotARealSelector", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := kr2NameEngine(t)
			kr2Put(t, e, 1, kr2Corpus(t, "Counterspell"), state.ZHand, false)
			// The SA source is itself named like a legal nonland of the
			// universe that was never remembered: a source fallback would
			// offer it.
			spell := kr2Put(t, e, 0, kr2Src(t, "Name:Grizzly Bears\nManaCost:B\nTypes:Sorcery\n"+tc.line+"\nOracle:x\n"), state.ZHand, false)
			d := kr2Cast(t, e, 0, spell)
			if !tc.ask {
				if d != nil {
					t.Fatalf("unknown selector posed %+v -- the source fallback leaked a never-remembered name", d)
				}
				if got := e.G.Obj(spell).ChosenName; got != "" {
					t.Fatalf("unknown selector chose %q", got)
				}
				return
			}
			d = kr2Want(t, d, "name")
			if labelIndex(d, "Counterspell") < 0 || labelIndex(d, "Grizzly Bears") >= 0 {
				t.Fatalf("control: options = %+v, want the remembered Counterspell and never the source's name", d.Options)
			}
			if d = kr2Answer(t, e, d, labelIndex(d, "Counterspell")); d != nil {
				t.Fatalf("unexpected ask: %+v", d)
			}
			if got := e.G.Obj(spell).ChosenName; got != "Counterspell" {
				t.Fatalf("ChosenName = %q, want Counterspell", got)
			}
			found := false
			for _, ev := range e.L.Events {
				if ev.Kind == events.Choose && ev.Counter == "name" && ev.Text == "Counterspell" {
					found = true
				}
			}
			if !found {
				t.Fatal("no Choose name event recorded the answer")
			}
		})
	}
}
