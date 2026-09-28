package rules

// Task: the COUNT-level trailing `$<Property>` suffix on the
// Count$ThisTurnCast_<spec> branch was unread. Rootha, Mastering the Moment's
// SVar Y (`Count$ThisTurnCast_Instant.YouCtrl,Sorcery.YouCtrl$GreatestCardManaCost`)
// sizes her Elemental token from the number of instants cast instead of the
// greatest mana value among the instant and sorcery spells cast this turn;
// April O'Neil, Hacktivist's `Count$ThisTurnCast_Card.YouCtrl$CardTypes`
// draws zero cards. Both bodies are evaluated here through the REAL cast
// machinery (castMode + finishCast), never a hand-built event log.

import (
	"testing"

	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// roothaYBody is the exact SVar body Rootha's token-sizing Y carries, pasted
// verbatim from her Forge script (GPL: inline in the test, never committed as
// a file).
const roothaYBody = "Count$ThisTurnCast_Instant.YouCtrl,Sorcery.YouCtrl$GreatestCardManaCost"

// aprilXBody is April O'Neil's SVar X, pasted verbatim.
const aprilXBody = "Count$ThisTurnCast_Card.YouCtrl$CardTypes"

// castSuffixSpell puts one inline instant or sorcery in seat 0's hand at main
// phase and casts it for real, funding the pool with enough generic mana.
func castSuffixSpell(t *testing.T, e *Engine, src string) state.ObjID {
	t.Helper()
	id := state.ObjID(0)
	for _, hid := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(hid); o != nil && o.Face() != nil && o.Face().Name == srcName(t, src) {
			id = hid
		}
	}
	if id == 0 {
		t.Fatalf("spell not in hand: %s", srcName(t, src))
	}
	e.G.Players[0].Pool[state.MC] = 10
	e.G.Players[0].Pool[state.MW], e.G.Players[0].Pool[state.MU] = 5, 5
	e.G.Players[0].Pool[state.MB], e.G.Players[0].Pool[state.MR] = 5, 5
	e.G.Players[0].Pool[state.MG] = 5
	castMode(t, e, id, "")
	finishCast(t, e, id)
	// The precondition the count reads: the cast actually went through the
	// stack and resolved (an instant/sorcery lands in the graveyard, a
	// creature on the battlefield), and rules' log walk can see it.
	if o := e.G.Obj(id); o == nil || o.Zone == state.ZStack || o.Zone == state.ZHand {
		t.Fatalf("spell %s did not resolve off the stack: %+v", srcName(t, src), o)
	}
	if len(e.EachSpellCastThisTurnMatching(0, "Card.YouCtrl", 0)) == 0 {
		t.Fatal("no cast this turn recorded in the log -- the count would read vauously")
	}
	return id
}

func srcName(t testing.TB, src string) string {
	t.Helper()
	return card(t, src).Faces[0].Name
}

// TestRoothaGreatestManaValueAmongInstantsAndSorceries is the brief's primary
// leaf: two differently sized boards, each casting one instant and one
// sorcery of DIFFERENT mana values, then evaluating Rootha's exact Y body.
// The greatest mana value must win. On the buggy build the body counts the
// one instant (an alternative counts instants) -- never the greatest -- and a
// sorcery-only board counts 0, so the two boards differ from the want and
// from each other.
func TestRoothaGreatestManaValueAmongInstantsAndSorceries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		instantSrc    string
		sorcerySrc    string
		want          int32
		wantInstantMV int32
		sorceryMV     int32
	}{
		// A 3-MV instant and a 5-MV sorcery: the greatest is 5. The buggy
		// build reads the ONE instant's mana value (1 counted spell) or the
		// plain instant count -- both != 5.
		{name: "instant3_sorcery5", instantSrc: "Name:Probe Bolt\nManaCost:2 R\nTypes:Instant\nOracle:x\n",
			sorcerySrc: "Name:Probe Geyser\nManaCost:4 R\nTypes:Sorcery\nOracle:x\n", want: 5, wantInstantMV: 3, sorceryMV: 5},
		// A 1-MV instant and a 3-MV sorcery: the greatest is 3. A hard-coded 5
		// from the first board cannot pass.
		{name: "instant1_sorcery3", instantSrc: "Name:Spark Bolt\nManaCost:R\nTypes:Instant\nOracle:x\n",
			sorcerySrc: "Name:Brief Counsel\nManaCost:2 U\nTypes:Sorcery\nOracle:x\n", want: 3, wantInstantMV: 1, sorceryMV: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := handEngine(t, card(t, tc.instantSrc), card(t, tc.sorcerySrc))
			// Precondition: the two casts' mana values actually differ, so a
			// greatest fold is distinguishable from a count or a min.
			if tc.wantInstantMV == tc.sorceryMV {
				t.Fatalf("test setup: instant MV %d == sorcery MV %d, fold unprovable", tc.wantInstantMV, tc.sorceryMV)
			}
			inst := castSuffixSpell(t, e, tc.instantSrc)
			sorc := castSuffixSpell(t, e, tc.sorcerySrc)
			if got := e.G.Obj(inst).Face().ManaValue(); got != tc.wantInstantMV {
				t.Fatalf("precondition: instant MV = %d, want %d", got, tc.wantInstantMV)
			}
			if got := e.G.Obj(sorc).Face().ManaValue(); got != tc.sorceryMV {
				t.Fatalf("precondition: sorcery MV = %d, want %d", got, tc.sorceryMV)
			}
			if n := len(e.EachSpellCastThisTurnMatching(0, "Instant.YouCtrl,Sorcery.YouCtrl", 0)); n != 2 {
				t.Fatalf("precondition: matching casts this turn = %d, want 2 (the spec must see both)", n)
			}
			ctx := &effects.Ctx{Controller: 0}
			got, ok := effects.EvalCountOK(e, ctx, roothaYBody)
			if !ok {
				t.Fatalf("Rootha's Y body (%q) did not evaluate", roothaYBody)
			}
			if got != tc.want {
				t.Fatalf("Rootha's Y = %d, want %d (the greatest mana value among the instant and sorcery spells cast this turn; instants %d, sorcery %d)",
					got, tc.want, tc.wantInstantMV, tc.sorceryMV)
			}
		})
	}
}

// TestAprilOneilCardTypesAmongSpellsCast is the second leaf: two spells of two
// different card types (an instant and a creature) make April's X read 2; a
// single-card-type board reads 1. On the buggy build the whole
// `YouCtrl$CardTypes` token is unknown and both read 0.
func TestAprilOneilCardTypesAmongSpellsCast(t *testing.T) {
	t.Parallel()
	const creatureSrc = "Name:Probe Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"
	t.Run("two_card_types", func(t *testing.T) {
		e := handEngine(t, card(t, "Name:Probe Spark\nManaCost:U\nTypes:Instant\nOracle:x\n"), card(t, creatureSrc))
		castSuffixSpell(t, e, "Name:Probe Spark\nManaCost:U\nTypes:Instant\nOracle:x\n")
		castSuffixSpell(t, e, creatureSrc)
		// Precondition: the two casts are genuinely different card types, so a
		// distinct-type count differs from the cast count.
		if n := len(e.EachSpellCastThisTurnMatching(0, "Card.YouCtrl", 0)); n != 2 {
			t.Fatalf("precondition: matching casts this turn = %d, want 2", n)
		}
		got, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, aprilXBody)
		if !ok {
			t.Fatalf("April's X body (%q) did not evaluate", aprilXBody)
		}
		if got != 2 {
			t.Fatalf("April's X = %d, want 2 (distinct card types: Instant + Creature)", got)
		}
	})
	t.Run("one_card_type", func(t *testing.T) {
		e := handEngine(t, card(t, "Name:Probe Spark\nManaCost:U\nTypes:Instant\nOracle:x\n"))
		castSuffixSpell(t, e, "Name:Probe Spark\nManaCost:U\nTypes:Instant\nOracle:x\n")
		if n := len(e.EachSpellCastThisTurnMatching(0, "Card.YouCtrl", 0)); n != 1 {
			t.Fatalf("precondition: matching casts this turn = %d, want 1", n)
		}
		got, ok := effects.EvalCountOK(e, &effects.Ctx{Controller: 0}, aprilXBody)
		if !ok {
			t.Fatalf("April's X body (%q) did not evaluate", aprilXBody)
		}
		if got != 1 {
			t.Fatalf("April's X = %d, want 1 (one distinct card type)", got)
		}
	})
}
