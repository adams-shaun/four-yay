package rules

// The remaining Count$LastStateBattlefieldWithFallback carriers on the as-cast
// battlefield snapshot (cast_battlefield_snapshot_test.go has Steer Clear):
// Faerie Fencing, Lifestream's Blessing, Reap, Volcanic Wind, plus the copy,
// clone, lifetime and present-empty-versus-absent contracts. Real corpus
// spells with synthetic surrounding permanents; no script text is committed.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// hardCastOption is id's plain "Cast <name>" option, skipping a Foretell
// option (also Kind "cast") offered ahead of it.
func hardCastOption(t *testing.T, e *Engine, id state.ObjID) decision.Option {
	t.Helper()
	name := e.G.Obj(id).Face().Name
	for _, o := range castOptions(t, e) {
		if o.Obj == id && o.Label == "Cast "+name {
			return o
		}
	}
	t.Fatalf("no plain cast option for %q", name)
	return decision.Option{}
}

// heldCast casts id answering every ask through pick (which returns the
// choice indices for the pending decision) and stops with the spell on the
// stack and its caster holding priority.
func heldCast(t *testing.T, e *Engine, id state.ObjID, pick func(d *decision.Decision) []int) *state.Object {
	t.Helper()
	submitChoices(t, e, hardCastOption(t, e, id).Index)
	for i := 0; i < 12; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision while casting %d", id)
		}
		if o := e.G.Obj(id); o.Zone == state.ZStack && d.Kind == decision.KPriority {
			return o
		}
		submitChoices(t, e, pick(d)...)
	}
	t.Fatalf("spell %d never reached the response window", id)
	return nil
}

// diesToGraveyard moves a battlefield permanent to its graveyard by a
// logged event and asserts it left.
func diesToGraveyard(t *testing.T, e *Engine, id state.ObjID) {
	t.Helper()
	if e.G.Obj(id).Zone != state.ZBattlefield {
		t.Fatalf("precondition: %d is not on the battlefield", id)
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZBattlefield, To: state.ZGraveyard})
	if e.G.Obj(id).Zone != state.ZGraveyard {
		t.Fatalf("precondition: %d did not die", id)
	}
}

// faerieFencingFixture seats a 6/6 victim opposite and Faerie Fencing in
// hand with BB, optionally with a Faerie in play for the caster.
func faerieFencingFixture(t *testing.T, seed uint64, faerie bool) (e *Engine, cfg Config, spell, victim, fae state.ObjID, caster state.PlayerID) {
	t.Helper()
	e, cfg, spell, caster = snapshotConfig(t, seed, "Faerie Fencing", "Colossal Dreadmaw", "Faerie Miscreant")
	victim = toBattlefield(t, e, 1-caster, "Colossal Dreadmaw")
	if faerie {
		fae = toBattlefield(t, e, caster, "Faerie Miscreant")
	}
	addMana(t, e, caster, "BB")
	return
}

// TestCastBattlefieldSnapshotFaerieFencingFaerieLeaves: the Faerie dies in
// response, yet the additional -3/-3 stays (a Faerie was controlled as it was
// cast).
func TestCastBattlefieldSnapshotFaerieFencingFaerieLeaves(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, fae, _ := faerieFencingFixture(t, 221, true)
	if e.Power(victim) != 6 {
		t.Fatalf("precondition: victim power %d, want 6", e.Power(victim))
	}
	so := holdCast(t, e, spell, victim, 1)
	if so.CastBattlefield == nil || so.X != 1 {
		t.Fatalf("precondition: as-cast battlefield %v, X %d", so.CastBattlefield, so.X)
	}
	diesToGraveyard(t, e, fae)
	passUntilStackEmpty(t, e, 20)
	if p, tt := e.Power(victim), e.Toughness(victim); p != 2 || tt != 2 {
		t.Fatalf("victim is %d/%d, want 2/2 (-1/-1 and the Faerie's -3/-3)", p, tt)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotFaerieFencingFaerieArrivesAfterCast: a Faerie
// that arrives in response was not controlled as the spell was cast.
func TestCastBattlefieldSnapshotFaerieFencingFaerieArrivesAfterCast(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, _, caster := faerieFencingFixture(t, 222, false)
	so := holdCast(t, e, spell, victim, 1)
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Faerie Fencing carries no as-cast battlefield")
	}
	fae := toBattlefield(t, e, caster, "Faerie Miscreant")
	if fae == 0 {
		t.Fatal("precondition: no Faerie arrived")
	}
	passUntilStackEmpty(t, e, 20)
	if p, tt := e.Power(victim), e.Toughness(victim); p != 5 || tt != 5 {
		t.Fatalf("victim is %d/%d, want 5/5 (only the -1/-1: the Faerie arrived after the cast)", p, tt)
	}
	replayCheck(t, e, cfg)
}

// lifestreamHandDelta casts Lifestream's Blessing with the caster controlling
// the named creatures, runs mutate while it waits on the stack, and returns
// how many cards the caster drew.
func lifestreamHandDelta(t *testing.T, seed uint64, atCast []string, mutate func(e *Engine, caster state.PlayerID, ids []state.ObjID)) (int, *Engine, Config) {
	t.Helper()
	e, cfg, spell, caster := snapshotConfig(t, seed, "Lifestream's Blessing", "Grizzly Bears", "Colossal Dreadmaw")
	var ids []state.ObjID
	for _, name := range atCast {
		ids = append(ids, toBattlefield(t, e, caster, name))
	}
	addMana(t, e, caster, "GGGGGG")
	so := heldCast(t, e, spell, func(d *decision.Decision) []int {
		t.Fatalf("unexpected ask while casting: %+v", d)
		return nil
	})
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Lifestream's Blessing carries no as-cast battlefield")
	}
	handBefore := append([]state.ObjID(nil), e.G.Zone(state.ZHand, caster)...)
	mutate(e, caster, ids)
	passUntilStackEmpty(t, e, 20)
	// Cards the mutation moved OUT of the hand (a creature put onto the
	// battlefield from it) are not draws: count them back in.
	hand := e.G.Zone(state.ZHand, caster)
	left := 0
	for _, id := range handBefore {
		if e.G.Obj(id).Zone != state.ZHand {
			left++
		}
	}
	return len(hand) - len(handBefore) + left, e, cfg
}

// TestCastBattlefieldSnapshotLifestreamBiggerCreatureLeaves: the 6-power
// creature dies in response; X is still the greatest power as the spell was
// cast.
func TestCastBattlefieldSnapshotLifestreamBiggerCreatureLeaves(t *testing.T) {
	t.Parallel()
	drew, e, cfg := lifestreamHandDelta(t, 231, []string{"Colossal Dreadmaw"}, func(e *Engine, _ state.PlayerID, ids []state.ObjID) {
		diesToGraveyard(t, e, ids[0])
	})
	if drew != 6 {
		t.Fatalf("drew %d, want 6 (the Dreadmaw's power as the spell was cast)", drew)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotLifestreamPowerChangesAfterCast: a creature's
// power changes (+1/+1 counters) and a bigger one arrives after the cast; X
// is the as-cast greatest power, which only the frozen layer-derived power
// answers.
func TestCastBattlefieldSnapshotLifestreamPowerChangesAfterCast(t *testing.T) {
	t.Parallel()
	drew, e, cfg := lifestreamHandDelta(t, 232, []string{"Grizzly Bears"}, func(e *Engine, caster state.PlayerID, ids []state.ObjID) {
		e.emit(events.Event{Kind: events.CounterChange, Obj: ids[0], Counter: "P1P1", Amount: 3})
		if e.Power(ids[0]) != 5 {
			t.Fatalf("precondition: Bears power %d after counters, want 5", e.Power(ids[0]))
		}
		toBattlefield(t, e, caster, "Colossal Dreadmaw")
	})
	if drew != 2 {
		t.Fatalf("drew %d, want 2 (the Bears' power as the spell was cast)", drew)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotReapBindsTargetedPlayer: Reap counts the BLACK
// permanents of the player it TARGETED, as cast. The cast-time target limit
// is that count, and the frozen read keeps it after the black permanent dies
// and after another one arrives.
func TestCastBattlefieldSnapshotReapBindsTargetedPlayer(t *testing.T) {
	t.Parallel()
	e, cfg, spell, caster := snapshotConfig(t, 241, "Reap", "Walking Corpse", "Grizzly Bears")
	oppCorpse := toBattlefield(t, e, 1-caster, "Walking Corpse") // black, the one that counts
	toBattlefield(t, e, 1-caster, "Grizzly Bears")               // green: must not count
	toBattlefield(t, e, caster, "Walking Corpse")                // black but the caster's own
	for i := 0; i < 2; i++ {
		lib := e.G.Zone(state.ZLibrary, caster)
		e.emit(events.Event{Kind: events.MoveZone, Obj: lib[len(lib)-1], From: state.ZLibrary, To: state.ZGraveyard})
	}
	if len(e.G.Zone(state.ZGraveyard, caster)) < 2 {
		t.Fatal("precondition: the caster's graveyard needs two cards to pick from")
	}
	addMana(t, e, caster, "GG")
	so := heldCast(t, e, spell, func(d *decision.Decision) []int {
		if d.Kind != decision.KTarget {
			t.Fatalf("unexpected ask while casting: %+v", d)
		}
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == 1-caster {
				return []int{o.Index}
			}
		}
		t.Fatalf("the opponent was not offered: %+v", d.Options)
		return nil
	})
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Reap carries no as-cast battlefield")
	}
	diesToGraveyard(t, e, oppCorpse)
	ctx := effects.NewCtx(spell, caster, effects.CtxInit{Targets: so.Targets, SVars: so.Face().SVars})
	n, ok := effects.EvalCountOK(e, &ctx, so.Face().SVars["NrBlackAtCasting"])
	if !ok || n != 1 {
		t.Fatalf("NrBlackAtCasting after the black permanent died = %d (ok %v), want the frozen 1", n, ok)
	}
	// Reap asks for its graveyard targets as it resolves, sized by that head:
	// the limit is the as-cast 1 although the opponent now controls no black
	// permanent.
	graveLimit := -1
	for i := 0; i < 12 && graveLimit < 0 && e.G.Obj(spell).Zone == state.ZStack; i++ {
		d := e.Pending()
		if d.Kind != decision.KPriority {
			if !strings.Contains(d.Prompt, "target card in your graveyard") {
				t.Fatalf("unexpected ask at resolution: %+v", d)
			}
			graveLimit = d.Max
			break
		}
		submitChoices(t, e, passOption(t, d).Index)
	}
	if graveLimit != 1 {
		t.Fatalf("graveyard target limit %d at resolution, want the frozen 1", graveLimit)
	}
	_ = cfg
}

// TestCastBattlefieldSnapshotVolcanicWindLimitAndDamage: the divided-damage
// carrier sizes its cast-time target limit and its X from the as-cast
// battlefield: three creatures at the cast, one dies in response, and the
// spell still deals 3.
func TestCastBattlefieldSnapshotVolcanicWindLimitAndDamage(t *testing.T) {
	t.Parallel()
	e, cfg, spell, caster := snapshotConfig(t, 251, "Volcanic Wind", "Walking Corpse", "Colossal Dreadmaw")
	own := toBattlefield(t, e, caster, "Walking Corpse")
	big := toBattlefield(t, e, 1-caster, "Colossal Dreadmaw")
	toBattlefield(t, e, 1-caster, "Walking Corpse")
	addMana(t, e, caster, "RRRRRR")
	var limit int
	so := heldCast(t, e, spell, func(d *decision.Decision) []int {
		if d.Kind != decision.KTarget {
			t.Fatalf("unexpected ask while casting: %+v", d)
		}
		limit = d.Max
		for _, o := range d.Options {
			if o.Obj == big {
				return []int{o.Index}
			}
		}
		t.Fatalf("Dreadmaw not offered: %+v", d.Options)
		return nil
	})
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Volcanic Wind carries no as-cast battlefield")
	}
	if limit != 3 {
		t.Fatalf("target limit %d, want 3 (three creatures as it was cast)", limit)
	}
	diesToGraveyard(t, e, own)
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(big).Damage; got != 3 {
		t.Fatalf("Volcanic Wind dealt %d, want 3 (X was 3 as it was cast)", got)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotCopyKeepsOriginalCast: a copy of the spell
// answers "as you cast this spell" with the ORIGINAL cast's battlefield, even
// after a response changed the board.
func TestCastBattlefieldSnapshotCopyKeepsOriginalCast(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, mount, caster := steerClearFixture(t, 261, true)
	so := holdCast(t, e, spell, victim, 0)
	e.emit(events.Event{Kind: events.StackCopy, Obj: spell, Player: caster})
	if len(e.G.Stack) != 2 {
		t.Fatalf("precondition: stack %v, want the spell and its copy", e.G.Stack)
	}
	cp := e.G.Obj(e.G.Stack[1])
	if !cp.IsCopy || cp.CastBattlefield == nil || cp.CastBattlefield != so.CastBattlefield {
		t.Fatalf("copy carries %v, want the original's as-cast battlefield %v", cp.CastBattlefield, so.CastBattlefield)
	}
	diesToGraveyard(t, e, mount)
	passUntilStackEmpty(t, e, 20)
	// 4 from the copy + 4 from the original (a 6-toughness victim): had either
	// read the empty resolution-time battlefield it would deal 2 and survive 4.
	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Fatalf("victim is in %v, want the graveyard (4 + 4 damage)", z)
	}
	replayCheck(t, e, cfg)
}

// TestCastBattlefieldSnapshotCloneKeepsFrozenFacts: an engine clone taken in
// the response window carries the snapshot and resolves from it, and
// resolving the clone leaves the original's snapshot untouched.
func TestCastBattlefieldSnapshotCloneKeepsFrozenFacts(t *testing.T) {
	t.Parallel()
	e, _, spell, victim, mount, _ := steerClearFixture(t, 271, true)
	so := holdCast(t, e, spell, victim, 0)
	snap := so.CastBattlefield
	if snap == nil {
		t.Fatal("precondition: Steer Clear carries no as-cast battlefield")
	}
	c := e.Clone()
	co := c.G.Obj(spell)
	if co.Zone != state.ZStack || co.CastBattlefield == nil {
		t.Fatalf("clone's spell: zone %v, snapshot %v", co.Zone, co.CastBattlefield)
	}
	diesToGraveyard(t, c, mount)
	passUntilStackEmpty(t, c, 20)
	if got := c.G.Obj(victim).Damage; got != 4 {
		t.Fatalf("clone resolved Steer Clear for %d, want 4", got)
	}
	if e.G.Obj(spell).CastBattlefield != snap || e.G.Obj(spell).Zone != state.ZStack {
		t.Fatal("resolving the clone disturbed the original's snapshot")
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(victim).Damage; got != 4 {
		t.Fatalf("original resolved Steer Clear for %d, want 4", got)
	}
}

// TestCastBattlefieldSnapshotLifetime: the snapshot lives exactly as long as
// the cast. It is cleared when the spell leaves the stack (resolution or
// counter), so a recast of the same ObjID freezes the NEW battlefield.
func TestCastBattlefieldSnapshotLifetime(t *testing.T) {
	t.Parallel()
	e, cfg, spell, victim, _, caster := steerClearFixture(t, 281, false)
	so := holdCast(t, e, spell, victim, 0)
	if so.CastBattlefield == nil {
		t.Fatal("precondition: Steer Clear carries no as-cast battlefield")
	}
	passUntilStackEmpty(t, e, 20)
	if got := e.G.Obj(victim).Damage; got != 2 {
		t.Fatalf("first cast dealt %d, want 2 (no Mount as it was cast)", got)
	}
	if e.G.Obj(spell).Zone == state.ZStack || e.G.Obj(spell).CastBattlefield != nil {
		t.Fatalf("resolved spell kept its snapshot (zone %v)", e.G.Obj(spell).Zone)
	}
	// Recast the same object with a Mount now in play; a countered spell drops
	// its snapshot the moment it leaves the stack.
	toBattlefield(t, e, caster, "Venomsac Lagac")
	recast := func() *state.Object {
		e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: e.G.Obj(spell).Zone, To: state.ZHand})
		addMana(t, e, caster, "W")
		e.pending = nil
		e.priorityRound()
		return holdCast(t, e, spell, victim, 0)
	}
	so = recast()
	if so.CastBattlefield == nil {
		t.Fatal("precondition: the second cast carries no as-cast battlefield")
	}
	e.emit(events.Event{Kind: events.MoveZone, Obj: spell, From: state.ZStack, To: state.ZGraveyard})
	if e.G.Obj(spell).CastBattlefield != nil {
		t.Fatal("a countered spell kept its as-cast battlefield")
	}
	// The third cast freezes the NEW battlefield, not a stale one.
	recast()
	passUntilStackEmpty(t, e, 20)
	if z := e.G.Obj(victim).Zone; z != state.ZGraveyard {
		t.Fatalf("after the recast the victim is in %v, want the graveyard (2 + 4 damage: a Mount was controlled as it was recast)", z)
	}
	_ = cfg
}

// TestCastBattlefieldSnapshotPresentEmptyVersusAbsent: a spell on the stack
// with NO snapshot reads the live battlefield (the fallback), but one frozen
// with an EMPTY battlefield is an authoritative zero even when a matching
// permanent is on the battlefield now.
func TestCastBattlefieldSnapshotPresentEmptyVersusAbsent(t *testing.T) {
	t.Parallel()
	e, _, spell, caster := snapshotConfig(t, 291, "Steer Clear", "Venomsac Lagac")
	toBattlefield(t, e, caster, "Venomsac Lagac")
	// On the stack by a bare PutOnStack: no cast flow, so no freeze.
	e.emit(events.Event{Kind: events.PutOnStack, Obj: spell, Player: caster, From: state.ZHand, To: state.ZStack})
	so := e.G.Obj(spell)
	if so.Zone != state.ZStack || so.CastBattlefield != nil {
		t.Fatalf("precondition: zone %v snapshot %v, want an unfrozen spell on the stack", so.Zone, so.CastBattlefield)
	}
	body := so.Face().SVars["Y"]
	ctx := effects.NewCtx(spell, caster, effects.CtxInit{SVars: so.Face().SVars})
	if n, ok := effects.EvalCountOK(e, &ctx, body); !ok || n != 1 {
		t.Fatalf("absent snapshot: Y = %d (ok %v), want the live 1", n, ok)
	}
	// An event with no IDs freezes an empty battlefield.
	e.emit(events.Event{Kind: events.CastBattlefield, Obj: spell, Player: caster})
	if so = e.G.Obj(spell); so.CastBattlefield == nil {
		t.Fatal("the empty CastBattlefield event left no snapshot")
	}
	if n, ok := effects.EvalCountOK(e, &ctx, body); !ok || n != 0 {
		t.Fatalf("present-empty snapshot: Y = %d (ok %v), want the authoritative 0", n, ok)
	}
}

// passOption is the pending priority decision's "pass" option.
func passOption(t *testing.T, d *decision.Decision) decision.Option {
	t.Helper()
	for _, o := range d.Options {
		if o.Kind == "pass" {
			return o
		}
	}
	t.Fatalf("no pass option in %+v", d)
	return decision.Option{}
}
