package rules

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Feather, Radiant Arbiter's chosen-card copy chain (ticket
// agent-20260918T210307Z-cf9de221): the whole chain is pinned end to end on
// the real corpus card --
//
//	Whenever you cast a noncreature spell that targets only Feather, you may
//	choose any number of other creatures that spell could target and pay {2}
//	for each of those creatures. If you do, for each of those creatures, copy
//	that spell. The copy targets that creature.
//
// The pieces this pins:
//
//   - Mode$ SpellAbilityCast's CAST half (trigmatch_cast.go): before this
//     round the mode only ever saw AbilityPush events, so the trigger never
//     fired on a spell cast at all.
//   - the CanBeTargetedByTriggeredSpellAbility choice predicate
//     (effects/filter.go): the ChooseCard pool ("other creatures that spell
//     could target") is non-empty.
//   - Count$ChosenSize (effects/count.go): SVar:CopyCost:Count$ChosenSize/
//     Times.2 prices the pay election {2} per chosen creature, folded into a
//     generic amount through UnlessCostResolved (effects/unless.go), which the
//     unless-pay settlement charges through the ordinary mana path
//     (rules/unless_tape.go).
//   - CopySpellAbility's DefinedTarget$ ChosenCard (effects/copy.go): one copy
//     per chosen creature, each copy's target replaced by its own creature
//     (the StackCopy event's IDs), not the original's.
//
// The trigger-side target-shape parameters (IsSingleTarget$, TargetsValid$)
// are a separately ticketed narrowing (issue-agent-20260918T210307Z-b6ca5a70):
// this trigger still over-fires on a noncreature spell that does not target
// Feather, which is why the test casts the spell AT Feather and relies on
// nothing else being cast.

const featherBearSrc = "Name:Grizzly Bears\nManaCost:1G\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// featherGame seeds a Commander game whose seat 0 commander is the real
// corpus Feather, Radiant Arbiter, with two bears and one Defiant Strike seeded
// behind it (mountains fill the rest); Feather and both bears enter the
// battlefield through logged moves and the Strike waits in hand.
func featherGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	feather := corpusCommander(t, reg, "Feather, Radiant Arbiter")
	strike := corpusCommander(t, reg, "Defiant Strike")
	bear := card(t, featherBearSrc)
	e, cfg := colourIdentityGame(t, seed, FormatCommander, feather, nil, bear, bear, strike)
	featherID := moveCommanderToBattlefield(t, e, 0, "Feather, Radiant Arbiter")
	bear1 := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	bear2 := moveToBattlefieldByName(t, e, 0, "Grizzly Bears")
	strikeID := moveSeededToHand(t, e, 0, "Defiant Strike")
	return e, cfg, featherID, bear1, bear2, strikeID
}

// featherBearlessGame is featherGame without the bears on the battlefield:
// only Feather enters, so the choose pool is empty.
func featherBearlessGame(t *testing.T, seed uint64) (*Engine, Config, state.ObjID, state.ObjID) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	feather := corpusCommander(t, reg, "Feather, Radiant Arbiter")
	strike := corpusCommander(t, reg, "Defiant Strike")
	e, cfg := colourIdentityGame(t, seed, FormatCommander, feather, nil, strike)
	featherID := moveCommanderToBattlefield(t, e, 0, "Feather, Radiant Arbiter")
	strikeID := moveSeededToHand(t, e, 0, "Defiant Strike")
	e.priorityRound()
	return e, cfg, featherID, strikeID
}

// moveCommanderToBattlefield moves the seat's commander from the command
// zone onto the battlefield through a logged MoveZone.
func moveCommanderToBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZCommand, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZCommand, To: state.ZBattlefield})
			e.pending = nil
			e.priorityRound()
			return id
		}
	}
	t.Fatalf("commander %q not in seat %d's command zone", name, p)
	return 0
}

// castStrikeAtFeather funds five green mana, casts the Defiant Strike targeting
// Feather, and pays from the pool. It returns the spell's stack object id.
func castStrikeAtFeather(t *testing.T, e *Engine, featherID, strikeID state.ObjID) {
	t.Helper()
	addMana(t, e, 0, "WWWWW")
	e.priorityRound()
	d := e.Pending()
	if d == nil {
		t.Fatalf("no decision pending before casting")
	}
	idx := -1
	for _, o := range d.Options {
		if o.Kind == "cast" && o.Obj == strikeID {
			idx = o.Index
		}
	}
	if idx < 0 {
		t.Fatalf("no cast option for Defiant Strike: %+v", d.Options)
	}
	submitChoices(t, e, idx)
	// The target ask: only Feather.
	d = e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("Defiant Strike target ask = %+v, want a target ask", d)
	}
	ti := -1
	for _, o := range d.Options {
		if o.Obj == featherID {
			ti = o.Index
		}
	}
	if ti < 0 {
		t.Fatalf("Feather not offered as a Defiant Strike target: %+v", d.Options)
	}
	submitChoices(t, e, ti)
}

// drainUntilKind passes priority (and answers any other ask with its first
// option) until a decision of the wanted kinds is pending; it returns it.
func drainUntilKind(t *testing.T, e *Engine, want ...decision.Kind) *decision.Decision {
	t.Helper()
	for i := 0; i < 60; i++ {
		d := e.Pending()
		if d == nil {
			t.Fatalf("no decision pending and no %v ask ever appeared", want)
		}
		for _, k := range want {
			if d.Kind == k {
				return d
			}
		}
		idx := -1
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
		} else if len(d.Options) > 0 {
			idx = d.Options[0].Index
		}
		if idx < 0 {
			t.Fatalf("no answer for %+v", d)
		}
		submitChoices(t, e, idx)
	}
	t.Fatalf("drain budget exhausted without a %v ask", want)
	return nil
}

// chooseCards submits a KChoose answer naming the given objects.
func chooseCards(t *testing.T, d *decision.Decision, e *Engine, objs ...state.ObjID) {
	t.Helper()
	var idxs []int
	for _, obj := range objs {
		found := false
		for _, o := range d.Options {
			if o.Obj == obj {
				idxs = append(idxs, o.Index)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("object %d not among the choose options %+v", obj, d.Options)
		}
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: idxs}); err != nil {
		t.Fatalf("submit choose %v: %v", idxs, err)
	}
}

// stackCopiesWithTargets collects the log's StackCopy events with their ID
// target overrides.
func stackCopiesWithTargets(e *Engine) []events.Event {
	var out []events.Event
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy {
			out = append(out, ev)
		}
	}
	return out
}

// TestFeatherChosenCopyAskPricedAndTargets is the Done pin: two chosen
// creatures make the pay election cost {4} (Count$ChosenSize/Times.2), the
// payment drains the pool, and EACH paid copy targets its own creature --
// both bears end up +1/+0, not one bear +6/+6 and the other untouched.
func TestFeatherChosenCopyAskPricedAndTargets(t *testing.T) {
	t.Parallel()
	e, cfg, featherID, bear1, bear2, strikeID := featherGame(t, 771)

	castStrikeAtFeather(t, e, featherID, strikeID)

	// The trigger fired on the cast (the SpellAbilityCast cast half) and its
	// ChooseCard ask is pending: two bears eligible, Feather herself excluded
	// by Other.
	d := drainUntilKind(t, e, decision.KChoose)
	if d.Prompt != "Choose any number of other creatures that spell could target" {
		t.Fatalf("choose prompt = %q", d.Prompt)
	}
	if d.Min != 0 || d.Max != 2 {
		t.Fatalf("choose bounds = %d..%d, want 0..2 (the eligible count)", d.Min, d.Max)
	}
	if n := len(d.Options); n != 2 {
		t.Fatalf("choose options = %d (%+v), want the two bears", n, d.Options)
	}
	chooseCards(t, d, e, bear1, bear2)

	// The pay election: priced {4} per the two chosen creatures, "paying"
	// CAUSES the copies (UnlessSwitched$ True).
	d = drainUntilKind(t, e, decision.KModes)
	if d.Min != 1 || d.Max != 1 || len(d.Options) != 2 {
		t.Fatalf("pay election = %+v, want a two-option KModes", d)
	}
	if !strings.Contains(d.Prompt, "{4}") || !strings.Contains(d.Prompt, "copy the spell") {
		t.Fatalf("pay prompt = %q, want the {4} copy offer", d.Prompt)
	}
	if got := e.G.Players[0].Pool.Total(); got != 4 {
		t.Fatalf("pool before the pay election = %d, want the {W} cast payment to have left 4", got)
	}
	submitChoices(t, e, 0) // pay
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying = %d, want the {4} charged", got)
	}

	// The copies: one per chosen creature, each with its OWN creature as its
	// target (the StackCopy IDs override), never the original's target.
	copies := stackCopiesWithTargets(e)
	if len(copies) != 2 {
		t.Fatalf("StackCopy events = %d, want one per chosen creature", len(copies))
	}
	seen := map[state.ObjID]state.ObjID{}
	for _, ev := range copies {
		if len(ev.IDs) != 1 || (ev.IDs[0] != bear1 && ev.IDs[0] != bear2) {
			t.Fatalf("StackCopy IDs = %v, want one chosen bear per copy", ev.IDs)
		}
		if other, dup := seen[ev.IDs[0]]; dup {
			t.Fatalf("two copies target the same creature (second copy targets %d, like copy %d)", ev.IDs[0], other)
		}
		seen[ev.IDs[0]] = ev.Obj
	}
	if len(seen) != 2 {
		t.Fatalf("copies targeted %d distinct creatures, want both bears", len(seen))
	}
	// Both copies are live on the stack, each with its OWN target recorded.
	top := e.G.Stack
	if len(top) < 3 {
		t.Fatalf("stack = %v, want the two copies above the original", top)
	}
	for _, cid := range top[len(top)-2:] {
		co := e.G.Obj(cid)
		if co == nil || !co.IsCopy {
			t.Fatalf("stack object %d is not a copy", cid)
		}
		if len(co.Targets) != 1 || (co.Targets[0].Obj != bear1 && co.Targets[0].Obj != bear2) {
			t.Fatalf("copy %d targets = %+v, want its own bear", cid, co.Targets)
		}
	}

	// Everything resolves: each bear +1/+0 from its OWN copy, Feather +1/+0
	// from the original.
	drainUntilPriority(t, e)
	if got := e.Power(bear1); got != 3 || e.Toughness(bear1) != 2 {
		t.Fatalf("bear1 = %d/%d, want 3/2 from its own copy", e.Power(bear1), e.Toughness(bear1))
	}
	if got := e.Power(bear2); got != 3 || e.Toughness(bear2) != 2 {
		t.Fatalf("bear2 = %d/%d, want 3/2 from its own copy", e.Power(bear2), e.Toughness(bear2))
	}
	if got := e.Power(featherID); got != 5 || e.Toughness(featherID) != 3 {
		t.Fatalf("Feather = %d/%d, want 5/3 from the original spell", e.Power(featherID), e.Toughness(featherID))
	}
	featherReplayCheck(t, e, cfg)
}

// TestFeatherChosenCopyDeclineCopiesNothing pins the switched orientation's
// decline half: declining the {4} election copies nothing (and, with nothing
// chosen, no pay election is posed at all).
func TestFeatherChosenCopyDeclineCopiesNothing(t *testing.T) {
	t.Parallel()
	e, cfg, featherID, bear1, _, strikeID := featherGame(t, 772)

	castStrikeAtFeather(t, e, featherID, strikeID)
	d := drainUntilKind(t, e, decision.KChoose)
	chooseCards(t, d, e, bear1) // one creature -> {2}
	d = drainUntilKind(t, e, decision.KModes)
	if !strings.Contains(d.Prompt, "{2}") {
		t.Fatalf("pay prompt = %q, want the {2} one-creature offer", d.Prompt)
	}
	submitChoices(t, e, 1) // decline
	drainUntilPriority(t, e)
	if copies := stackCopiesWithTargets(e); len(copies) != 0 {
		t.Fatalf("declined pay still copied %d times", len(copies))
	}
	if got := e.Power(bear1); got != 2 {
		t.Fatalf("bear1 = %d, want untouched", got)
	}
	if got := e.Power(featherID); got != 5 {
		t.Fatalf("Feather = %d, want the original spell's +1 only", got)
	}
	featherReplayCheck(t, e, cfg)
}

// TestFeatherEmptyChoiceNeverAsksPay pins the empty-choice gate: with no
// eligible other creature the ChooseCard ask is never posted (its only legal
// answer is empty) and the pay election is never posed either.
func TestFeatherEmptyChoiceNeverAsksPay(t *testing.T) {
	t.Parallel()
	e, cfg, featherID, strikeID := featherBearlessGame(t, 773)
	castStrikeAtFeather(t, e, featherID, strikeID)
	// No eligible creatures -> no ChooseCard ask (the empty-answer shape) and
	// no pay election: the chain resolves straight back to priority.
	drainUntilPriority(t, e)
	for _, ev := range e.L.Events {
		if ev.Kind == events.StackCopy {
			t.Fatalf("empty choice still copied (StackCopy %d)", ev.Obj)
		}
	}
	if got := e.Power(featherID); got != 5 {
		t.Fatalf("Feather = %d, want the original spell's +1 only", got)
	}
	featherReplayCheck(t, e, cfg)
}

// drainUntilPriority passes priority until the stack is empty and a plain
// priority decision is pending for seat 0.
func drainUntilPriority(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 80; i++ {
		if len(e.G.Stack) == 0 {
			d := e.Pending()
			if d == nil {
				e.priorityRound()
				return
			}
			if d.Kind == decision.KPriority {
				return
			}
		}
		d := e.Pending()
		if d == nil {
			e.priorityRound()
			continue
		}
		idx := -1
		if d.Kind == decision.KPriority {
			for _, o := range d.Options {
				if o.Kind == "pass" {
					idx = o.Index
				}
			}
		} else if len(d.Options) > 0 {
			idx = d.Options[0].Index
		}
		if idx < 0 {
			t.Fatalf("no answer for %+v", d)
		}
		submitChoices(t, e, idx)
	}
	t.Fatal("drainUntilPriority budget exhausted")
}

// featherReplayCheck is commanderReplayCheck with the Commanders bookkeeping
// read from cfg instead of the reconstructed command zone: this fixture's
// Feather left the command zone through a logged move, so a zone read finds
// no commander there while the live game keeps the genesis seating.
func featherReplayCheck(t *testing.T, e *Engine, cfg Config) {
	t.Helper()
	g := state.NewGameLife(cfg.Names, cfg.StartingLife)
	g.Tokens = cfg.Tokens
	deckIDs := make([][]state.ObjID, len(cfg.Decks))
	for i, deck := range cfg.Decks {
		p := state.PlayerID(i)
		ids := make([]state.ObjID, 0, len(deck))
		for _, c := range deck {
			ids = append(ids, g.AddObject(c, p).ID)
		}
		g.SetZone(state.ZLibrary, p, ids)
		deckIDs[i] = ids
	}
	for _, ev := range e.L.Events {
		events.Apply(g, ev)
	}
	totalCmd := 0
	for _, cs := range cfg.Commanders {
		totalCmd += len(cs)
	}
	for p := range cfg.Commanders {
		pl := &g.Players[p]
		if len(cfg.Commanders[p]) > 0 {
			// cfg.Commanders names DECK indices; the live bookkeeping carries
			// OBJECT ids, which the deck's own AddObject order assigns.
			for _, ci := range cfg.Commanders[p] {
				pl.Commanders = append(pl.Commanders, deckIDs[p][ci])
			}
			pl.CmdCasts = make([]int32, len(pl.Commanders))
		}
		if totalCmd > 0 {
			pl.CmdDamage = make([]int32, totalCmd)
		}
	}
	if diff := diffGames(e.G, g); diff != "" {
		t.Fatalf("log-only replay differs:\n%s", diff)
	}
}
