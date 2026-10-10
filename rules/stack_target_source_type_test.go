package rules

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// An ability on the stack has no card characteristics of its own; a
// card-type qualifier in a ValidTgts$ naming it (Echo, Perceptive Prodigy's
// `Card.Creature`, Scientist Supreme of A.I.M.'s bare `Artifact`) is judged
// against the ability's SOURCE (CR 113.7a), by one helper both the offer and
// the resolution recheck call (matchesTargetSpec).

// srcTypeArtifactTapperSrc is a plain `{T}: draw` artifact: the artifact-source
// ability probe.
const srcTypeArtifactTapperSrc = "Name:Whip\nManaCost:1\nTypes:Artifact\n" +
	"A:AB$ Draw | Cost$ T | Defined$ You | SpellDescription$ Tap: draw a card.\nOracle:x\n"

// srcTypeArtifactSpellSrc is an artifact spell card: the wrong-type spell control for
// a `Card.Creature` filter.
const srcTypeArtifactSpellSrc = "Name:Trinket\nManaCost:2\nTypes:Artifact\nOracle:x\n"

func srcTypeCopyAbilitySA(t *testing.T, validTgts, targetType string) *cards.SA {
	t.Helper()
	src := "Name:Copier\nManaCost:1\nTypes:Creature\nPT:1/1\n" +
		"A:AB$ CopySpellAbility | Cost$ 1 T | TargetType$ " + targetType + " | ValidTgts$ " + validTgts +
		" | MayChooseTarget$ True | SpellDescription$ copy.\nOracle:x\n"
	return card(t, src).Faces[0].Abilities[0]
}

type srcTypeFixture struct {
	e                                *Engine
	creatureAbility, artifactAbility state.ObjID // seat 0's own
	oppCreatureAbility               state.ObjID // seat 1's
	creatureSpell, artifactSpell     state.ObjID
	creatureSource, artifactSource   state.ObjID
}

func newSrcTypeFixture(t *testing.T) srcTypeFixture {
	t.Helper()
	e := counterHands(t, nil, nil,
		[]*cards.Card{card(t, tapperSrc), card(t, srcTypeArtifactTapperSrc)},
		[]*cards.Card{card(t, tapperSrc)})
	bf0 := e.G.Zone(state.ZBattlefield, 0)
	f := srcTypeFixture{e: e, creatureSource: bf0[0], artifactSource: bf0[1]}
	oppSource := e.G.Zone(state.ZBattlefield, 1)[0]
	mint := func(src state.ObjID, ctl state.PlayerID) state.ObjID {
		o := e.G.AddObject(nil, ctl)
		o.Ability = e.G.Obj(src).Face().Abilities[0]
		o.Source = src
		o.Zone = state.ZStack
		return o.ID
	}
	f.creatureAbility = mint(f.creatureSource, 0)
	f.artifactAbility = mint(f.artifactSource, 0)
	f.oppCreatureAbility = mint(oppSource, 1)
	cs := e.G.AddObject(card(t, grizzlySrc), 0)
	cs.Zone = state.ZStack
	as := e.G.AddObject(card(t, srcTypeArtifactSpellSrc), 0)
	as.Zone = state.ZStack
	f.creatureSpell, f.artifactSpell = cs.ID, as.ID
	e.G.SetZone(state.ZStack, 0, []state.ObjID{f.creatureAbility, f.artifactAbility, f.oppCreatureAbility, cs.ID, as.ID})
	// Preconditions the assertions below depend on: the two sources really
	// carry different card types, so a type filter can tell them apart.
	if !e.matchesSpec("Creature", f.creatureSource, e.specCtx(f.creatureSource, 0)) ||
		e.matchesSpec("Artifact", f.creatureSource, e.specCtx(f.creatureSource, 0)) ||
		!e.matchesSpec("Artifact", f.artifactSource, e.specCtx(f.artifactSource, 0)) ||
		e.matchesSpec("Creature", f.artifactSource, e.specCtx(f.artifactSource, 0)) {
		t.Fatal("precondition: creature source must be a non-artifact creature and artifact source a non-creature artifact")
	}
	return f
}

// TestStackTargetSourceTypeFilter: the census over a creature-source and an
// artifact-source ability of the chooser's own (plus a creature spell, an
// artifact spell and an opponent's creature-source ability) honours a card-type
// qualifier on the ability's SOURCE, leaves the spell candidates' own
// face-read unchanged, and keeps the controller qualifier on the wrapper.
func TestStackTargetSourceTypeFilter(t *testing.T) {
	t.Parallel()
	f := newSrcTypeFixture(t)
	const abil = "Activated.YouCtrl,Triggered.YouCtrl"
	const spellAbil = "Spell,Activated.YouCtrl,Triggered.YouCtrl"
	cases := []struct {
		name, validTgts, targetType string
		want                        []state.ObjID
	}{
		{"Card offers both own abilities", "Card", abil, []state.ObjID{f.creatureAbility, f.artifactAbility}},
		{"Card.Creature offers only the creature-source ability", "Card.Creature", abil, []state.ObjID{f.creatureAbility}},
		{"Artifact offers only the artifact-source ability", "Artifact", abil, []state.ObjID{f.artifactAbility}},
		{"Card.Artifact offers only the artifact-source ability", "Card.Artifact", abil, []state.ObjID{f.artifactAbility}},
		{"Card.nonCreature offers only the artifact-source ability", "Card.nonCreature", abil, []state.ObjID{f.artifactAbility}},
		{"Card.Creature with a spell kind: creature spell + creature ability, not the artifact spell",
			"Card.Creature", spellAbil, []state.ObjID{f.creatureAbility, f.creatureSpell}},
		{"Artifact with a spell kind: artifact spell + artifact ability",
			"Artifact", spellAbil, []state.ObjID{f.artifactAbility, f.artifactSpell}},
	}
	for _, c := range cases {
		got := censusOf(t, f.e, 0, srcTypeCopyAbilitySA(t, c.validTgts, c.targetType))
		slices.Sort(got)
		want := slices.Clone(c.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: census %v, want %v", c.name, got, want)
		}
	}
	// The opponent's creature-source ability is never offered under .YouCtrl,
	// whatever the type filter says (the controller qualifier stays on the
	// wrapper): precondition it IS a creature-source ability.
	if o := f.e.G.Obj(f.oppCreatureAbility); o.Controller != 1 || !f.e.matchesSpec("Creature", o.Source, f.e.specCtx(o.Source, 1)) {
		t.Fatalf("precondition: opponent ability %+v is not a seat-1 creature-source ability", o)
	}
	for _, vt := range []string{"Card", "Card.Creature"} {
		if got := censusOf(t, f.e, 0, srcTypeCopyAbilitySA(t, vt, "Activated.YouCtrl,Triggered.YouCtrl")); slices.Contains(got, f.oppCreatureAbility) {
			t.Errorf("ValidTgts$ %s offered the opponent's ability under .YouCtrl: %v", vt, got)
		}
	}
	// Without the controller qualifier the opponent's ability is offered to a
	// `Card.Creature` filter (it is a creature-source ability).
	if got := censusOf(t, f.e, 0, srcTypeCopyAbilitySA(t, "Card.Creature", "Activated,Triggered")); !slices.Contains(got, f.oppCreatureAbility) {
		t.Errorf("unqualified Activated,Triggered Card.Creature census %v missing the opponent's creature-source ability %d", got, f.oppCreatureAbility)
	}
}

// TestStackTargetSourceTypeRecheck: the resolution recheck (legalTargets) runs
// the same judge: a target offered under `Card.Creature` stays legal at
// resolution, an artifact-source ability is dropped, and a creature-source
// ability whose source stopped being a creature mid-race is dropped.
func TestStackTargetSourceTypeRecheck(t *testing.T) {
	t.Parallel()
	f := newSrcTypeFixture(t)
	sa := srcTypeCopyAbilitySA(t, "Card.Creature", "Activated.YouCtrl,Triggered.YouCtrl")
	offered := censusOf(t, f.e, 0, sa)
	if !slices.Contains(offered, f.creatureAbility) {
		t.Fatalf("precondition: census %v does not offer the creature-source ability %d", offered, f.creatureAbility)
	}
	zones := targetZones(sa)
	if len(zones) != 1 || zones[0] != state.ZStack {
		t.Fatalf("precondition: target zones %v, want [Stack]", zones)
	}
	recheck := func(ids ...state.ObjID) []state.Target {
		var ts []state.Target
		for _, id := range ids {
			ts = append(ts, state.Target{Obj: id})
		}
		// self is the (absent) resolving copy; source the copier permanent.
		return f.e.legalTargets(ts, sa, zones, 0, f.creatureSource, 0)
	}
	got := recheck(f.creatureAbility, f.artifactAbility)
	if len(got) != 1 || got[0].Obj != f.creatureAbility {
		t.Fatalf("recheck kept %v, want only the creature-source ability %d", got, f.creatureAbility)
	}
	// Mid-race: the ability is re-pointed at the artifact source, so its
	// source is no longer a creature (the same judge must drop it).
	f.e.G.Obj(f.creatureAbility).Source = f.artifactSource
	if got := recheck(f.creatureAbility); len(got) != 0 {
		t.Fatalf("recheck kept %v after the ability's source became a non-creature, want none", got)
	}
}
