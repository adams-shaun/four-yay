package effects

// The three level-B spell-cast filter predicates this ticket added, each
// pinned through the SAME table the matcher and the UnknownPredicates census
// classify through, so the two cannot disagree (effects/filter.go's
// predicates map; never edited separately).
//
//   - singleTarget: the spell on the stack carries exactly one chosen target
//     (Spinerock Tyrant's `ValidSA$ Instant.singleTarget,Sorcery.singleTarget`).
//   - prepared: the cast rode CR 722.3c's prepared-copy grant
//     (Codie, Ravenous Codex's `ValidCard$ Card.prepared`).
//   - ManaCostPartialBlue: the printed mana cost carries at least one blue
//     mana symbol, hybrid and Phyrexian forms included (Namor the
//     Sub-Mariner's `Card.nonCreature+ManaCostPartialBlue`).

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

func TestSingleTargetPredicateCountsExactlyOneTarget(t *testing.T) {
	g, id := board(t)
	// The predicate evaluates the spell ON THE STACK: move the fixture
	// instant there, which the Spell base reads (zone, not type line).
	o := g.Obj(id["myInstant"])
	o.Zone = state.ZStack
	g.SetZone(state.ZStack, 0, []state.ObjID{id["myInstant"]})
	// Zero targets ("up to one target" cast with none chosen) does not match.
	if MatchesSpec(g, "Instant.singleTarget", id["myInstant"], 0) {
		t.Fatal("a targetless spell matched singleTarget")
	}
	if MatchesSpec(g, "Spell.singleTarget", id["myInstant"], 0) {
		t.Fatal("a targetless spell matched singleTarget through the Spell base")
	}
	o.Targets = []state.Target{{Obj: id["myBear"]}}
	if !MatchesSpec(g, "Instant.singleTarget", id["myInstant"], 0) {
		t.Fatal("a one-target spell did not match singleTarget")
	}
	if !MatchesSpec(g, "Spell.singleTarget", id["myInstant"], 0) {
		t.Fatal("the Spell base must not gate the predicate")
	}
	if MatchesSpec(g, "Instant.!singleTarget", id["myInstant"], 0) {
		t.Fatal("!singleTarget must not match a one-target spell")
	}
	o.Targets = append(o.Targets, state.Target{Player: 1, IsPlayer: true})
	if MatchesSpec(g, "Instant.singleTarget", id["myInstant"], 0) {
		t.Fatal("a two-target spell matched singleTarget")
	}
	if !MatchesSpec(g, "Instant.!singleTarget", id["myInstant"], 0) {
		t.Fatal("!singleTarget must match a two-target spell")
	}
	if un := UnknownPredicates("Instant.singleTarget,Sorcery.singleTarget"); len(un) != 0 {
		t.Fatalf("UnknownPredicates(Spinerock Tyrant's ValidSA$) = %v, want empty", un)
	}
}

func TestPreparedPredicateReadsTheCastProvenanceBit(t *testing.T) {
	g, id := board(t)
	if MatchesSpec(g, "Card.prepared", id["myArtifact"], 0) {
		t.Fatal("an ordinary cast matched Card.prepared")
	}
	// The prepared source permanent's own IsPrepared status is NOT the
	// predicate: the CR 722.3c fold clears it before the deferred SpellCast
	// trigger evaluates the stack object, so the status read would fail the
	// one cast the mechanic exists to make.
	g.Obj(id["myArtifact"]).Prepared = true
	if !MatchesSpec(g, "Card.IsPrepared", id["myArtifact"], 0) {
		t.Fatal("precondition: the IsPrepared status predicate reads the designation")
	}
	if MatchesSpec(g, "Card.prepared", id["myArtifact"], 0) {
		t.Fatal("the prepared DESIGNATION alone must not match Card.prepared")
	}
	g.Obj(id["myArtifact"]).CastFlags |= state.FlagPreparedCopy
	if !MatchesSpec(g, "Card.prepared", id["myArtifact"], 0) {
		t.Fatal("a FlagPreparedCopy cast did not match Card.prepared")
	}
	if MatchesSpec(g, "Card.!prepared", id["myArtifact"], 0) {
		t.Fatal("!prepared must not match a prepared-copy cast")
	}
	if un := UnknownPredicates("Card.prepared"); len(un) != 0 {
		t.Fatalf("UnknownPredicates(Card.prepared) = %v, want empty", un)
	}
}

func TestManaCostPartialBlueReadsThePrintedCost(t *testing.T) {
	g := state.NewGame([]string{"you", "them"})
	mk := func(name, cost string) state.ObjID {
		c := parseTestCard(t, "Name:"+name+"\nManaCost:"+cost+"\nTypes:Instant\nOracle:x\n")
		return g.AddObject(c, 0).ID
	}
	blue := mk("Blue", "2 U")
	hybrid := mk("Hybrid", "2U")       // {2/U}
	phyrexian := mk("Phyrexian", "UP") // {U/P}
	other := mk("Other", "1 G")
	colourless := mk("Colourless", "3")
	creature := parseTestCard(t, "Name:Blue Creature\nManaCost:U\nTypes:Creature Faerie\nPT:1/1\nOracle:x\n")
	bear := g.AddObject(creature, 0).ID
	for _, tc := range []struct {
		name string
		id   state.ObjID
		want bool
	}{
		{"one blue pip", blue, true},
		{"hybrid {2/U}", hybrid, true},
		{"Phyrexian {U/P}", phyrexian, true},
		{"no blue", other, false},
		{"colourless only", colourless, false},
	} {
		if got := MatchesSpec(g, "Card.ManaCostPartialBlue", tc.id, 0); got != tc.want {
			t.Errorf("%s: ManaCostPartialBlue = %v, want %v", tc.name, got, tc.want)
		}
	}
	// Namor's full spec: the conjunction with nonCreature reads both halves.
	if !MatchesSpec(g, "Card.nonCreature+ManaCostPartialBlue", blue, 0) {
		t.Error("nonCreature+ManaCostPartialBlue did not match a blue instant")
	}
	if MatchesSpec(g, "Card.nonCreature+ManaCostPartialBlue", bear, 0) {
		t.Error("nonCreature+ManaCostPartialBlue matched a creature")
	}
	if un := UnknownPredicates("Card.nonCreature+ManaCostPartialBlue"); len(un) != 0 {
		t.Fatalf("UnknownPredicates(Namor's ValidCard$) = %v, want empty", un)
	}
}

// parseTestCard parses one freely-authored fixture face the way board() does.
func parseTestCard(t testing.TB, src string) *cards.Card {
	t.Helper()
	c, diags := cards.ParseBytes("t.txt", []byte(src))
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	c.Link()
	for _, f := range c.Faces {
		f.ApplyIntrinsics()
	}
	return c
}
