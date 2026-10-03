package combat

import (
	"math"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// fakeBoard is a Board over a bare state.Game with no continuous effects
// and no statics: characteristics are the printed face, keywords a fixed
// per-object set. It proves the predicates run without an engine; the
// engine-level behaviour is pinned by rules' combat tests through the real
// adapter (rules/combat_board.go).
type fakeBoard struct {
	g  *state.Game
	kw map[state.ObjID][]Keyword
}

func (f *fakeBoard) Game() *state.Game   { return f.g }
func (f *fakeBoard) Log() []events.Event { return nil }
func (f *fakeBoard) IsCreature(id state.ObjID) bool {
	o := f.g.Obj(id)
	return o != nil && o.Face() != nil && o.Face().IsCreature()
}
func (f *fakeBoard) HasKW(id state.ObjID, kw Keyword) bool {
	for _, k := range f.kw[id] {
		if k == kw {
			return true
		}
	}
	return false
}
func (f *fakeBoard) Keywords(id state.ObjID) []string { return nil }
func (f *fakeBoard) Power(id state.ObjID) int32 {
	if o := f.g.Obj(id); o != nil && o.Face() != nil {
		return int32(o.Face().Power())
	}
	return 0
}
func (f *fakeBoard) ObjectColors(o *state.Object) string                          { return effects.ColorsOf(o) }
func (f *fakeBoard) ProtectedFrom(target, source state.ObjID) bool                { return false }
func (f *fakeBoard) Active() []state.ContinuousEffect                             { return nil }
func (f *fakeBoard) RestrictionApplies(*state.ContinuousEffect, state.ObjID) bool { return false }
func (f *fakeBoard) Statics(mode string) []Static                                 { return nil }
func (f *fakeBoard) StaticGateHolds(Static) bool                                  { return false }
func (f *fakeBoard) MatchesSpec(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID, rememberedPlayers []state.PlayerID) bool {
	sc := effects.NewSpecContext(you, source)
	for _, r := range remembered {
		sc.Remembered = append(sc.Remembered, state.Target{Obj: r})
	}
	sc.RememberedPlayers = rememberedPlayers
	return effects.MatchesSpecCtx(f.g, spec, id, sc)
}
func (f *fakeBoard) MatchesStaticSpec(spec string, id state.ObjID, sv Static) bool {
	return f.MatchesSpec(spec, id, sv.Source, sv.Controller, nil, nil)
}
func (f *fakeBoard) GoadMatches(spec string, id, source state.ObjID, you state.PlayerID, remembered []state.ObjID) bool {
	return f.MatchesSpec(spec, id, source, you, remembered, nil)
}
func (f *fakeBoard) PlayerSpecCtx(source state.ObjID) effects.PlayerSpecCtx {
	return effects.PlayerSpecCtx{Source: source}
}
func (f *fakeBoard) LandSpecCtx(defender state.PlayerID, attacker state.ObjID) effects.SpecContext {
	return effects.NewSpecContext(defender, attacker)
}

const bearText = "Name:Bear\nManaCost:1 G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// onBattlefield adds a hand-authored card to seat's battlefield (a test
// fixture: no engine, so no events to route through).
func onBattlefield(t *testing.T, g *state.Game, seat state.PlayerID, text string) state.ObjID {
	t.Helper()
	c, diags := cards.ParseBytes("fixture.txt", []byte(text))
	if c == nil {
		t.Fatalf("parse fixture: %v", diags)
	}
	o := g.AddObject(c, seat)
	id := o.ID
	g.SetZone(state.ZBattlefield, seat, append(g.Zone(state.ZBattlefield, seat), id))
	g.Obj(id).Zone = state.ZBattlefield
	return id
}

func TestCanAttackReadsOnlyTheBoard(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	b := &fakeBoard{g: g, kw: map[state.ObjID][]Keyword{}}
	ready := onBattlefield(t, g, 0, bearText)
	sick := onBattlefield(t, g, 0, bearText)
	g.Obj(sick).SummonSick = true
	hasty := onBattlefield(t, g, 0, bearText)
	g.Obj(hasty).SummonSick = true
	b.kw[hasty] = []Keyword{KWHaste}
	wall := onBattlefield(t, g, 0, bearText)
	b.kw[wall] = []Keyword{KWDefender}
	theirs := onBattlefield(t, g, 1, bearText)

	for _, c := range []struct {
		name string
		id   state.ObjID
		want bool
	}{
		{"untapped creature", ready, true},
		{"summoning sick", sick, false},
		{"summoning sick with haste", hasty, true},
		{"defender, nothing lifts the wall", wall, false},
		{"not the active player's", theirs, false},
	} {
		if got := CanAttack(b, c.id); got != c.want {
			t.Errorf("CanAttack(%s) = %v, want %v", c.name, got, c.want)
		}
		if got := CanAttackPair(b, c.id, 1); got != c.want {
			t.Errorf("CanAttackPair(%s, 1) = %v, want %v", c.name, got, c.want)
		}
	}
	g.Obj(ready).Tapped = true
	if CanAttack(b, ready) {
		t.Error("a tapped creature can attack")
	}
}

func TestCanBlockFlyingAndReach(t *testing.T) {
	g := state.NewGame([]string{"a", "b"})
	b := &fakeBoard{g: g, kw: map[state.ObjID][]Keyword{}}
	flier := onBattlefield(t, g, 0, bearText)
	b.kw[flier] = []Keyword{KWFlying}
	g.Obj(flier).IsAttacking, g.Obj(flier).Attacking = true, 1
	ground := onBattlefield(t, g, 1, bearText)
	reach := onBattlefield(t, g, 1, bearText)
	b.kw[reach] = []Keyword{KWReach}

	if CanBlock(b, ground, flier) {
		t.Error("a ground creature blocked a flier")
	}
	if !CanBlock(b, reach, flier) {
		t.Error("a reach creature could not block a flier")
	}
	g.Obj(reach).Tapped = true
	if CanBlock(b, reach, flier) {
		t.Error("a tapped creature blocked")
	}
	if got := LegalBlockerCount(b, flier, 1); got != 0 {
		t.Errorf("LegalBlockerCount = %d, want 0 (ground creature and a tapped reach)", got)
	}
	if got := DefenderCreatureCount(b, 1); got != 2 {
		t.Errorf("DefenderCreatureCount = %d, want 2", got)
	}
}

func TestRequirementSetCountsNamedDutiesSeparately(t *testing.T) {
	var s RequirementSet
	if s.Any() {
		t.Fatal("empty set binds")
	}
	s.addNamed(1)
	s.addNamed(1)
	s.addNamed(2)
	if !s.Any() || s.SatisfiedBy(1) != 2 || s.SatisfiedBy(2) != 1 || s.MaxNamed() != 2 {
		t.Fatalf("named counts wrong: %+v", s.Named)
	}
	if got := s.SatisfiedByOffer(1, 0); got != 2 {
		t.Errorf("player offer satisfies %d, want 2", got)
	}
	if got := s.SatisfiedByOffer(1, 7); got != 0 {
		t.Errorf("battle offer satisfies %d, want 0 (named duties name the player)", got)
	}
	s.Goad = true
	if got := s.SatisfiedByOffer(2, 0); got != 2 {
		t.Errorf("goaded player offer satisfies %d, want 2", got)
	}
}

func TestLandwalkSpecDropsTheDescription(t *testing.T) {
	for _, c := range []struct {
		in       string
		spec     string
		landwalk bool
	}{
		{"Landwalk:Island", "Island", true},
		{"Landwalk:Land.Snow:snow Land", "Land.Snow", true},
		{"Landwalk", "", true},
		{"Flying", "", false},
	} {
		spec, ok := LandwalkSpec(c.in)
		if spec != c.spec || ok != c.landwalk {
			t.Errorf("LandwalkSpec(%q) = %q, %v; want %q, %v", c.in, spec, ok, c.spec, c.landwalk)
		}
	}
}

func TestParseHiddenKeywordSpellings(t *testing.T) {
	for _, c := range []struct {
		in   string
		want HiddenKeywordFlags
	}{
		{"HIDDEN CARDNAME can't attack or block.", HiddenKeywordFlags{CantAttack: true, CantBlock: true}},
		{"CantAttackOrBlock", HiddenKeywordFlags{CantAttack: true, CantBlock: true}},
		{"CARDNAME can't attack.", HiddenKeywordFlags{CantAttack: true}},
		{"HIDDEN CARDNAME can't block.", HiddenKeywordFlags{CantBlock: true}},
		{"HIDDEN CARDNAME must be blocked if able.", HiddenKeywordFlags{MustBlock: true}},
		{"MustBlock", HiddenKeywordFlags{MustBlock: true}},
		{"CARDNAME can't block unless it's your turn.", HiddenKeywordFlags{}},
	} {
		if got := ParseHiddenKeyword(c.in); got != c.want {
			t.Errorf("ParseHiddenKeyword(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestAttackCeilingMatchesParseAmount(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int32
	}{
		{"", math.MaxInt32},
		{"2", 2},
		{" 1 ", 1},
		{"+3", 3},
		{"-1", math.MaxInt32},
		{"X", math.MaxInt32},
		{"99999999999", math.MaxInt32},
	} {
		if got := attackCeiling(c.in); got != c.want {
			t.Errorf("attackCeiling(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
