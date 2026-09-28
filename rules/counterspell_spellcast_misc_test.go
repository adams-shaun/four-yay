package rules

// One end-to-end carrier per distinct shape the param census closed in this
// wave (rules/paramcensus_test.go knownUnsupportedParams): Counter's
// Destination$ (Remand, Force of Will), CopySpellAbility's Controller$
// (Chain Lightning), Vote's card ballot (VoteCard$/VoteSubAbility$, Council's
// Judgment), the SpellCast trigger's ValidSA$ mana comparison (Roiling
// Vortex) and its ActivatorThisTurnCast$ cast count (The Lord of Pain).
// Every card is the REAL compiled corpus card loaded by name from the
// gitignored registry -- no Forge script text is copied into this file.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// miscHandsEngine builds a two-seat engine from corpus decks whose named
// fixtures ride on top, then moves every named card to its seat's hand (or
// battlefield) with a LOGGED MoveZone -- the searchEngine fixture's shape,
// generalised to both seats -- so the whole setup is replayable and
// replayCheck is meaningful. Direct zone seeding is not replayable (the
// genesis replay rebuilds hands from the deck, not from a hand patch).
func miscHandsEngine(t *testing.T, reg *cards.Registry, hand0, hand1, board0, board1 []string) (*Engine, Config) {
	t.Helper()
	mountain, ok := reg.Lookup("Mountain")
	if !ok {
		t.Fatal("missing corpus card \"Mountain\"")
	}
	deck := func(names []string) []*cards.Card {
		out := make([]*cards.Card, 0, 40)
		for _, name := range names {
			c, ok := reg.Lookup(name)
			if !ok {
				t.Fatalf("missing corpus card %q", name)
			}
			out = append(out, c)
		}
		for len(out) < 40 {
			out = append(out, mountain)
		}
		return out
	}
	cfg := Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{
			deck(slices.Concat(hand0, board0)),
			deck(slices.Concat(hand1, board1)),
		},
		Tokens: reg.Tokens}
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	seat := func(p state.PlayerID, hands, boards []string) {
		for _, name := range boards {
			miscMoveByName(t, e, p, name, state.ZBattlefield)
		}
		for _, name := range hands {
			miscMoveByName(t, e, p, name, state.ZHand)
		}
	}
	seat(0, hand0, board0)
	seat(1, hand1, board1)
	return e, cfg
}

// miscMoveByName is searchMoveByName generalised to both seats: the named
// card moves from its hand/library slot to `to` via a logged MoveZone. The
// LIBRARY is scanned before the hand so a duplicate name moves two distinct
// instances (a hand-first scan would find the instance a previous call just
// moved and silently move nothing).
func miscMoveByName(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZLibrary, state.ZHand} {
		for _, id := range e.G.Zone(z, p) {
			o := e.G.Obj(id)
			if o != nil && o.Face() != nil && o.Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				e.pending = nil
				e.priorityRound()
				return id
			}
		}
	}
	t.Fatalf("corpus fixture %q absent from seat %d's hand/library", name, p)
	return 0
}

// countNamed counts the cards named name in seat p's zone.
func countNamed(t *testing.T, e *Engine, zone state.Zone, p state.PlayerID, name string) int {
	t.Helper()
	n := 0
	for _, id := range e.G.Zone(zone, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			n++
		}
	}
	return n
}

// miscHandObj finds the id of the hand card named name on seat p.
func miscHandObj(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZHand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("seat %d's hand has no %q", p, name)
	return 0
}

// miscBoardObj finds the id of the battlefield permanent named name
// controlled by seat p.
func miscBoardObj(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("seat %d's battlefield has no %q", p, name)
	return 0
}

// miscCastOption returns the pending priority decision's cast option for
// obj, or fails.
func miscCastOption(t *testing.T, e *Engine, obj state.ObjID) int {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no decision pending while seeking a cast option")
	}
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == obj {
			return o.Index
		}
	}
	t.Fatalf("no cast option for %d in %+v", obj, d.Options)
	return -1
}

// miscPass submits the pending priority decision's pass option.
func miscPass(t *testing.T, e *Engine) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("expected a priority decision to pass, got %+v", d)
	}
	for _, o := range d.Options {
		if o.Kind == "pass" {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("priority decision with no pass option: %+v", d.Options)
}

// TestCounterDestinationMovesTheCounteredCard (Remand, Force of Will --
// param:api:Counter.Destination): Counter's Destination$ sends the countered
// card to the named zone instead of the default graveyard -- Remand's hand
// hand-off (with its SubAbility$ draw still running), and Force of Will's
// explicit Graveyard spelling taking the ordinary path.
func TestCounterDestinationMovesTheCounteredCard(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Grizzly Bears", "Grizzly Bears"},
		[]string{"Remand", "Force of Will"}, nil, nil)
	addMana(t, e, 0, "GG")
	addMana(t, e, 1, "UUUUU")
	bear := miscHandObj(t, e, 0, "Grizzly Bears")
	remand := miscHandObj(t, e, 1, "Remand")

	// Seat 0 casts the first Bear; seat 0 passes and seat 1 counters it with
	// Remand at the spell on the stack.
	submitChoices(t, e, miscCastOption(t, e, bear))
	miscPass(t, e)
	submitChoices(t, e, passToCast(t, e, remand))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Remand, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Obj == bear {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the bear spell was not offered as Remand's target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	p1Before := len(e.G.Zone(state.ZHand, 1))
	passUntilStackEmpty(t, e, 30)

	// Destination$ Hand: the countered spell is back in its OWNER's hand
	// (both Bears in hand again), never the graveyard, and the SubAbility$
	// draw gave seat 1 one more card (Remand itself went to the graveyard).
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZHand {
		t.Fatalf("Remand-countered Bear = %+v, want it back in seat 0's hand", o)
	}
	if n := countNamed(t, e, state.ZHand, 0, "Grizzly Bears"); n != 2 {
		t.Fatalf("seat 0 holds %d Bears after the Remand, want 2", n)
	}
	if o := e.G.Obj(remand); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("resolved Remand = %+v, want graveyard", o)
	}
	if n := len(e.G.Zone(state.ZHand, 1)); n != p1Before+1 {
		t.Fatalf("seat 1 holds %d cards after the resolution, want %d (one Remand draw)", n, p1Before+1)
	}

	// Force of Will carries Destination$ Graveyard explicitly: the countered
	// Bear takes the ordinary graveyard path. Fund both seats again (the
	// first phase's payments drained them).
	addMana(t, e, 0, "GG")
	addMana(t, e, 1, "UUUUU")
	bear2 := miscHandObj(t, e, 0, "Grizzly Bears")
	fow := miscHandObj(t, e, 1, "Force of Will")
	submitChoices(t, e, passToCast(t, e, bear2))
	miscPass(t, e)
	submitChoices(t, e, passToCast(t, e, fow))
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Force of Will, got %+v", d)
	}
	tIdx = -1
	for _, o := range d.Options {
		if o.Obj == bear2 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("the second bear spell was not offered as Force of Will's target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(bear2); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("Force of Will-countered Bear = %+v, want graveyard", o)
	}
	replayCheck(t, e, cfg)
}

// TestCopySpellAbilityControllerGivesTheCopyToThePayer (Chain Lightning --
// param:api:CopySpellAbility.Controller): the copy a paid unless-cost makes
// belongs to the Controller$ selector's player (CR 707.10's "they may copy
// this spell"), here TargetedOrController -- the targeted player -- rather
// than the spell's resolving controller. The caster is the active seat (a
// sorcery), so the payer/copy-owner under test is seat 1.
func TestCopySpellAbilityControllerGivesTheCopyToThePayer(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Chain Lightning"}, nil, nil, []string{"Grizzly Bears"})
	addMana(t, e, 0, "R")
	addMana(t, e, 1, "RR")
	bolt := miscHandObj(t, e, 0, "Chain Lightning")

	// Seat 0 casts Chain Lightning at seat 1 (the player). Its resolution
	// deals 3, then poses the unless-pay ask to the targeted player; paying
	// (the R R) makes the COPY, and Controller$ TargetedOrController makes
	// seat 1 -- the payer, not seat 0 -- its controller.
	submitChoices(t, e, miscCastOption(t, e, bolt))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected a target decision for Chain Lightning, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 was not offered as Chain Lightning's target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("expected seat 1's unless-pay ask after the damage, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index) // pay {R}{R}

	// The copy sits on the stack under seat 1's control.
	stackCopy := false
	for _, ev := range e.L.Events {
		stackCopy = stackCopy || (ev.Kind == events.StackCopy && ev.Player == 1)
	}
	copied := false
	for i := range e.G.Objs {
		o := &e.G.Objs[i]
		if o.IsCopy && o.Zone == state.ZStack {
			copied = true
			if o.Controller != 1 {
				t.Fatalf("the copy is controlled by seat %d, want the paying seat 1", o.Controller)
			}
		}
	}
	if !copied || !stackCopy {
		t.Fatalf("no seat-1-controlled copy was created for the paid unless-cost (copy %v, event %v)", copied, stackCopy)
	}

	// CR 707.10c: the copy's controller (seat 1) may choose new targets.
	// Keep the inherited player target (option 0) so the copy still hits
	// seat 1 and the life assertion below is meaningful.
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KTarget || d.ResumeKind != "copy_targets" {
		t.Fatalf("expected the copy's new-target ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[0].Index)

	// The copy resolves its kept target: 3 more damage to seat 1, whose pool
	// is now empty, so the copy's own unless ask must be a decline.
	d = passUntilNonPriority(t, e, 30)
	if d == nil || d.Kind != decision.KModes || d.Player != 1 {
		t.Fatalf("expected the copy's unless-pay ask, got %+v", d)
	}
	submitChoices(t, e, d.Options[len(d.Options)-1].Index) // decline
	passUntilStackEmpty(t, e, 30)
	if life := e.G.Players[1].Life; life != 14 {
		t.Fatalf("seat 1's life = %d, want 14 (3 + 3 damage)", life)
	}
	replayCheck(t, e, cfg)
}

// TestCouncilsJudgmentExilesTheMostVoted (param:api:Vote.VoteCard,
// param:api:Vote.VoteSubAbility): VoteCard$ builds the ballot from the
// battlefield permanents the filter admits (a nonland permanent the caster
// does not control), the votes are recorded, and VoteSubAbility$ exiles each
// permanent with the most votes or tied for most -- here the only eligible
// permanent, voted unanimously.
func TestCouncilsJudgmentExilesTheMostVoted(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Council's Judgment"}, nil,
		[]string{"Grizzly Bears"}, []string{"Grizzly Bears"})
	addMana(t, e, 0, "CWW")
	judgment := miscHandObj(t, e, 0, "Council's Judgment")
	opponentBear := miscBoardObj(t, e, 1, "Grizzly Bears")
	submitChoices(t, e, miscCastOption(t, e, judgment))
	miscPass(t, e) // seat 0's follow-up priority
	// VoteCard$ is a real private per-voter decision; both voters select the
	// only eligible permanent before the ordinary priority drain continues.
	voteAsks := 0
	for len(e.G.Stack) > 0 {
		d := e.Pending()
		if d == nil {
			t.Fatal("no decision while draining Council's Judgment")
		}
		if d.Kind == decision.KChoose {
			voteAsks++
			submitChoices(t, e, d.Options[0].Index)
			continue
		}
		passUntilStackEmpty(t, e, 1)
	}
	if voteAsks != 2 {
		t.Fatalf("Council's Judgment posed %d VoteCard asks, want one per voter (2)", voteAsks)
	}

	// The opponent's bear was the only legal ballot entry, every player
	// voted for it, and the exile sub-ability moved it. Seat 0's own bear is
	// outside the ballot and stays.
	if o := e.G.Obj(opponentBear); o == nil || o.Zone != state.ZExile {
		t.Fatalf("the voted-for opponent bear = %+v, want exile", o)
	}
	own := miscBoardObj(t, e, 0, "Grizzly Bears")
	if o := e.G.Obj(own); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("seat 0's own bear = %+v, want it untouched on the battlefield", o)
	}
	votes := 0
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "votes for Grizzly Bears" {
			votes++
		}
	}
	if votes != 2 {
		t.Fatalf("%d vote notes recorded, want one per voting player (2)", votes)
	}
	replayCheck(t, e, cfg)
}

// TestRoilingVortexFiresOnlyOnFreeCasts (param:trig:SpellCast.ValidSA):
// ValidSA$ Spell.ManaSpent EQ0 gates the cast trigger on the mana the
// activator actually spent -- a zero-mana cast (Ornithopter) triggers the 5
// damage, a mana-paid cast (Grizzly Bears) does not.
func TestRoilingVortexFiresOnlyOnFreeCasts(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Grizzly Bears", "Ornithopter"}, nil, []string{"Roiling Vortex"}, nil)
	addMana(t, e, 0, "GG")
	vortex := miscBoardObj(t, e, 0, "Roiling Vortex")
	if o := e.G.Obj(vortex); o == nil {
		t.Fatal("Roiling Vortex is not on the battlefield")
	}

	// The paid cast: one green mana left the pool for it, so the ValidSA$
	// comparison fails and no trigger fires.
	bear := miscHandObj(t, e, 0, "Grizzly Bears")
	submitChoices(t, e, miscCastOption(t, e, bear))
	miscPass(t, e)
	passUntilStackEmpty(t, e, 30)
	if life := e.G.Players[0].Life; life != 20 {
		t.Fatalf("seat 0's life = %d after the paid cast, want an untriggered 20", life)
	}

	// The free cast: nothing was spent, the trigger fires and Roiling Vortex
	// deals 5 to the caster.
	ornithopter := miscHandObj(t, e, 0, "Ornithopter")
	submitChoices(t, e, miscCastOption(t, e, ornithopter))
	miscPass(t, e)
	passUntilStackEmpty(t, e, 30)
	if life := e.G.Players[0].Life; life != 15 {
		t.Fatalf("seat 0's life = %d after the free cast, want the triggered 15", life)
	}
	replayCheck(t, e, cfg)
}

// TestSpellCastActivatorThisTurnCastGatesTheTrigger (The Lord of Pain --
// param:trig:SpellCast.ActivatorThisTurnCast): ActivatorThisTurnCast$ EQ1
// fires the cast trigger only on the activator's FIRST spell of the turn --
// the count includes the triggering cast itself -- so the second spell the
// same player casts that turn triggers nothing.
func TestSpellCastActivatorThisTurnCastGatesTheTrigger(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	e, cfg := miscHandsEngine(t, reg,
		[]string{"Grizzly Bears", "Grizzly Bears"}, nil,
		[]string{"The Lord of Pain"}, nil)
	addMana(t, e, 0, "GG")
	pain := miscBoardObj(t, e, 0, "The Lord of Pain")
	if o := e.G.Obj(pain); o == nil {
		t.Fatal("The Lord of Pain is not on the battlefield")
	}

	// First cast of the turn: the trigger fires and its target ask (another
	// player, CR 603.3d -- chosen as the trigger is put on the stack, before
	// priority) is answered with seat 1.
	bear := miscHandObj(t, e, 0, "Grizzly Bears")
	submitChoices(t, e, miscCastOption(t, e, bear))
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("expected the trigger's player-target ask, got %+v", d)
	}
	tIdx := -1
	for _, o := range d.Options {
		if o.Kind == "player" && o.Player == 1 {
			tIdx = o.Index
		}
	}
	if tIdx < 0 {
		t.Fatalf("seat 1 was not offered as the trigger's target: %+v", d.Options)
	}
	submitChoices(t, e, tIdx)
	passUntilStackEmpty(t, e, 30)
	// The damage amount is the cast spell's mana value (the castprov-era
	// TriggeredSpellAbility$CardManaCostLKI count expression): the Bears'
	// mana value 2 was previously UNREAD and degraded to 0 (the gap the
	// comment below used to pin an untouched life total on); the alias read
	// in effects/count.go's evalRefProperty made it real, so seat 1 now
	// loses exactly 2. What the shape still pins is the GATE: the trigger
	// asked on the first cast and not on the second.
	if life := e.G.Players[1].Life; life != 18 {
		t.Fatalf("seat 1's life = %d after the first cast's trigger, want 20 minus the Bears' mana value 2", life)
	}

	// Second cast of the turn: EQ1 is false (this is the activator's second
	// spell), so no trigger at all -- no ask, nothing to answer.
	addMana(t, e, 0, "GG")
	bear2 := miscHandObj(t, e, 0, "Grizzly Bears")
	submitChoices(t, e, miscCastOption(t, e, bear2))
	miscPass(t, e)
	n := passUntilStackEmpty(t, e, 30)
	d = e.Pending()
	if d == nil || d.Kind != decision.KPriority {
		t.Fatalf("after the second cast the pending decision is %+v, want plain priority (no trigger ask)", d)
	}
	if n < 0 {
		t.Fatal("unreachable")
	}
	replayCheck(t, e, cfg)
}
