package view

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

// richChars is flatChars with every derived fact the projection can carry
// switched on, so a reused View is exercised on the fields flatChars leaves
// empty: pending triggers (one optional), an optional stack ability, a
// nonzero availability, a library-top reveal for seat 0, potential actions
// with a placeholder label, a layer-3 name, an effective cost and a token
// suppressor that is OFF (tokens are formatted). Every slice it returns is
// fresh, as the real engine's are.
type richChars struct{ flatChars }

func (c richChars) Keywords(id state.ObjID) []string {
	if id%2 == 0 {
		return []string{"Flying", "Trample"}
	}
	return c.flatChars.Keywords(id)
}
func (c richChars) PendingTriggers() []state.PendingTrigger {
	return []state.PendingTrigger{
		{Source: 3, Controller: 0, Label: "a"},
		{Source: 5, Controller: 1, Label: "b", Optional: true, Decider: 1},
	}
}
func (c richChars) StackOptional(state.ObjID) (bool, state.PlayerID) { return true, 2 }
func (c richChars) AvailableMana(p state.PlayerID) state.Mana {
	var m state.Mana
	m[state.MG] = int32(p) + 1
	return m
}
func (c richChars) MayLookAtLibraryTop(p state.PlayerID) bool { return p == 0 }
func (c richChars) PotentialActions(p state.PlayerID) []decision.PotentialAction {
	return []decision.PotentialAction{{Kind: "cast", Obj: state.ObjID(p) + 1, Label: "Bear: CARDNAME attacks"}}
}
func (c richChars) Name(id state.ObjID) string {
	if id%3 == 0 {
		return "Renamed"
	}
	return ""
}
func (c richChars) SpellEffectiveCost(_ state.PlayerID, id state.ObjID) string {
	if id%4 == 0 {
		return "G"
	}
	return ""
}

// richBoard is fourSeatBoard dressed with every per-card and per-player
// field the projection fills: counters, an attacker with blockers, a
// face-down exile, a phased-out permanent, graveyard cards, a stack spell
// with targets and an ability, restricted mana, the initiative and a
// CR 720 controlled seat.
func richBoard(t *testing.T) *state.Game {
	t.Helper()
	g := fourSeatBoard(t)
	c, diags := cards.ParseBytes("rich.txt", []byte("Name:Elf\nManaCost:G\nTypes:Creature Elf Druid\nPT:1/1\nA:AB$ Mana | Cost$ T | Produced$ G | SpellDescription$ Add {G}.\nOracle:x\n"))
	if len(diags) != 0 {
		t.Fatalf("parsing: %v", diags)
	}
	c.Link()
	bf0 := g.Zone(state.ZBattlefield, 0)
	for i := 0; i < 3; i++ {
		o := g.AddObject(c, 0)
		o.Zone = state.ZBattlefield
		bf0 = append(bf0, o.ID)
	}
	g.SetZone(state.ZBattlefield, 0, bf0)
	atk := g.Obj(bf0[1])
	atk.IsAttacking, atk.Attacking = true, 1
	atk.Counters = []state.Counter{{Kind: "P1P1", N: 2}, {Kind: "Charge", N: 1}}
	blocker := g.Zone(state.ZBattlefield, 1)[0]
	atk.BlockedBy = []state.ObjID{blocker}
	g.Obj(bf0[2]).PhasedOut = true
	g.Obj(bf0[3]).Tapped = true

	var gy, ex []state.ObjID
	for i := 0; i < 2; i++ {
		o := g.AddObject(c, 1)
		o.Zone = state.ZGraveyard
		gy = append(gy, o.ID)
	}
	fd := g.AddObject(c, 1)
	fd.Zone, fd.FaceDown = state.ZExile, true
	ex = append(ex, fd.ID)
	g.SetZone(state.ZGraveyard, 1, gy)
	g.SetZone(state.ZExile, 1, ex)

	spell := g.AddObject(c, 2)
	spell.Zone, spell.Controller = state.ZStack, 2
	spell.Targets = []state.Target{{Obj: bf0[1]}, {Player: 3, IsPlayer: true}}
	ab := g.AddObject(c, 3)
	ab.Card, ab.Zone, ab.Controller, ab.Source = nil, state.ZStack, 3, bf0[1]
	ab.Ability = c.Faces[0].Abilities[0]
	ab.Targets = []state.Target{{Player: 0, IsPlayer: true}}
	g.Stack = append(g.Stack, spell.ID, ab.ID)

	g.Players[2].RestrictedMana = []state.ManaRestriction{{Color: "G", Amount: 1, Valid: "Spell.Creature"}}
	g.Initiative, g.HasInitiative = 3, true
	g.ControlledBy = map[state.PlayerID]state.PlayerID{3: 2}
	return g
}

// overBoard is a finished game: seat 2 won.
func overBoard(t *testing.T) *state.Game {
	g := fourSeatBoard(t)
	g.Over, g.Winner, g.Turn = true, 2, 9
	return g
}

func richDecision(player state.PlayerID) *decision.Decision {
	return &decision.Decision{
		Seq: 9, Player: player, Kind: decision.Kind("priority"), Prompt: "go",
		Options: []decision.Option{
			{Index: 0, Kind: "pass", Label: "Pass"},
			{Index: 1, Kind: "ability", Label: "Bear: CARDNAME fights"},
		},
		WindowReasons:   []decision.WindowReason{{}},
		PaymentFallback: &decision.PaymentFallback{Reason: "r"},
		ManaPayment:     &decision.ManaPaymentWindow{AutoFill: []state.ObjID{4, 5}},
	}
}

func viewJSON(v View) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestProjectIntoCarriesNoStaleData is the reuse contract on hand-built
// states that light up every field: one dst refilled through a cycle of
// different boards, viewers, visibilities, Chars and decisions -- a rich
// board after a bare one and back, a commander board between them, a
// finished game -- is at every step exactly the fresh projection. A field
// that kept a previous state's value (a counter map key, a blocker, a
// pointer, a decision option, a hand that is hidden now) fails here.
func TestProjectIntoCarriesNoStaleData(t *testing.T) {
	bare, rich, cmdr, over := fourSeatBoard(t), richBoard(t), commanderFixture(t), overBoard(t)
	type step struct {
		g  *state.Game
		ch Chars
		d  *decision.Decision
	}
	steps := []step{
		{rich, richChars{flatChars{rich}}, richDecision(0)},
		{bare, flatChars{bare}, nil},
		{cmdr, richChars{flatChars{cmdr}}, richDecision(1)},
		{rich, richChars{flatChars{rich}}, richDecision(2)},
		{over, flatChars{over}, richDecision(0)},
		{rich, flatChars{rich}, nil},
		{bare, richChars{flatChars{bare}}, richDecision(3)},
		{rich, nil, richDecision(0)},
		{rich, richChars{flatChars{rich}}, richDecision(0)},
	}
	viewers := []state.PlayerID{0, 1, 2, 3, NoSeat}
	viss := []Visibility{Seat, Public, Omniscient}
	var shared View
	perViewer := make([]View, len(viewers)*len(viss))
	for round := 0; round < 2; round++ {
		for si, s := range steps {
			for vi, viewer := range viewers {
				for wi, vis := range viss {
					for _, also := range [][]state.PlayerID{nil, {1}} {
						want := ProjectForControlledFor(s.g, s.ch, viewer, vis, also, s.d)
						ProjectForControlledInto(&shared, s.g, s.ch, viewer, vis, also, s.d)
						if !reflect.DeepEqual(shared, want) {
							t.Fatalf("round %d step %d viewer %d %s also %v: shared dst stale\n got: %s\nwant: %s", round, si, viewer, vis, also, viewJSON(shared), viewJSON(want))
						}
						k := vi*len(viss) + wi
						ProjectForControlledInto(&perViewer[k], s.g, s.ch, viewer, vis, also, s.d)
						if !reflect.DeepEqual(perViewer[k], want) {
							t.Fatalf("round %d step %d viewer %d %s also %v: per-viewer dst stale\n got: %s\nwant: %s", round, si, viewer, vis, also, viewJSON(perViewer[k]), viewJSON(want))
						}
					}
				}
			}
		}
	}
	// The fixtures really are rich: the dst saw every reusable field.
	v := ProjectFor(rich, richChars{flatChars{rich}}, 0, Seat, richDecision(0))
	p0, p1 := v.Players[0], v.Players[1]
	var sawCounters, sawBlocked, sawAttack, sawProduces bool
	for _, c := range p0.Battlefield {
		sawCounters = sawCounters || c.Counters != nil
		sawBlocked = sawBlocked || c.BlockedBy != nil
		sawAttack = sawAttack || c.AttackingPlayer != nil
		sawProduces = sawProduces || c.Produces != nil
	}
	if !sawCounters || !sawBlocked || !sawAttack || !sawProduces || p0.LibraryTop == nil ||
		len(v.Stack) != 2 || v.Stack[1].Card == nil || v.Stack[1].Decider == nil || v.Pending[1].Decider == nil ||
		p1.Archetype == nil || len(p1.Exile) != 1 || !p1.Exile[0].FaceDown || v.Players[2].PoolRestrictions == nil ||
		v.Decision == nil || v.Decision.ManaPayment == nil || v.Players[3].Hand != nil {
		t.Fatalf("rich fixture does not exercise every reusable field: %s", viewJSON(v))
	}
	cv := ProjectFor(cmdr, flatChars{cmdr}, 0, Seat, nil)
	if cv.Players[1].CmdDamage == nil || len(cv.Players[0].Commanders) != 1 {
		t.Fatalf("commander fixture lacks the commander fields: %s", viewJSON(cv))
	}
}

// TestProjectIntoDoesNotAliasTheEngine pins that a reused dst never shares
// storage with what it was projected from: mutating the refilled View leaves
// the game, the decision and a second projection untouched.
func TestProjectIntoDoesNotAliasTheEngine(t *testing.T) {
	g := richBoard(t)
	ch := richChars{flatChars{g}}
	d := richDecision(0)
	var dst View
	for i := 0; i < 2; i++ {
		ProjectInto(&dst, g, ch, 0, d)
	}
	for i := range dst.Players {
		for j := range dst.Players[i].Battlefield {
			c := &dst.Players[i].Battlefield[j]
			for k := range c.BlockedBy {
				c.BlockedBy[k] = 999
			}
			for k := range c.Keywords {
				c.Keywords[k] = "x"
			}
			for k := range c.Counters {
				c.Counters[k] = -1
			}
		}
	}
	dst.Decision.Options[0].Label = "mutated"
	dst.Decision.ManaPayment.AutoFill[0] = 999
	if g.Obj(g.Zone(state.ZBattlefield, 0)[1]).BlockedBy[0] == 999 || d.Options[0].Label != "Pass" || d.ManaPayment.AutoFill[0] != 4 {
		t.Fatal("a refilled View aliases the game or the decision it was projected from")
	}
	var other View
	ProjectInto(&other, g, ch, 0, d)
	if reflect.DeepEqual(other, dst) {
		t.Fatal("mutation of one View reached another")
	}
	if !reflect.DeepEqual(other, Project(g, ch, 0, d)) {
		t.Fatal("a second dst differs from the fresh projection")
	}
}

// TestProjectIntoAllocatesNothingOnceWarm is the zero-allocation pin: with a
// Chars that allocates nothing itself, refilling a warm dst with the rich
// board -- every per-card pointer, slice and map populated, a decision with
// a payment window attached -- allocates zero times, for a seat view and an
// omniscient one.
func TestProjectIntoAllocatesNothingOnceWarm(t *testing.T) {
	g := richBoard(t)
	ch := noAllocChars{flatChars{g}}
	d := richDecision(0)
	for _, vis := range []Visibility{Seat, Omniscient, Public} {
		var dst View
		ProjectForInto(&dst, g, ch, 0, vis, d)
		if n := testing.AllocsPerRun(50, func() { ProjectForInto(&dst, g, ch, 0, vis, d) }); n != 0 {
			t.Errorf("%s: warm ProjectForInto allocates %.1f times per call, want 0", vis, n)
		}
	}
}

// noAllocChars is flatChars whose answers allocate nothing (fixed slices),
// so an allocation measured through it is the projection's own.
type noAllocChars struct{ flatChars }

var fixedKeywords = []string{"Flying", "Trample"}

func (noAllocChars) Keywords(id state.ObjID) []string {
	if id%2 == 0 {
		return fixedKeywords
	}
	return nil
}
func (noAllocChars) AbilityCosts(state.PlayerID, state.ObjID) []string { return nil }

func BenchmarkProjectInto(b *testing.B) {
	g := richBoard(&testing.T{})
	ch := noAllocChars{flatChars{g}}
	d := richDecision(0)
	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = Project(g, ch, 0, d)
		}
	})
	b.Run("into", func(b *testing.B) {
		b.ReportAllocs()
		var dst View
		for b.Loop() {
			ProjectInto(&dst, g, ch, 0, d)
		}
	})
}
