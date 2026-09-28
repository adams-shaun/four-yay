package rules

// Multikicker (CR 702.43, task multikicker1) pins: the "multikicked" cast
// option, the count ask (multikickAsk, the replicateAsk shape), the trailing
// pay-time FlagMultikicked CastInfo carrying the count into
// state.Object.TimesKicked, the Count$TimesKicked head, and the legacy
// plain-Kicker carriers' SVar-gated emission (Into the Roil's stream stays
// byte-identical; Stronghold Arena's Count$TimesKicked/Thrice reads a real
// count).

import (
	"strconv"

	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// submitPass submits the pending priority decision's pass option.
func passHere(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("not at priority: %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d)
}

// castOptionFor returns the cast option for obj with the given mode ("" =
// the plain cast), failing when absent.
func castOptMode(t *testing.T, opts []decision.Option, id state.ObjID, mode string) decision.Option {
	t.Helper()
	for _, o := range opts {
		if o.Kind == "cast" && o.Obj == id && o.Mode == mode {
			return o
		}
	}
	t.Fatalf("no cast option (obj %d mode %q) in %+v", id, mode, opts)
	return decision.Option{}
}

// castInfosFor returns every CastInfo event on obj as (flags, amount).
func castInfosFor(e *Engine, obj state.ObjID) [][2]string {
	var out [][2]string
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == obj {
			out = append(out, [2]string{ev.Counter, strconv.Itoa(int(ev.Amount))})
		}
	}
	return out
}

// awaitETBTargetAsk submits pass until the ETB trigger's placement target ask
// (KTarget) is pending, failing on anything else.
func awaitETBTargetAsk(t *testing.T, e *Engine) *decision.Decision {
	t.Helper()
	for i := 0; i < 10; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision pending")
		}
		if d.Kind == decision.KTarget {
			return d
		}
		if d.Kind != decision.KPriority {
			t.Fatalf("unexpected decision waiting for the choice ask: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "pass" {
				submitChoices(t, e, o.Index)
				break
			}
		}
	}
	t.Fatal("the choice ask never arrived")
	return nil
}

// awaitAnthemETBStack crosses the spell-resolution boundary explicitly. A
// stack-empty drain can stop when the spell leaves the stack, before its ETB
// trigger is placed; only drain the trigger after observing its stack object.
func awaitAnthemETBStack(t *testing.T, e *Engine, anthem state.ObjID) {
	t.Helper()
	if o := e.G.Obj(anthem); o == nil || o.Zone != state.ZStack {
		t.Fatalf("precondition: Anthem %d must be on the stack before resolution: %+v", anthem, o)
	}
	for i := 0; i < 12; i++ {
		if n := len(e.G.Stack); n > 0 {
			if top := e.G.Obj(e.G.Stack[n-1]); top != nil && top.Ability != nil && top.Source == anthem {
				if !hasEvent(e, events.TriggerPush, anthem) {
					t.Fatal("precondition: Anthem ETB stack object has no TriggerPush event")
				}
				return
			}
		}
		passHere(t, e) // both players pass; the next boundary places the ETB
	}
	t.Fatalf("Anthem ETB trigger never reached the stack (depth %d, pending %+v)", len(e.G.Stack), e.Pending())
}

func hasMultikickedCastInfo(e *Engine, obj state.ObjID) bool {
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == obj &&
			events.FlagsFrom(ev.Counter)&state.FlagMultikicked != 0 {
			return true
		}
	}
	return false
}

// TestMarshalsAnthemMultikickedETBReturnsKickedCount drives the real corpus
// card end to end: two kicks cost {2}{W}{W} + 2x{1}{W} = 8 mana exactly, the
// ETB trigger's TargetMax$ X (SVar:X:Count$TimesKicked) bounds the graveyard
// ask at 2, and the answered ask returns exactly those creatures.
func TestMarshalsAnthemMultikickedETBReturnsKickedCount(t *testing.T) {
	t.Parallel()
	e, _, anthem := gateFixture(t, 911, "Marshal's Anthem", gateRaiderSrc, gateRaiderSrc)
	g1 := gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	g2 := gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	addMana(t, e, 0, "WWWWWWWW") // 4 generic + 4 W, exactly

	opts := castOptions(t, e)
	mk := castOptMode(t, opts, anthem, "multikicked")
	// The plain cast is offered beside it.
	castOptMode(t, opts, anthem, "")
	submitChoices(t, e, mk.Index)

	// The count ask: pool 8 bounds max at 2 (base+3 kicks = 10 is not
	// payable); options run 0..2.
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "multikick" {
		t.Fatalf("multikick ask missing: %+v", d)
	}
	if len(d.Options) != 3 || d.Options[0].Amount != 0 || d.Options[2].Amount != 2 {
		t.Fatalf("multikick options: %+v", d.Options)
	}
	submitChoices(t, e, d.Options[2].Index)

	if o := e.G.Obj(anthem); o.Zone != state.ZStack || o.TimesKicked != 2 ||
		o.CastFlags&(state.FlagKicked|state.FlagMultikicked) != state.FlagKicked|state.FlagMultikicked {
		t.Fatalf("on the stack: zone %s kicked %d flags %#x", o.Zone, o.TimesKicked, o.CastFlags)
	}
	if e.G.Players[0].Pool.Total() != 0 {
		t.Fatalf("pool after payment %d, want 0 (8 mana exactly)", e.G.Players[0].Pool.Total())
	}
	// The trailing CastInfo is the count's only carrier: exactly one
	// multikicked event, Amount 2.
	n := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.CastInfo && ev.Obj == anthem &&
			events.FlagsFrom(ev.Counter)&state.FlagMultikicked != 0 {
			if ev.Amount != 2 {
				t.Fatalf("multikicked CastInfo Amount %d, want 2", ev.Amount)
			}
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d multikicked CastInfo events, want 1", n)
	}

	passHere(t, e)
	// The anthem resolves; the ETB trigger poses the graveyard ask bounded
	// Max 2 over the two raiders.
	d = awaitETBTargetAsk(t, e)
	if d.Max != 2 || len(d.Options) != 2 {
		t.Fatalf("ETB ask Min %d Max %d options %+v", d.Min, d.Max, d.Options)
	}
	submitChoices(t, e, d.Options[0].Index, d.Options[1].Index)
	passUntilStackEmpty(t, e, 20)
	for _, id := range []state.ObjID{g1, g2} {
		if z := e.G.Obj(id).Zone; z != state.ZBattlefield {
			t.Fatalf("raider %d zone %s, want returned to the battlefield", id, z)
		}
	}
}

// TestMarshalsAnthemPlainCastETBAsksForNothing pins the count-0 shape: a
// plain cast leaves X at 0, so the ETB trigger fires but its resolved
// TargetMin$ 0 / TargetMax$ X pair takes no targets and poses no ask. The
// graveyard stays untouched. A multikicked cast answered "No multikick"
// has the same result -- no FlagKicked or multikicked CastInfo.
func TestMarshalsAnthemPlainCastETBAsksForNothing(t *testing.T) {
	t.Parallel()
	e, _, anthem := gateFixture(t, 912, "Marshal's Anthem", gateRaiderSrc, gateRaiderSrc)
	g1 := gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	g2 := gateMoveFromLibrary(t, e, "Raider", state.ZGraveyard)
	addMana(t, e, 0, "WWWW") // {2}{W}{W} exactly

	submitChoices(t, e, castOptMode(t, castOptions(t, e), anthem, "").Index)
	if d := e.Pending(); d != nil && d.Kind == decision.KChoose {
		t.Fatalf("plain cast posed an ask: %+v", d)
	}
	awaitAnthemETBStack(t, e, anthem)
	passUntilStackEmpty(t, e, 20) // an unexpected target ask fails the drain
	if o := e.G.Obj(anthem); o.CastFlags&state.FlagKicked != 0 || o.TimesKicked != 0 {
		t.Fatalf("plain cast flags %#x kicked %d", o.CastFlags, o.TimesKicked)
	}
	for _, id := range []state.ObjID{g1, g2} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("raider %d zone %s, want untouched in the graveyard", id, z)
		}
	}

	// Decline through the ask: same plain cast, same silence.
	e2, _, anthem2 := gateFixture(t, 913, "Marshal's Anthem", gateRaiderSrc, gateRaiderSrc)
	g3 := gateMoveFromLibrary(t, e2, "Raider", state.ZGraveyard)
	addMana(t, e2, 0, "WWWWWWWW")
	submitChoices(t, e2, castOptMode(t, castOptions(t, e2), anthem2, "multikicked").Index)
	d := e2.Pending()
	if d == nil || d.Kind != decision.KChoose {
		t.Fatalf("multikick ask missing: %+v", d)
	}
	submitChoices(t, e2, d.Options[0].Index) // "No multikick"
	if o := e2.G.Obj(anthem2); o.CastFlags != 0 || hasMultikickedCastInfo(e2, anthem2) {
		t.Fatalf("declined kick: flags %#x, multikicked CastInfo present", o.CastFlags)
	}
	awaitAnthemETBStack(t, e2, anthem2)
	passUntilStackEmpty(t, e2, 20)
	if z := e2.G.Obj(g3).Zone; z != state.ZGraveyard {
		t.Fatalf("raider %d zone %s, want untouched", g3, z)
	}
}

// TestEverflowingChaliceKickedEntersWithChargeCounters drives the real corpus
// card: two kicks enter it with 2 CHARGE counters (the K:etbCounter
// CounterNum$ XKicked chain resolving through SVar XKicked:Count$TimesKicked),
// and the {T} ability adds that many {C}.
func TestEverflowingChaliceKickedEntersWithChargeCounters(t *testing.T) {
	t.Parallel()
	e, _, chalice := gateFixture(t, 914, "Everflowing Chalice")
	addMana(t, e, 0, "GGGG") // two {2} kicks

	submitChoices(t, e, castOptMode(t, castOptions(t, e), chalice, "multikicked").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "multikick" {
		t.Fatalf("multikick ask missing: %+v", d)
	}
	if len(d.Options) != 3 {
		t.Fatalf("pool 4 bounds max at 2: %+v", d.Options)
	}
	submitChoices(t, e, d.Options[2].Index)
	passUntilStackEmpty(t, e, 20)

	o := e.G.Obj(chalice)
	if o.Zone != state.ZBattlefield || o.Counter("CHARGE") != 2 || o.TimesKicked != 2 {
		t.Fatalf("chalice: zone %s CHARGE %d kicked %d", o.Zone, o.Counter("CHARGE"), o.TimesKicked)
	}
	// {T}: Add {C} for each charge counter.
	d = e.Pending()
	idx := -1
	for _, opt := range d.Options {
		if opt.Kind == "activate" && opt.Obj == chalice {
			idx = opt.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no ability option for the chalice: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Players[0].Pool.Total(); got != 2 {
		t.Fatalf("tapped chalice added %d mana, want 2", got)
	}
}

// TestCometStormXAndMultikickRideSeparateEvents drives the {X} competition:
// Comet Storm pairs {X} with Multikicker, so the X value and the kick count
// ride separate pay-time CastInfo events and both fields read correctly. The
// target ask's TargetMin/Max$ TargetsNum (Count$TimesKicked/Plus.1) reads the
// pending count seeded by targetBoundCtx, so one kick demands 2 targets and
// each takes X damage.
func TestCometStormXAndMultikickRideSeparateEvents(t *testing.T) {
	t.Parallel()
	e, _, storm := gateFixture(t, 915, "Comet Storm", gateRaiderSrc, gateRaiderSrc)
	g1 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	g2 := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "RRRRR") // X<=2 given R R + one {1} kick

	submitChoices(t, e, castOptMode(t, castOptions(t, e), storm, "multikicked").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "multikick" {
		t.Fatalf("multikick ask missing: %+v", d)
	}
	submitChoices(t, e, d.Options[1].Index) // one kick
	d = e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
		t.Fatalf("X ask missing: %+v", d)
	}
	if got := d.Options[len(d.Options)-1].Amount; got != 2 {
		t.Fatalf("max X %d, want 2", got)
	}
	submitChoices(t, e, d.Options[len(d.Options)-1].Index)
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget || d.Min != 2 || d.Max != 2 {
		t.Fatalf("target ask Min %d Max %d (want TargetsNum = kick+1 = 2): %+v",
			dMin(d), dMax(d), d)
	}
	idx := -1
	var t1, t2 int
	for _, o := range d.Options {
		if o.Obj == g1 && t1 == 0 {
			t1, idx = o.Index, o.Index
		} else if o.Obj == g2 {
			t2 = o.Index
		}
	}
	if idx < 0 || t2 == 0 {
		t.Fatalf("raiders not offered: %+v", d.Options)
	}
	submitChoices(t, e, t1, t2)
	o := e.G.Obj(storm)
	if o.Zone != state.ZStack || o.X != 2 || o.TimesKicked != 1 {
		t.Fatalf("on the stack: X %d kicked %d", o.X, o.TimesKicked)
	}
	if n := len(castInfosFor(e, storm)); n != 2 {
		t.Fatalf("%d CastInfo events on the storm, want the two-event split", n)
	}
	passHere(t, e)
	passUntilStackEmpty(t, e, 20)
	for _, id := range []state.ObjID{g1, g2} {
		if z := e.G.Obj(id).Zone; z != state.ZGraveyard {
			t.Fatalf("raider %d zone %s, want dead to X=2 damage", id, z)
		}
	}
}

func dMin(d *decision.Decision) int {
	if d == nil {
		return -1
	}
	return d.Min
}

func dMax(d *decision.Decision) int {
	if d == nil {
		return -1
	}
	return d.Max
}

// TestMultikickAskBoundedByPool pins the fixture-level ask shape: options run
// 0..max with the max the pool bounds (the multikickerCost payment is
// composed per answer), and a pool that cannot pay base+one payment offers no
// multikicked option at all.
func TestMultikickAskBoundedByPool(t *testing.T) {
	t.Parallel()
	src := "Name:Kickerling\nManaCost:2 G\nTypes:Creature\nPT:2/2\nK:Multikicker:3\nOracle:x\n"
	e, _, k := newFixtureDeck(t, 916, src)
	addMana(t, e, 0, "GGGGGGGGG") // base 3 + two 3-generic payments = 9; a third is 12

	mk := castOptMode(t, castOptions(t, e), k, "multikicked")
	submitChoices(t, e, mk.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 3 {
		t.Fatalf("multikick ask: %+v", d)
	}
	for i, want := range []string{"No multikick", "Pay multikicker once", "Pay multikicker 2 times"} {
		if d.Options[i].Label != want || d.Options[i].Amount != i {
			t.Fatalf("option %d: %+v, want label %q amount %d", i, d.Options[i], want, i)
		}
	}
	submitChoices(t, e, d.Options[1].Index)
	if o := e.G.Obj(k); o.Zone != state.ZStack || o.TimesKicked != 1 {
		t.Fatalf("one kick: zone %s kicked %d", o.Zone, o.TimesKicked)
	}
	passUntilStackEmpty(t, e, 20)

	// A pool that cannot pay base + one payment offers no multikicked option.
	e2, _, k2 := newFixtureDeck(t, 917, src)
	addMana(t, e2, 0, "GGG") // base exactly
	for _, o := range castOptions(t, e2) {
		if o.Obj == k2 && o.Mode == "multikicked" {
			t.Fatal("multikicked offered with no payable payment")
		}
	}
	castOptMode(t, castOptions(t, e2), k2, "")
}

// TestStrongholdArenaTimesKickedThrice drives the legacy plain-Kicker carrier
// with the /Thrice op: a kicked1 cast counts 1 kick (3 life), a kickedboth
// cast counts 2 (6 life) -- the count rides the SVar-gated trailing CastInfo
// because the face carries Count$TimesKicked.
func TestStrongholdArenaTimesKickedThrice(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mode string
		pool       string
		wantLife   int32
		wantKicks  int32
	}{
		{"kicked1", "kicked1", "BBGGWW", 23, 1},
		{"kickedboth", "kickedboth", "BBGGWW", 26, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, _, arena := gateFixture(t, 918, "Stronghold Arena")
			addMana(t, e, 0, tc.pool)
			submitChoices(t, e, castOptMode(t, castOptions(t, e), arena, tc.mode).Index)
			passUntilStackEmpty(t, e, 20)
			o := e.G.Obj(arena)
			if o.Zone != state.ZBattlefield || o.TimesKicked != tc.wantKicks {
				t.Fatalf("arena: zone %s kicked %d, want %d", o.Zone, o.TimesKicked, tc.wantKicks)
			}
			if got := e.G.Players[0].Life; got != tc.wantLife {
				t.Fatalf("life %d, want %d (3 x %d kicks)", got, tc.wantLife, tc.wantKicks)
			}
			if !hasMultikickedCastInfo(e, arena) {
				t.Fatal("no multikicked CastInfo on the SVar-gated kicked cast")
			}
		})
	}
}

// TestIntoTheRoilKickedEmitsNoMultikickEvent is the byte-stream guard: a
// kicked cast of a plain-Kicker face WITHOUT a Count$TimesKicked SVar gains no
// event -- the gate (b) emission is the design, so unrelated kicked casts stay
// byte-identical.
func TestIntoTheRoilKickedEmitsNoMultikickEvent(t *testing.T) {
	t.Parallel()
	e, _, roil := gateFixture(t, 919, "Into the Roil", gateRaiderSrc)
	raider := gateMoveFromLibrary(t, e, "Raider", state.ZBattlefield)
	addMana(t, e, 0, "UUUU")
	submitChoices(t, e, castOptMode(t, castOptions(t, e), roil, "kicked").Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("target ask missing: %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Obj == raider {
			tIdx = o.Index
		}
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(raider).Zone; z != state.ZHand {
		t.Fatalf("raider zone %s, want returned to hand (the kicked cast still works)", z)
	}
	if hasMultikickedCastInfo(e, roil) {
		t.Fatal("an SVar-less kicked cast emitted a multikicked CastInfo")
	}
	if o := e.G.Obj(roil); o.TimesKicked != 0 {
		t.Fatalf("roil TimesKicked %d in the graveyard, want 0", o.TimesKicked)
	}
}
