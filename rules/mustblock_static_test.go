package rules

// stat:MustBlock — the blocker's CR 509.1a duty "this creature blocks this
// turn if able." The MODE and its battlefield-static read have been enforced
// since combatrestriction1 (rules/combat/block.go MustBlockCandidates, first
// loop, ValidCreature$ through activeStatics), but the CORPUS spells the duty
// through an Effect: Culvert Ambusher and Hustle // Bustle both deliver
// `StaticAbilities$ MustBlock` with `ValidCreature$ Card.IsRemembered` from an
// `AB$/SP$ Effect` body, so the duty arrives as a registered CONTINUOUS
// restriction, not a printed battlefield static. MustBlockCandidates' second
// loop then consulted restrictionApplies, which only knew ValidCard$/ValidTarget$/
// ValidCards$ -- none of which the shape carries -- and fell through to the
// `len(Remembered) > 0` blanket, marking EVERY creature on the defender's board
// as required to block rather than just the remembered one.
//
// This test pins the continuous-validcreature read directly, and the coverage
// test pins that the primitive is declared supported so the cards clear the
// skip gate. Both leaves assert their own preconditions.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/rules/combat"
	"github.com/adams-shaun/gorge/state"
)

// TestMustBlockContinuousRestrictionScopesToRemembered pins the Effect-
// delivered shape: a registered MustBlock restriction with ValidCreature$
// Card.IsRemembered and exactly one remembered creature marks that creature's
// duty and no other.
func TestMustBlockContinuousRestrictionScopesToRemembered(t *testing.T) {
	t.Parallel()
	e := threeSeatEngine(t)
	// The Effect's source is a resolved instant sitting on the battlefield for
	// the test's purposes; its identity only matters as ce.Source.
	src := onBoardCard(t, e, 0, card(t, "Name:MustBlock Effect Source\nManaCost:1\nTypes:Enchantment\nOracle:x\n"))
	remembered := onBoardCard(t, e, 0, card(t, "Name:Duty Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))
	other := onBoardCard(t, e, 0, card(t, "Name:Bystander Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"))

	// The corpus shape: `Mode$ MustBlock | ValidCreature$ Card.IsRemembered`.
	e.AddContinuous(state.ContinuousEffect{
		Source:         src,
		Controller:     0,
		Restriction:    "MustBlock",
		RestrictParams: map[string]string{"ValidCreature": "Card.IsRemembered"},
		Remembered:     []state.ObjID{remembered},
		UntilEOT:       true,
	})

	// Preconditions: the restriction is live and remembers exactly one id, and
	// both creatures are real creatures on seat 0's battlefield.
	live := 0
	for _, ce := range e.active() {
		if ce.Restriction == "MustBlock" {
			live++
			if len(ce.Remembered) != 1 || ce.Remembered[0] != remembered {
				t.Fatalf("precondition: restriction remembers %v, want exactly [%d]", ce.Remembered, remembered)
			}
		}
	}
	if live != 1 {
		t.Fatalf("precondition: %d live MustBlock restrictions, want 1", live)
	}
	if !e.IsCreature(remembered) || !e.IsCreature(other) || remembered == other {
		t.Fatalf("precondition: both distinct creatures must be on the battlefield")
	}

	req := combat.MustBlockCandidates(asBoard(e), 0)
	if !req[remembered] {
		t.Fatalf("remembered creature not marked required: %v", req)
	}
	if req[other] {
		t.Fatalf("bystander creature wrongly marked required (ValidCreature$ Card.IsRemembered not scoped): %v", req)
	}
}

// TestMustBlockPrimitiveIsRegistered pins the support declaration the skip gate
// reads: without it every corpus carrier of `Mode$ MustBlock` is rejected as
// unsupported even though the rules enforce it.
func TestMustBlockPrimitiveIsRegistered(t *testing.T) {
	if !effects.Supported()["stat:MustBlock"] {
		t.Fatal(`effects.Supported() is missing "stat:MustBlock"`)
	}
}

// mustBlockCarriers is the class census the brief demands: every corpus face
// whose compiled statics carry `Mode$ MustBlock`, pinned so a corpus-pin bump
// that adds a new carrier fails loudly rather than silently joining an
// untested set. Names are the face names (a split card contributes both
// faces).
var mustBlockCarriers = []string{
	"A-Shessra, Death's Whisper", "Academic Dispute", "Berserker's Frenzy",
	"Boros Battleshaper", "Brutal Hordechief", "Courtly Provocateur",
	"Culling Mark", "Culvert Ambusher", "Domineering Will", "Grand Melee",
	"Hustle", "Invasion Plans", "Iron Golem", "Khârn the Betrayer",
	"Mark for Death", "Nacatl Hunt-Pride", "Peema Aether-Seer",
	"Predatory Rampage", "Provoke", "Razorgrass Screen", "Relentless Raptor",
	"Shessra, Death's Whisper", "Spirespine", "Targeting Rocket",
	"Timely Interference", "Watchdog", "You've Been Caught Stealing",
}

// TestMustBlockClassCensus pins the corpus-wide carrier set of Mode$ MustBlock.
func TestMustBlockClassCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]bool{}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			hit := false
			for _, st := range f.Statics {
				if st.Mode == "MustBlock" {
					hit = true
				}
			}
			// The Effect-delivered carriers (Culvert Ambusher, Hustle // Bustle)
			// live in an SVar named by `StaticAbilities$`, reached only through
			// EachRawEffectChild -- the same source Face.Primitives reads for the
			// stat: coverage token, so the census and the skip gate cannot
			// disagree about which cards carry the mode.
			f.EachRawEffectChild(func(ch cards.EffectChild) {
				if ch.Static != nil && ch.Static.Mode == "MustBlock" {
					hit = true
				}
			})
			if hit {
				got[f.Name] = true
			}
		}
	}
	list := make([]string, 0, len(got))
	for n := range got {
		list = append(list, n)
	}
	sort.Strings(list)
	want := append([]string(nil), mustBlockCarriers...)
	sort.Strings(want)
	if len(list) != len(want) {
		t.Fatalf("Mode$ MustBlock carriers: got %d, want %d\ngot:  %s\nwant: %s", len(list), len(want), joinQuoted(list), joinQuoted(want))
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("Mode$ MustBlock carrier %d = %q, want %q\ngot:  %s\nwant: %s", i, list[i], want[i], joinQuoted(list), joinQuoted(want))
		}
	}
}

func joinQuoted(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += "\"" + v + "\""
	}
	return out
}
