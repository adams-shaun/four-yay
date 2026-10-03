package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"

	"github.com/adams-shaun/gorge/state"
)

// The cast/activation trigger family's target-shape parameters (task
// targetsvalid1): TargetsValid$ and IsSingleTarget$. Before this both were
// unread, so a "Whenever you cast a spell that targets CARDNAME" trigger
// (the whole Heroic family) and Feather, Radiant Arbiter's "Whenever you
// cast a noncreature spell that targets only CARDNAME" fired on EVERY cast
// of a matching spell -- wrong-wide.
//
// Pins, on the real corpus cards plus one synthetic IsSingleTarget probe:
//
//   - Feather, Radiant Arbiter (Mode$ SpellAbilityCast, the spell arm): the
//     trigger fires for a noncreature spell targeting only her and NOT for a
//     noncreature spell targeting something else, nor for a creature spell;
//   - War-Wing Siren (Mode$ SpellCast, the Heroic shape): fires for a
//     spell targeting it, not for a spell targeting another creature;
//   - the synthetic IsSingleTarget$ probe: a one-target spell fires, a
//     two-target spell does not.
//
// Feather's resolution half (the ChosenSize copy ask) is a separate ticket;
// its ChooseCard here degrades to an empty choice, so the drain answers an
// empty KChoose and must settle.

const targetsValidProbe = "Name:Probe\nManaCost:1 R\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ 2\nOracle:Pump a creature.\n"

// targetsValidEngine puts the given fixture card on seat 0's battlefield
// (the trigger SOURCE, un-cast) and the given cards in seat 0's hand, funds
// the pool and re-asks priority.
func targetsValidEngine(t *testing.T, battlefield *cards.Card, hand ...*cards.Card) (*Engine, state.ObjID) {
	t.Helper()
	e := handEngine(t, append([]*cards.Card{battlefield}, hand...)...)
	bf := e.G.Zone(state.ZHand, 0)[0]
	e.emit(events.Event{Kind: events.MoveZone, Obj: bf, From: state.ZHand, To: state.ZBattlefield})
	drainQueuedTriggers(t, e)
	for _, r := range "WWWWRRRRGG" {
		e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: string(r), Amount: 1})
	}
	e.priorityRound()
	return e, bf
}

// targetsValidAddToHand drops a fixture card into seat 0's hand and re-asks
// priority, so the next beginCast sees it.
func targetsValidAddToHand(t *testing.T, e *Engine, c *cards.Card) state.ObjID {
	t.Helper()
	o := e.G.AddObject(c, 0)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
	e.priorityRound()
	return o.ID
}

// castTargetsValidProbe casts the named card in seat 0's hand and answers
// its target ask with the object target named by want (or the first creature
// option when want is 0). Returns once the cast transaction has completed --
// targets recorded, costs paid, deferred cast trigger queued.
func castTargetsValidProbe(t *testing.T, e *Engine, name string, want state.ObjID) {
	t.Helper()
	id := state.ObjID(0)
	for _, hid := range e.G.Zone(state.ZHand, 0) {
		if o := e.G.Obj(hid); o != nil && o.Face() != nil && o.Face().Name == name {
			id = hid
		}
	}
	if id == 0 {
		t.Fatalf("%s not in hand", name)
	}
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the probe cast did not ask for a target: %+v", d)
	}
	idx := -1
	for i, o := range d.Options {
		if want == 0 {
			if o.Kind != "player" && idx < 0 {
				idx = i
			}
			continue
		}
		if o.Kind != "player" && o.Obj == want {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("no target option for %d: %+v", want, d.Options)
	}
	submitChoices(t, e, idx)
}

// queuedTriggersFrom counts queued triggers plus on-stack trigger ability
// objects whose source is src. A fired cast trigger may already have been
// pushed onto the stack by the time the cast transaction returns (the
// engine auto-pushes pending triggers when it re-asks priority), so both
// places are read.
func queuedTriggersFrom(e *Engine, src state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == src {
			n++
		}
	}
	for _, sid := range e.G.Stack {
		o := e.G.Obj(sid)
		if o != nil && o.Source == src && o.Zone == state.ZStack {
			n++
		}
	}
	return n
}

// drainTargetsValid settles the queue after a fired trigger, answering a
// KChoose (Feather's ChooseCard degrade, Min 0) with the empty pick and a
// KArrange (Siren's scry) with keep-top. A KPriority here means the drain
// is done.
func drainTargetsValid(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 50; i++ {
		// A pending non-priority ask must be handled BEFORE the stack is
		// resolved: the unless ask a resolving trigger poses sits pending
		// while the suspending ability stays on the stack, and resolving
		// under a live ask would loop forever.
		if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
			switch d.Kind {
			case decision.KChoose, decision.KArrange:
				submitChoices(t, e, 0)
			case decision.KModes:
				submitChoices(t, e, 1) // the decline ("Don't pay")
			case decision.KTarget:
				// CR 707.10c: keep the copy's inherited targets (the leading
				// d.Min options, placed first).
				if d.ResumeKind != "copy_targets" {
					t.Fatalf("unexpected non-copy target decision during the drain: %+v", d)
				}
				ch := []int{}
				keep := d.Min
				if keep < 1 {
					keep = 1
				}
				for j := 0; j < len(d.Options) && j < keep; j++ {
					ch = append(ch, d.Options[j].Index)
				}
				submitChoices(t, e, ch...)
			default:
				t.Fatalf("unexpected decision %v during the drain: %+v", d.Kind, d)
			}
			continue
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		if len(e.G.Stack) > 0 {
			e.resolveTop()
			continue
		}
		return
	}
	t.Fatalf("the drain did not settle")
}

// TestFeatherRadiantArbiterFiresOnSpellTargetingOnlyHer: Feather's
// "Whenever you cast a noncreature spell that targets only CARDNAME" fires
// for an instant targeting only her.
func TestFeatherRadiantArbiterFiresOnSpellTargetingOnlyHer(t *testing.T) {
	t.Parallel()
	feather := corpusAlternativeCard(t, "Feather, Radiant Arbiter")
	probe := card(t, targetsValidProbe)
	e, featherID := targetsValidEngine(t, feather, probe)
	castTargetsValidProbe(t, e, "Probe", featherID)
	if n := queuedTriggersFrom(e, featherID); n != 1 {
		t.Fatalf("Feather queued %d triggers for a spell targeting only her, want 1", n)
	}
	drainTargetsValid(t, e)
}

// TestFeatherRadiantArbiterSilentOnSpellTargetingSomethingElse: the same
// trigger must NOT fire for an instant targeting another creature.
func TestFeatherRadiantArbiterSilentOnSpellTargetingSomethingElse(t *testing.T) {
	t.Parallel()
	feather := corpusAlternativeCard(t, "Feather, Radiant Arbiter")
	probe := card(t, targetsValidProbe)
	e, featherID := targetsValidEngine(t, feather, probe)
	bear := putToken(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2", state.ZBattlefield)
	castTargetsValidProbe(t, e, "Probe", bear)
	if n := queuedTriggersFrom(e, featherID); n != 0 {
		t.Fatalf("Feather queued %d triggers for a spell targeting Bear, want 0", n)
	}
}

// TestFeatherRadiantArbiterSilentOnCreatureSpell: ValidSA$ Spell.nonCreature
// keeps a creature cast silent even though TargetsValid$ cannot be met by a
// target-less cast either.
func TestFeatherRadiantArbiterSilentOnCreatureSpell(t *testing.T) {
	t.Parallel()
	feather := corpusAlternativeCard(t, "Feather, Radiant Arbiter")
	e, featherID := targetsValidEngine(t, feather)
	bear := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	id := targetsValidAddToHand(t, e, bear)
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	if d := e.Pending(); d != nil && d.Kind != decision.KPriority {
		t.Fatalf("unexpected decision %v casting a vanilla creature: %+v", d.Kind, d)
	}
	if n := queuedTriggersFrom(e, featherID); n != 0 {
		t.Fatalf("Feather queued %d triggers for a creature spell, want 0", n)
	}
}

// TestWarWingSirenHeroicFiresOnlyOnSpellsTargetingIt: the SpellCast arm
// (the Heroic shape) reads TargetsValid$ Card.Self off the real corpus card.
func TestWarWingSirenHeroicFiresOnlyOnSpellsTargetingIt(t *testing.T) {
	t.Parallel()
	hoplite := corpusAlternativeCard(t, "War-Wing Siren")
	probe := card(t, targetsValidProbe)
	e, hopliteID := targetsValidEngine(t, hoplite, probe)
	castTargetsValidProbe(t, e, "Probe", hopliteID)
	if n := queuedTriggersFrom(e, hopliteID); n != 1 {
		t.Fatalf("Siren queued %d triggers for a spell targeting it, want 1", n)
	}
	drainTargetsValid(t, e)
	if got := len(e.G.Obj(hopliteID).Counters); got == 0 {
		t.Fatalf("the fired Heroic trigger put no +1/+1 counter on Siren")
	}
	// A second spell targeting Bear does not fire it again.
	probe2 := card(t, "Name:Probe II\nManaCost:1 R\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | NumAtt$ 2\nOracle:Pump a creature.\n")
	targetsValidAddToHand(t, e, probe2)
	bear := putToken(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2", state.ZBattlefield)
	castTargetsValidProbe(t, e, "Probe II", bear)
	if n := queuedTriggersFrom(e, hopliteID); n != 0 {
		t.Fatalf("Siren fired for a spell targeting Bear, want silent")
	}
}

// TestIsSingleTargetParamOneTargetFiresTwoDoesNot: IsSingleTarget$ True on a
// synthetic SpellCast trigger -- exactly one target fires, two do not.
func TestIsSingleTargetParamOneTargetFiresTwoDoesNot(t *testing.T) {
	t.Parallel()
	watch := card(t, "Name:Watcher\nManaCost:1 G\nTypes:Creature\nPT:2/2\n"+
		"T:Mode$ SpellCast | ValidActivatingPlayer$ You | IsSingleTarget$ True | TriggerZones$ Battlefield | Execute$ TrigDraw\n"+
		"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"Oracle:Whenever you cast a spell that has a single target, draw a card.\n")
	probe := card(t, targetsValidProbe)
	e, watchID := targetsValidEngine(t, watch, probe)
	putToken(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2", state.ZBattlefield)
	castTargetsValidProbe(t, e, "Probe", 0)        // one target (the first creature option)
	handAfterCast := len(e.G.Zone(state.ZHand, 0)) // the probe has left the hand
	if n := queuedTriggersFrom(e, watchID); n != 1 {
		t.Fatalf("the watcher queued %d triggers for a one-target spell, want 1", n)
	}
	drainTargetsValid(t, e)
	if got := len(e.G.Zone(state.ZHand, 0)) - handAfterCast; got != 1 {
		t.Fatalf("the one-target spell's trigger drew %d cards, want 1", got)
	}
	// A two-target spell (TargetMax$ 2, both picks) must not fire it again.
	two := card(t, "Name:Probe II\nManaCost:1 R\nTypes:Instant\nA:SP$ Pump | ValidTgts$ Creature | TargetMax$ 2 | NumAtt$ 1\nOracle:Pump creatures.\n")
	twoID := targetsValidAddToHand(t, e, two)
	e.beginCast(0, decision.Option{Kind: "cast", Obj: twoID})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the two-target probe did not ask for targets: %+v", d)
	}
	// Two picks: the first replaces, the second appends.
	idx1, idx2 := -1, -1
	for i, opt := range d.Options {
		if opt.Kind == "player" {
			continue
		}
		if idx1 < 0 {
			idx1 = i
		} else if idx2 < 0 {
			idx2 = i
		}
	}
	if idx1 < 0 || idx2 < 0 {
		t.Fatalf("the two-target ask lacks two creature options: %+v", d.Options)
	}
	// A multi-target ask takes all picks in ONE intent (Min..Max choices);
	// handleTarget's first-replaces-then-appends shapes come from the picks'
	// positions in that one answer.
	submitChoices(t, e, idx1, idx2)
	if n := queuedTriggersFrom(e, watchID); n != 0 {
		t.Fatalf("the watcher fired on a two-target spell (queued %d), want 0", n)
	}
}

// ---------- The activation arm (round 3) ----------
//
// The round-2 diff wired trigmatch.TargetShapeMatches into trigmatch.abilityCastMatches, but the
// match there is evaluated synchronously inside payCast's AbilityPush emit —
// BEFORE handleTarget's ability branch records the chosen targets onto the
// minted object, and against ev.Obj (the SOURCE permanent, whose own Targets
// is always empty). Both params were therefore permanently silent on this
// arm: a measured probe queued 0 where the param-less shape queued 1. The
// fix reads the activation's chosen targets off the pending cast (pc.targets,
// held by payCast for exactly this window). The tests below are the probe,
// kept: an ability-cast trigger with TargetsValid$ must actually FIRE.
//
// These assertions count only e.pendingTriggers (the trigger is queued at
// the AbilityPush emit and nothing re-asks priority before the assert, so it
// is still pending). queuedTriggersFrom cannot be used on this arm: the
// activated ability object sitting on the stack also carries Source == the
// source permanent and would count as a false positive.

// abilityTargetsValidWatcher carries an AbilityCast trigger with
// TargetsValid$ Creature and two abilities: a pump targeting a creature and
// a damage spell targeting a player.
const abilityTargetsValidWatcher = "Name:Watcher\nManaCost:1 G\nTypes:Creature\nPT:2/2\n" +
	"T:Mode$ AbilityCast | ValidActivatingPlayer$ You | TargetsValid$ Creature | TriggerZones$ Battlefield | Execute$ TrigDraw\n" +
	"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\n" +
	"A:AB$ Pump | ValidTgts$ Creature | NumAtt$ 2 | Cost$ 1 R\n" +
	"A:AB$ DealDamage | ValidTgts$ Player | NumDmg$ 1 | Cost$ 1 R\n" +
	"Oracle:x\n"

// triggersFromSource counts the source's fired triggers: queued-but-not-yet-
// pushed entries (pendingTriggers) plus trigger objects already pushed onto
// the stack -- the stack side filtered through state.TriggerOf because the
// ACTIVATED ability object sitting beside the trigger shares its Source and
// must not count.
func triggersFromSource(e *Engine, src state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == src {
			n++
		}
	}
	for _, sid := range e.G.Stack {
		o := e.G.Obj(sid)
		if o == nil || o.Zone != state.ZStack || o.Source != src {
			continue
		}
		if _, isTrig := state.TriggerOf(e.G, o); isTrig {
			n++
		}
	}
	return n
}

// activateTargetsValid activates src's ability at index ability and answers
// its target ask with the object targets named by targets (in order).
func activateTargetsValid(t *testing.T, e *Engine, src state.ObjID, ability int, targets ...state.ObjID) {
	t.Helper()
	e.beginActivation(0, decision.Option{Kind: "ability", Obj: src, Ability: ability})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the activation did not ask for targets: %+v", d)
	}
	used := map[int]bool{}
	idxs := make([]int, 0, len(targets))
	for _, want := range targets {
		idx := -1
		for i, o := range d.Options {
			if used[i] || o.Kind == "player" {
				continue
			}
			if o.Obj == want {
				idx = i
				break
			}
		}
		if idx < 0 {
			t.Fatalf("no target option for %d: %+v", want, d.Options)
		}
		used[idx] = true
		idxs = append(idxs, idx)
	}
	submitChoices(t, e, idxs...)
}

// TestAbilityCastTargetsValidFiresOnCreatureTargetingActivation: the
// round-2 probe, now green — an AbilityCast trigger with TargetsValid$
// Creature FIRES for an activation whose target is a creature (the watcher's
// own pump targeting itself), and the fired trigger's draw resolves.
func TestAbilityCastTargetsValidFiresOnCreatureTargetingActivation(t *testing.T) {
	t.Parallel()
	watch := card(t, abilityTargetsValidWatcher)
	e, watchID := targetsValidEngine(t, watch)
	activateTargetsValid(t, e, watchID, 0, watchID)
	if n := triggersFromSource(e, watchID); n != 1 {
		t.Fatalf("the watcher queued %d triggers for a creature-targeting activation, want 1", n)
	}
	handAfterCast := len(e.G.Zone(state.ZHand, 0)) // the ability is on the stack, not the hand
	drainTargetsValid(t, e)
	if got := len(e.G.Zone(state.ZHand, 0)) - handAfterCast; got != 1 {
		t.Fatalf("the creature-targeting activation's trigger drew %d cards, want 1", got)
	}
}

// TestAbilityCastTargetsValidSilentOnPlayerTargetingActivation: the same
// trigger stays silent for an activation targeting a player (the watcher's
// damage ability), and for an activation with no targets at all there is no
// TargetsValid$ match either.
func TestAbilityCastTargetsValidSilentOnPlayerTargetingActivation(t *testing.T) {
	t.Parallel()
	watch := card(t, abilityTargetsValidWatcher)
	e, watchID := targetsValidEngine(t, watch)
	// ability 1: DealDamage targeting a player. The ask offers player options;
	// take the first one.
	e.beginActivation(0, decision.Option{Kind: "ability", Obj: watchID, Ability: 1})
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget {
		t.Fatalf("the damage activation did not ask for targets: %+v", d)
	}
	pidx := -1
	for i, o := range d.Options {
		if o.Kind == "player" {
			pidx = i
			break
		}
	}
	if pidx < 0 {
		t.Fatalf("the damage ask offers no player option: %+v", d.Options)
	}
	submitChoices(t, e, pidx)
	if n := triggersFromSource(e, watchID); n != 0 {
		t.Fatalf("the watcher fired on a player-targeting activation (queued %d), want 0", n)
	}
}

// TestAbilityCastIsSingleTargetOnActivation: IsSingleTarget$ True on the
// activation arm — a two-target activation stays silent, a one-target one
// fires.
func TestAbilityCastIsSingleTargetOnActivation(t *testing.T) {
	t.Parallel()
	watch := card(t, "Name:Watcher\nManaCost:1 G\nTypes:Creature\nPT:2/2\n"+
		"T:Mode$ AbilityCast | ValidActivatingPlayer$ You | IsSingleTarget$ True | TriggerZones$ Battlefield | Execute$ TrigDraw\n"+
		"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"A:AB$ Pump | ValidTgts$ Creature | TargetMax$ 2 | NumAtt$ 1 | Cost$ 1 R\n"+
		"Oracle:x\n")
	e, watchID := targetsValidEngine(t, watch)
	bear1 := putToken(t, e, 0, "Name:Bear\nTypes:Creature\nPT:2/2", state.ZBattlefield)
	bear2 := putToken(t, e, 0, "Name:Bearer\nTypes:Creature\nPT:2/2", state.ZBattlefield)
	activateTargetsValid(t, e, watchID, 0, bear1, bear2)
	if n := triggersFromSource(e, watchID); n != 0 {
		t.Fatalf("the watcher fired on a two-target activation (queued %d), want 0", n)
	}
	drainTargetsValid(t, e) // resolve the two-target pump
	e.priorityRound()
	activateTargetsValid(t, e, watchID, 0, bear1)
	if n := triggersFromSource(e, watchID); n != 1 {
		t.Fatalf("the watcher queued %d triggers for a one-target activation, want 1", n)
	}
	drainTargetsValid(t, e)
}

// TestSpellAbilityCastSpellArmValidCardRead: the spell arm's ValidCard$ is
// read (round-2 MINOR): a SpellAbilityCast trigger restricted to Instant
// spells stays silent for a creature cast and fires for an instant cast.
func TestSpellAbilityCastSpellArmValidCardRead(t *testing.T) {
	t.Parallel()
	watch := card(t, "Name:Watcher\nManaCost:1 G\nTypes:Creature\nPT:2/2\n"+
		"T:Mode$ SpellAbilityCast | ValidActivatingPlayer$ You | ValidCard$ Instant | TriggerZones$ Battlefield | Execute$ TrigDraw\n"+
		"SVar:TrigDraw:DB$ Draw | Defined$ You | NumCards$ 1\n"+
		"Oracle:x\n")
	creature := card(t, "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n")
	e, watchID := targetsValidEngine(t, watch, creature)
	id := e.G.Zone(state.ZHand, 0)[0]
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	if n := triggersFromSource(e, watchID); n != 0 {
		t.Fatalf("the watcher fired on a creature cast under ValidCard$ Instant (queued %d), want 0", n)
	}
	drainTargetsValid(t, e) // resolve the vanilla cast
	probe := card(t, targetsValidProbe)
	targetsValidAddToHand(t, e, probe)
	castTargetsValidProbe(t, e, "Probe", 0)
	if n := triggersFromSource(e, watchID); n != 1 {
		t.Fatalf("the watcher queued %d triggers for an instant cast, want 1", n)
	}
	drainTargetsValid(t, e)
}
