package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// requireOneEventTrigger proves the real corpus T: line was queued. These
// tests deliberately stop at placement: the trigger effects themselves have
// independent primitive coverage, while this file pins the event boundary
// each Mode$ spelling observes.
func requireOneEventTrigger(t *testing.T, e *Engine, name string) {
	t.Helper()
	if len(e.pendingTriggers) != 1 {
		t.Fatalf("%s queued %d triggers, want 1", name, len(e.pendingTriggers))
	}
}

func TestSacrificedTriggerMayhemDevil(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Mayhem Devil"))
	victim := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.MoveZone, Obj: victim, From: state.ZBattlefield,
		To: state.ZGraveyard, Text: "sacrificed"})
	requireOneEventTrigger(t, e, "Mayhem Devil")
}

// observedTriggerCount counts a source's trigger exactly once whether it is
// still waiting in the trigger queue or has already been represented by the
// replayed TriggerPush event. Submit calls Advance, so effect/cleanup tests can
// legitimately observe either side of that placement boundary.
func observedTriggerCount(e *Engine, source state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == source {
			n++
		}
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.TriggerPush && ev.Obj == source {
			n++
		}
	}
	return n
}

// TestDiscardedTriggerNecropotenceFromResolvingEffect drives Mind Peel's real
// corpus Discard SA through resolveTop and its mid-resolution answer. This
// guards the producer boundary: effDiscard itself must mark the MoveZone, not
// rely on a test manufacturing a discard label.
func TestDiscardedTriggerNecropotenceFromResolvingEffect(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	necro := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Necropotence"))
	mindPeel := e.G.AddObject(mustCorpusCard(t, reg, "Mind Peel"), 1)
	mindPeel.Zone = state.ZStack
	mindPeel.Targets = []state.Target{{Player: 0, IsPlayer: true}}
	e.G.SetZone(state.ZStack, 0, []state.ObjID{mindPeel.ID})

	e.resolveTop()
	d := e.Pending()
	if d == nil || d.ResumeKind != "discard" || len(d.Options) == 0 {
		t.Fatalf("Mind Peel discard decision = %+v, want a real discard ask", d)
	}
	discarded := d.Options[0].Obj
	submitChoices(t, e, d.Options[0].Index)
	if got := observedTriggerCount(e, necro); got != 1 {
		t.Fatalf("Necropotence observed triggers = %d, want 1 from Mind Peel", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Obj == discarded && events.IsDiscard(ev) && !events.IsDiscardCost(ev) {
			found = true
		}
	}
	if !found {
		t.Fatal("Mind Peel moved the chosen card without a canonical discard event")
	}
}

// TestDiscardedTriggerNecropotenceFromCleanup drives the CR 514.1 decision
// path. Cleanup is not an effect primitive, so it has its own discard producer
// and must carry the same action marker without masquerading as a cost.
func TestDiscardedTriggerNecropotenceFromCleanup(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	necro := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Necropotence"))
	e.G.Active = 0
	e.G.Step = state.StepCleanup
	extra := onHand(t, e, 0, "Name:Cleanup Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")

	e.priorityRound()
	submitDiscard(t, e, extra)
	if got := observedTriggerCount(e, necro); got != 1 {
		t.Fatalf("Necropotence observed triggers = %d, want 1 from cleanup discard", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Obj == extra && events.IsDiscard(ev) && !events.IsDiscardCost(ev) {
			found = true
		}
	}
	if !found {
		t.Fatal("cleanup moved the chosen card without a canonical discard event")
	}
}

func TestDiscardedTriggerValidCauseRejectsCosts(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	orvar := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Orvar, the All-Form"))
	var tr cards.Trigger
	for _, candidate := range e.G.Obj(orvar).Face().Triggers {
		if candidate.Mode == "Discarded" {
			tr = candidate
			break
		}
	}
	if tr.Effect == nil || tr.Params["ValidCause"] != "SpellAbility.OppCtrl" {
		t.Fatalf("unexpected Orvar discard trigger: %+v", tr)
	}
	cost := events.DiscardCost(orvar)
	if e.discardedMatches(tr, orvar, cost) {
		t.Fatal("Orvar matched a discard cost with no opposing spell or ability")
	}
	cause := e.G.AddObject(mustCorpusCard(t, reg, "Lightning Bolt"), 1)
	cause.Zone = state.ZStack
	e.G.SetZone(state.ZStack, 0, []state.ObjID{cause.ID})
	if e.discardedMatches(tr, orvar, cost) {
		t.Fatal("Orvar attributed a discard cost to an unrelated spell already on the stack")
	}
	if !e.discardedMatches(tr, orvar, events.Discard(orvar, 0)) {
		t.Fatal("Orvar did not match a discard caused by an opponent spell")
	}
}

func TestCommitCrimeTriggerForsakenMiner(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	miner := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forsaken Miner"))
	e.G.SetZone(state.ZBattlefield, 0, nil)
	e.G.Obj(miner).Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{miner})
	spell := e.G.AddObject(mustCorpusCard(t, reg, "Lightning Bolt"), 0)
	own := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	// A multi-target spell commits one crime, even if the criminal target is
	// appended after an innocent first target.
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, IDs: []state.ObjID{own}})
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, Player: 1, Amount: 3})
	requireOneEventTrigger(t, e, "Forsaken Miner")
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, Player: 1, Amount: 3})
	requireOneEventTrigger(t, e, "Forsaken Miner")
}

func TestTapsTriggerCityOfBrass(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	city := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "City of Brass"))
	e.emit(events.Event{Kind: events.Tap, Obj: city})
	requireOneEventTrigger(t, e, "City of Brass")
}

// An entry with Tapped$ True is a state of its zone change, not an event of
// becoming tapped. effects.ChangeZone records that distinction in the replayed
// Tap event so City of Brass cannot deal damage for entering tapped.
func TestTapsTriggerCityOfBrassDoesNotFireForEntryTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	city := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "City of Brass"))
	e.emit(events.Event{Kind: events.Tap, Obj: city, Text: "entered tapped"})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("entering tapped queued %d Taps triggers, want 0", len(e.pendingTriggers))
	}
}

func TestTapsForManaTriggerCryptGhastPaysForCastImmediately(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	spell := mustCorpusCard(t, reg, "Black Knight") // real {B}{B} corpus cost
	e := handEngine(t, spell)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Crypt Ghast"))
	swamp := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swamp"))
	spellID := e.G.Zone(state.ZHand, 0)[0]

	// Exercise the real CR 601.2g payment window: one Swamp's printed ability
	// supplies only {B}, so this cast succeeds only if Crypt Ghast's triggered
	// mana ability resolves immediately under CR 605.3b and supplies the second.
	e.pending = nil
	e.beginCast(0, decision.Option{Kind: "cast", Obj: spellID})
	e.Advance()
	submitChoices(t, e, activateOption(t, e, swamp))
	// The merged engine also expands Crypt Ghast's Extort keyword into a
	// SpellCast trigger (kw:Extort, this branch's primitive), which rides the
	// stack after the spell -- so the stack is [spell, extort trigger], not
	// the pre-merge [spell] alone. What the payment window pins is that the
	// spell itself is ON the stack and fully paid: the Ghast TapsForMana
	// trigger resolved immediately under CR 605.3b (pool empty, no pending
	// triggers) and supplied the second {B}.
	onStack := false
	for _, id := range e.G.Stack {
		if id == spellID {
			onStack = true
		}
	}
	if !onStack || e.G.Obj(spellID).Zone != state.ZStack {
		t.Fatalf("Black Knight stack/zone = %v/%s, want paid cast on stack", e.G.Stack, e.G.Obj(spellID).Zone)
	}
	if got := e.G.Players[0].Pool.Total(); got != 0 {
		t.Fatalf("pool after paying {B}{B} = %+v (total %d), want empty", e.G.Players[0].Pool, got)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("Crypt Ghast left %d pending triggers, want its mana trigger resolved without the stack", len(e.pendingTriggers))
	}

	// TapsForMana is an event mode, not itself a license to bypass the stack:
	// Manabarbs produces no mana and lacks Forge's triggered-mana marker.
	e = layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Manabarbs"))
	swamp = onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swamp"))
	e.resolveManaAbility(0, swamp, e.availableManaAbilities(0, swamp)[0], false)
	requireOneEventTrigger(t, e, "Manabarbs")
}

func TestTapsForManaTriggerForsakenMonumentRejectsWrongColourAndActor(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forsaken Monument"))
	swamp := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swamp"))
	e.resolveManaAbility(0, swamp, e.availableManaAbilities(0, swamp)[0], false)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("black mana queued %d colourless-only triggers", len(e.pendingTriggers))
	}
	// Crypt Ghast's Activator$ You must not observe another player's land.
	e = layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Crypt Ghast"))
	swamp = onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Swamp"))
	e.resolveManaAbility(1, swamp, e.availableManaAbilities(1, swamp)[0], false)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("opponent activation queued %d You-only triggers", len(e.pendingTriggers))
	}
}

func TestTapsForManaTriggerRegalBehemothChecksMonarch(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Regal Behemoth"))
	swamp := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Swamp"))
	e.resolveManaAbility(0, swamp, e.availableManaAbilities(0, swamp)[0], false)
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("non-monarch queued %d Regal Behemoth triggers", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.MonarchChange, Player: 0})
	e.emit(events.Event{Kind: events.Untap, Obj: swamp})
	before := e.G.Players[0].Pool
	e.resolveManaAbility(0, swamp, e.availableManaAbilities(0, swamp)[0], false)
	// Produced$ Combo Any is a colour the monarch chooses (CR 605.3b resolves
	// it at once, but not as colourless).
	d := e.Pending()
	if d == nil || d.Player != 0 || len(d.Options) != 5 {
		t.Fatalf("Regal Behemoth colour decision = %+v, want seat 0 choosing one of WUBRG", d)
	}
	submitChoices(t, e, manaOption(t, d, "G"))
	pool := e.G.Players[0].Pool
	if pool[state.MB]-before[state.MB] != 1 || pool[state.MG]-before[state.MG] != 1 || pool.Total()-before.Total() != 2 {
		t.Fatalf("monarch pool %+v (before %+v), want the Swamp's {B} plus Regal Behemoth's chosen {G}", pool, before)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("Regal Behemoth left %d pending triggers, want immediate mana resolution", len(e.pendingTriggers))
	}
}

func TestAttackersDeclaredOneTargetTriggerHorizonExplorer(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Horizon Explorer"))
	attacker := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{attacker}})
	requireOneEventTrigger(t, e, "Horizon Explorer")
}

func TestAttackersDeclaredOneTargetKarazikarCarriesBothPlayers(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deck := mountainDeck(t, 40)
	e := New(Config{Seed: 1, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck, deck, deck}})
	karazikar := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Karazikar, the Eye Tyrant"))
	attacker := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	hands := [2]int{len(e.G.Zone(state.ZHand, 0)), len(e.G.Zone(state.ZHand, 1))}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 2, IDs: []state.ObjID{attacker}})
	requireOneEventTrigger(t, e, "Karazikar")
	e.putTriggersOnStack()
	e.resolveTop()
	for _, p := range []state.PlayerID{0, 1} {
		if got := e.G.Players[p].Life; got != 19 {
			t.Fatalf("seat %d life = %d, want 19", p, got)
		}
		if got, want := len(e.G.Zone(state.ZHand, p)), hands[p]; got != want+1 {
			t.Fatalf("seat %d hand = %d, want %d", p, got, want+1)
		}
	}

	// The other real trigger targets a creature controlled by the attacked
	// player; a seat-1 creature must never appear in this seat-2-only offer.
	e = New(Config{Seed: 1, Names: []string{"a", "b", "c"}, Decks: [][]*cards.Card{deck, deck, deck}})
	karazikar = onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Karazikar, the Eye Tyrant"))
	attacker = onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	wrong := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	right := onBoardCard(t, e, 2, mustCorpusCard(t, reg, "Grizzly Bears"))
	_ = karazikar
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 2, IDs: []state.ObjID{attacker}})
	e.putTriggersOnStack()
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != right {
		t.Fatalf("Karazikar target options = %+v, want only attacked player's %d (not %d)", d, right, wrong)
	}
}

// castOnStack puts a real corpus spell on the stack for seat p aimed at
// target player tp, the fixture resolveTop needs to drive a resolving effect.
func castOnStack(t *testing.T, e *Engine, reg *cards.Registry, name string, p, tp state.PlayerID) state.ObjID {
	t.Helper()
	o := e.G.AddObject(mustCorpusCard(t, reg, name), p)
	o.Zone = state.ZStack
	o.Targets = []state.Target{{Player: tp, IsPlayer: true}}
	e.G.SetZone(state.ZStack, 0, append(e.G.Zone(state.ZStack, 0), o.ID))
	return o.ID
}

// TestSacrificedTriggerSurvivesRestInPeaceRedirect: Diabolic Edict's real
// Sacrifice effect under Rest in Peace. The creature is exiled instead of put
// into the graveyard, but it was still sacrificed (CR 701.21a, CR 614.6), so
// Mayhem Devil triggers. An ordinary death Rest in Peace redirects the same
// way is not a sacrifice and must not.
func TestSacrificedTriggerSurvivesRestInPeaceRedirect(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	devil := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Mayhem Devil"))
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rest in Peace"))
	dies := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))

	e.emit(events.Event{Kind: events.MoveZone, Obj: dies, From: state.ZBattlefield, To: state.ZGraveyard})
	if o := e.G.Obj(dies); o == nil || o.Zone != state.ZExile {
		t.Fatalf("Rest in Peace did not exile the dying creature: %+v", o)
	}
	if got := observedTriggerCount(e, devil); got != 0 {
		t.Fatalf("Mayhem Devil triggers after an ordinary redirected death = %d, want 0", got)
	}

	victim := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	castOnStack(t, e, reg, "Diabolic Edict", 0, 1)
	e.resolveTop()
	if o := e.G.Obj(victim); o == nil || o.Zone != state.ZExile {
		t.Fatalf("Diabolic Edict under Rest in Peace left the victim in %v, want exile", o.Zone)
	}
	if got := observedTriggerCount(e, devil); got != 1 {
		t.Fatalf("Mayhem Devil triggers after a sacrifice redirected to exile = %d, want 1", got)
	}
	found := false
	for _, ev := range e.L.Events {
		if ev.Obj == victim && events.IsSacrifice(ev) && ev.To == state.ZExile {
			found = true
		}
	}
	if !found {
		t.Fatal("the replacement's exile move lost the sacrifice marker")
	}
}

// TestDiscardedTriggerSurvivesDestinationReplacements drives Mind Peel's real
// discard under two real destination replacements: Rest in Peace (exile
// instead) and Library of Leng (top of library instead, gated on Discard$
// True and EffectOnly$ True). Either way the card was discarded (CR 701.9a),
// so Necropotence triggers. Leng must not apply to the CR 514.1 cleanup
// discard, which no effect causes.
func TestDiscardedTriggerSurvivesDestinationReplacements(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		replacement string
		to          state.Zone
	}{{"Rest in Peace", state.ZExile}, {"Library of Leng", state.ZLibrary}} {
		t.Run(tc.replacement, func(t *testing.T) {
			e := layerEngine(t)
			necro := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Necropotence"))
			onBoardCard(t, e, 0, mustCorpusCard(t, reg, tc.replacement))
			castOnStack(t, e, reg, "Mind Peel", 1, 0)
			e.resolveTop()
			d := e.Pending()
			if d == nil || d.ResumeKind != "discard" || len(d.Options) == 0 {
				t.Fatalf("Mind Peel discard decision = %+v, want a real discard ask", d)
			}
			discarded := d.Options[0].Obj
			submitChoices(t, e, d.Options[0].Index)
			if o := e.G.Obj(discarded); o == nil || o.Zone != tc.to {
				t.Fatalf("discarded card zone = %v, want %v", o.Zone, tc.to)
			}
			if got := observedTriggerCount(e, necro); got != 1 {
				t.Fatalf("Necropotence triggers after a redirected discard = %d, want 1", got)
			}
		})
	}

	t.Run("Library of Leng ignores cleanup discard", func(t *testing.T) {
		e := layerEngine(t)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Library of Leng"))
		necro := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Necropotence"))
		e.G.Active = 0
		e.G.Step = state.StepCleanup
		// Leng grants no maximum hand size, so cleanup would ask nothing;
		// Leng's own discard gate is what this pins, driven by the emit.
		extra := onHand(t, e, 0, "Name:Cleanup Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n")
		e.emit(events.Discard(extra, 0))
		if o := e.G.Obj(extra); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("cleanup discard under Library of Leng went to %v, want graveyard", o.Zone)
		}
		if got := observedTriggerCount(e, necro); got != 1 {
			t.Fatalf("Necropotence triggers after cleanup discard = %d, want 1", got)
		}
	})
}

// TestDiscardReplacementObstinateBalothGates drives Obstinate Baloth's real
// Discard$ True | EffectOnly$ True | ValidCause$ SpellAbility.OppCtrl
// replacement. Only an opponent's discard effect puts it onto the battlefield;
// the player's own discard effect and the CR 514.1 cleanup discard leave it in
// the graveyard. When it does redirect, the card was still discarded (CR
// 701.9a), so Necropotence triggers either way.
func TestDiscardReplacementObstinateBalothGates(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	setup := func(t *testing.T) (*Engine, state.ObjID, state.ObjID) {
		e := layerEngine(t)
		necro := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Necropotence"))
		o := e.G.AddObject(mustCorpusCard(t, reg, "Obstinate Baloth"), 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
		return e, necro, o.ID
	}
	peel := func(t *testing.T, e *Engine, caster state.PlayerID, baloth state.ObjID) {
		castOnStack(t, e, reg, "Mind Peel", caster, 0)
		e.resolveTop()
		d := e.Pending()
		if d == nil || d.ResumeKind != "discard" {
			t.Fatalf("Mind Peel discard decision = %+v, want a real discard ask", d)
		}
		for _, opt := range d.Options {
			if opt.Obj == baloth {
				submitChoices(t, e, opt.Index)
				return
			}
		}
		t.Fatalf("Obstinate Baloth is not a discard option: %+v", d.Options)
	}
	for _, tc := range []struct {
		name   string
		caster state.PlayerID
		want   state.Zone
	}{{"opponent's effect", 1, state.ZBattlefield}, {"own effect", 0, state.ZGraveyard}} {
		t.Run(tc.name, func(t *testing.T) {
			e, necro, baloth := setup(t)
			peel(t, e, tc.caster, baloth)
			if o := e.G.Obj(baloth); o == nil || o.Zone != tc.want {
				t.Fatalf("Obstinate Baloth zone = %v, want %v", o.Zone, tc.want)
			}
			if got := observedTriggerCount(e, necro); got != 1 {
				t.Fatalf("Necropotence triggers = %d, want 1", got)
			}
		})
	}
	t.Run("cleanup discard", func(t *testing.T) {
		e, _, baloth := setup(t)
		e.emit(events.Discard(baloth, 0))
		if o := e.G.Obj(baloth); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("cleanup-discarded Obstinate Baloth zone = %v, want graveyard", o.Zone)
		}
	})
}

// castOnStackAt puts a real corpus spell on the stack for seat p with one
// chosen target, the fixture resolveTop needs to drive a targeted effect.
func castOnStackAt(t *testing.T, e *Engine, reg *cards.Registry, name string, p state.PlayerID, target state.Target) state.ObjID {
	t.Helper()
	o := e.G.AddObject(mustCorpusCard(t, reg, name), p)
	o.Zone = state.ZStack
	o.Targets = []state.Target{target}
	e.G.SetZone(state.ZStack, 0, append(e.G.Zone(state.ZStack, 0), o.ID))
	return o.ID
}

// enterFromHand puts a real corpus card into seat p's hand and moves it onto
// the battlefield through emit, so replacement effects apply to the entry.
func enterFromHand(t *testing.T, e *Engine, reg *cards.Registry, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	o := e.G.AddObject(mustCorpusCard(t, reg, name), p)
	o.Zone = state.ZHand
	e.G.SetZone(state.ZHand, p, append(e.G.Zone(state.ZHand, p), o.ID))
	e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: state.ZHand, To: state.ZBattlefield})
	return o.ID
}

// TestTapsTriggerIgnoresEnterTappedReplacement drives the real "enters
// tapped" replacement bodies (DB$ Tap | ETB$ True). A permanent that enters
// tapped never becomes tapped (CR 603.2e; Forge TapEffect's ETB branch runs
// no Taps trigger), so City of Brass entering under Frozen Aether deals no
// damage and Rhoda/Verity Circle do not trigger for a creature entering under
// Authority of the Consuls. The same creature becoming tapped afterwards
// does trigger both, so the matcher itself is live.
func TestTapsTriggerIgnoresEnterTappedReplacement(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	t.Run("City of Brass under Frozen Aether", func(t *testing.T) {
		e := layerEngine(t)
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Frozen Aether"))
		city := enterFromHand(t, e, reg, 1, "City of Brass")
		if o := e.G.Obj(city); o.Zone != state.ZBattlefield || !o.Tapped {
			t.Fatalf("City of Brass zone/tapped = %v/%v, want battlefield tapped", o.Zone, o.Tapped)
		}
		if got := observedTriggerCount(e, city); got != 0 {
			t.Fatalf("City of Brass triggers for entering tapped = %d, want 0", got)
		}
	})
	t.Run("Baloth Prime's own enters-tapped replacement", func(t *testing.T) {
		e := layerEngine(t)
		rhoda := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rhoda, Geist Avenger"))
		baloth := enterFromHand(t, e, reg, 1, "Baloth Prime")
		if o := e.G.Obj(baloth); !o.Tapped || o.Counter("STUN") != 6 {
			t.Fatalf("Baloth Prime tapped/stun = %v/%d, want tapped with six stun counters", o.Tapped, o.Counter("STUN"))
		}
		if got := observedTriggerCount(e, rhoda); got != 0 {
			t.Fatalf("Rhoda triggers for Baloth Prime entering tapped = %d, want 0", got)
		}
	})
	t.Run("Rhoda and Verity Circle under Authority of the Consuls", func(t *testing.T) {
		e := layerEngine(t)
		rhoda := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rhoda, Geist Avenger"))
		circle := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Verity Circle"))
		onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Authority of the Consuls"))
		bears := enterFromHand(t, e, reg, 1, "Grizzly Bears")
		if !e.G.Obj(bears).Tapped {
			t.Fatal("Authority of the Consuls did not tap the entering creature")
		}
		if r, c := observedTriggerCount(e, rhoda), observedTriggerCount(e, circle); r != 0 || c != 0 {
			t.Fatalf("Rhoda/Verity Circle triggers for entering tapped = %d/%d, want 0/0", r, c)
		}
		e.emit(events.Event{Kind: events.Untap, Obj: bears})
		castOnStackAt(t, e, reg, "Pressure Point", 0, state.Target{Obj: bears})
		e.resolveTop()
		if r, c := observedTriggerCount(e, rhoda), observedTriggerCount(e, circle); r != 1 || c != 1 {
			t.Fatalf("Rhoda/Verity Circle triggers when the creature becomes tapped = %d/%d, want 1/1", r, c)
		}
	})
}

// TestTapsTriggerValidPlayerIsTheTapper: a Taps trigger's ValidPlayer$ is the
// player who tapped the permanent (Forge Card.tap's tapper), not its
// controller. Pressure Point's real Tap effect, cast by seat 0 at seat 1's
// creature, is "you tap an untapped creature an opponent controls" for
// Icewrought Sentry, Solitary Sanctuary and Sharae of Numbing Depths; seat 1
// tapping its own creature is not. Sharae's ActivationLimit$ 1 holds it to
// one trigger a turn.
func TestTapsTriggerValidPlayerIsTheTapper(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	e.G.Turn = 1
	sentry := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Icewrought Sentry"))
	sanctuary := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Solitary Sanctuary"))
	sharae := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Sharae of Numbing Depths"))
	theirs := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	other := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	tap := func(caster state.PlayerID, target state.ObjID) {
		t.Helper()
		e.emit(events.Event{Kind: events.Untap, Obj: target})
		castOnStackAt(t, e, reg, "Pressure Point", caster, state.Target{Obj: target})
		e.resolveTop()
		if !e.G.Obj(target).Tapped {
			t.Fatalf("Pressure Point did not tap %d", target)
		}
	}
	counts := func() [3]int {
		return [3]int{observedTriggerCount(e, sentry), observedTriggerCount(e, sanctuary), observedTriggerCount(e, sharae)}
	}

	tap(1, theirs)
	if got := counts(); got != [3]int{0, 0, 0} {
		t.Fatalf("an opponent tapping its own creature: Sentry/Sanctuary/Sharae = %v, want [0 0 0]", got)
	}
	tap(0, theirs)
	if got := counts(); got != [3]int{1, 1, 1} {
		t.Fatalf("seat 0 tapping an opponent's creature: Sentry/Sanctuary/Sharae = %v, want [1 1 1]", got)
	}
	tap(0, other)
	if got := counts(); got != [3]int{2, 2, 1} {
		t.Fatalf("a second tap the same turn: Sentry/Sanctuary/Sharae = %v, want [2 2 1] (Sharae triggers only once each turn)", got)
	}
	e.G.Turn++
	tap(0, theirs)
	if got := counts(); got != [3]int{3, 3, 2} {
		t.Fatalf("a tap on the next turn: Sentry/Sanctuary/Sharae = %v, want [3 3 2]", got)
	}
}

// TestTapsTriggerFirstTimeDuringYourTurnCaptainAmerica drives Captain
// America's real FirstTime$ True | PlayerTurn$ True line: only the first time
// each of your creatures becomes tapped in a turn, and only during your turn.
func TestTapsTriggerFirstTimeDuringYourTurnCaptainAmerica(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	e.G.Turn, e.G.Active = 1, 0
	captain := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Captain America, Living Legend"))
	bears := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Grizzly Bears"))
	tap := func() {
		t.Helper()
		e.emit(events.Event{Kind: events.Untap, Obj: bears})
		castOnStackAt(t, e, reg, "Pressure Point", 0, state.Target{Obj: bears})
		e.resolveTop()
	}
	tap()
	if got := observedTriggerCount(e, captain); got != 1 {
		t.Fatalf("first tap this turn: Captain America triggers = %d, want 1", got)
	}
	tap()
	if got := observedTriggerCount(e, captain); got != 1 {
		t.Fatalf("second tap this turn: Captain America triggers = %d, want still 1", got)
	}
	e.G.Turn = 2
	tap()
	if got := observedTriggerCount(e, captain); got != 2 {
		t.Fatalf("first tap of the next turn: Captain America triggers = %d, want 2", got)
	}
	e.G.Turn, e.G.Active = 3, 1
	tap()
	if got := observedTriggerCount(e, captain); got != 2 {
		t.Fatalf("a tap during the opponent's turn: Captain America triggers = %d, want still 2", got)
	}
}

// TestTapsForManaProducedMatchesContainedColourless: Forsaken Monument's
// Produced$ C fires when the tapped-for mana CONTAINS colourless (Forge's
// contains test), so Coral Atoll's real "Produced$ C U" gets the extra {C}.
func TestTapsForManaProducedMatchesContainedColourless(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forsaken Monument"))
	atoll := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Coral Atoll"))
	e.resolveManaAbility(0, atoll, e.availableManaAbilities(0, atoll)[0], false)
	if pool := e.G.Players[0].Pool; pool[state.MC] != 2 || pool[state.MU] != 1 || pool.Total() != 3 {
		t.Fatalf("pool after Coral Atoll under Forsaken Monument = %+v, want {C}{C}{U}", pool)
	}
}

// TestTriggeredManaHonoursRecipientAndColourChoice drives the CR 605.3b
// triggered-mana path with real scripts. Defined$ TriggeredCardController
// gives the mana to the tapped land's controller (Vernal Bloom, Gauntlet of
// Might are symmetric), and Produced$ Any asks the player receiving it for a
// colour (Fertile Ground on an opponent's land) -- including inside a spell's
// payment window, which resumes once the colour is chosen.
func TestTriggeredManaHonoursRecipientAndColourChoice(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	for _, tc := range []struct {
		trigger, land string
		slot          int
	}{{"Vernal Bloom", "Forest", state.MG}, {"Gauntlet of Might", "Mountain", state.MR}} {
		t.Run(tc.trigger, func(t *testing.T) {
			e := handEngine(t)
			onBoardCard(t, e, 0, mustCorpusCard(t, reg, tc.trigger))
			land := onBoardCard(t, e, 1, mustCorpusCard(t, reg, tc.land))
			e.resolveManaAbility(1, land, e.availableManaAbilities(1, land)[0], false)
			if got := e.G.Players[1].Pool[tc.slot]; got != 2 {
				t.Fatalf("%s's controller pool = %+v, want the land's mana plus %s's", tc.land, e.G.Players[1].Pool, tc.trigger)
			}
			if got := e.G.Players[0].Pool.Total(); got != 0 {
				t.Fatalf("%s's controller received %d mana for an opponent's %s", tc.trigger, got, tc.land)
			}
		})
	}
	t.Run("Fertile Ground on an opponent's land", func(t *testing.T) {
		e := handEngine(t)
		fertile := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fertile Ground"))
		forest := onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Forest"))
		e.G.Obj(fertile).AttachedTo = forest
		e.resolveManaAbility(1, forest, e.availableManaAbilities(1, forest)[0], false)
		d := e.Pending()
		if d == nil || d.Player != 1 || len(d.Options) != 5 {
			t.Fatalf("Fertile Ground colour decision = %+v, want the land's controller (seat 1) choosing WUBRG", d)
		}
		submitChoices(t, e, manaOption(t, d, "U"))
		if pool := e.G.Players[1].Pool; pool[state.MG] != 1 || pool[state.MU] != 1 || pool.Total() != 2 {
			t.Fatalf("seat 1 pool = %+v, want the Forest's {G} plus the chosen {U}", pool)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("Fertile Ground's controller received %d mana", got)
		}
	})
	t.Run("colour choice inside a payment window", func(t *testing.T) {
		spell := mustCorpusCard(t, reg, "Growth Spiral") // real {G}{U} cost
		e := handEngine(t, spell)
		spellID := e.G.Zone(state.ZHand, 0)[0]
		fertile := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Fertile Ground"))
		forest := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forest"))
		e.G.Obj(fertile).AttachedTo = forest
		e.pending = nil
		e.beginCast(0, decision.Option{Kind: "cast", Obj: spellID})
		e.Advance()
		submitChoices(t, e, activateOption(t, e, forest))
		d := e.Pending()
		if d == nil || d.Player != 0 || len(d.Options) != 5 || d.Options[0].Kind != "mana" {
			t.Fatalf("decision after tapping the Forest = %+v, want Fertile Ground's colour choice", d)
		}
		submitChoices(t, e, manaOption(t, d, "U"))
		if len(e.G.Stack) != 1 || e.G.Stack[0] != spellID {
			t.Fatalf("stack = %v, want Growth Spiral paid with {G} and Fertile Ground's {U}", e.G.Stack)
		}
		if got := e.G.Players[0].Pool.Total(); got != 0 {
			t.Fatalf("pool after paying {G}{U} = %+v, want empty", e.G.Players[0].Pool)
		}
	})
}

// TestDiscardedSelfTriggersFireFromHand: "when you discard this card"
// declares no TriggerZones$, and the card is in its owner's hand when it is
// discarded. Orvar's ValidCause$ SpellAbility.OppCtrl admits only an
// opponent's effect; Bartered Cow and Titanbones trigger for any discard,
// including Lion's Eye Diamond's discard-your-hand cost.
func TestDiscardedSelfTriggersFireFromHand(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	names := []string{"Orvar, the All-Form", "Bartered Cow", "Titanbones, Towering Heart"}
	inHand := func(t *testing.T, e *Engine, name string) state.ObjID {
		o := e.G.AddObject(mustCorpusCard(t, reg, name), 0)
		o.Zone = state.ZHand
		e.G.SetZone(state.ZHand, 0, append(e.G.Zone(state.ZHand, 0), o.ID))
		return o.ID
	}
	for _, tc := range []struct {
		name   string
		caster state.PlayerID
		want   []int
	}{{"opponent's Mind Peel", 1, []int{1, 1, 1}}, {"own Mind Peel", 0, []int{0, 1, 1}}} {
		for i, card := range names {
			t.Run(tc.name+"/"+card, func(t *testing.T) {
				e := layerEngine(t)
				id := inHand(t, e, card)
				castOnStack(t, e, reg, "Mind Peel", tc.caster, 0)
				e.resolveTop()
				d := e.Pending()
				if d == nil || d.ResumeKind != "discard" {
					t.Fatalf("Mind Peel discard decision = %+v, want a real discard ask", d)
				}
				chosen := -1
				for _, opt := range d.Options {
					if opt.Obj == id {
						chosen = opt.Index
					}
				}
				if chosen < 0 {
					t.Fatalf("%s is not a discard option: %+v", card, d.Options)
				}
				submitChoices(t, e, chosen)
				if e.G.Obj(id).Zone != state.ZGraveyard {
					t.Fatalf("%s zone = %v, want discarded to the graveyard", card, e.G.Obj(id).Zone)
				}
				if got := observedTriggerCount(e, id); got != tc.want[i] {
					t.Fatalf("%s triggers = %d, want %d", card, got, tc.want[i])
				}
			})
		}
	}
	t.Run("Lion's Eye Diamond cost", func(t *testing.T) {
		e := layerEngine(t)
		ids := make([]state.ObjID, len(names))
		for i, card := range names {
			ids[i] = inHand(t, e, card)
		}
		led := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Lion's Eye Diamond"))
		e.resolveManaAbility(0, led, e.availableManaAbilities(0, led)[0], false)
		for i, id := range ids {
			if e.G.Obj(id).Zone != state.ZGraveyard {
				t.Fatalf("%s zone = %v, want discarded to the graveyard", names[i], e.G.Obj(id).Zone)
			}
			if got, want := observedTriggerCount(e, id), []int{0, 1, 1}[i]; got != want {
				t.Fatalf("%s triggers for a discard cost = %d, want %d", names[i], got, want)
			}
		}
	})
}

// TestCommitCrimeTargetingExileIsNotACrime pins CR 700.13's list: a card in an
// opponent's graveyard is a crime target (judged by owner, CR 108.4a); an
// opponent-owned card in exile is not.
func TestCommitCrimeTargetingExileIsNotACrime(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	miner := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Forsaken Miner"))
	e.G.SetZone(state.ZBattlefield, 0, nil)
	e.G.Obj(miner).Zone = state.ZGraveyard
	e.G.SetZone(state.ZGraveyard, 0, []state.ObjID{miner})
	spell := e.G.AddObject(mustCorpusCard(t, reg, "Lightning Bolt"), 0)
	card := func(z state.Zone) state.ObjID {
		o := e.G.AddObject(mustCorpusCard(t, reg, "Grizzly Bears"), 1)
		o.Zone = z
		e.G.SetZone(z, 1, append(e.G.Zone(z, 1), o.ID))
		return o.ID
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, IDs: []state.ObjID{card(state.ZExile)}})
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("targeting an opponent's exiled card queued %d crime triggers, want 0", len(e.pendingTriggers))
	}
	e.emit(events.Event{Kind: events.TargetsChosen, Obj: spell.ID, IDs: []state.ObjID{card(state.ZGraveyard)}})
	requireOneEventTrigger(t, e, "Forsaken Miner")
}

// TestUnsupportedSelectorsKeepOtherModesFiring pins the supported initiative
// intervening-if on Rasaad yn Bashir's Attacks trigger: it only fires when
// its controller has the designation. Mutiny's
// TargetsWithDefinedController$ ParentTargetedController remains unsupported
// and leaves the target offer as it was rather than emptying it.
func TestUnsupportedSelectorsKeepOtherModesFiring(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := layerEngine(t)
	rasaad := onBoardCard(t, e, 0, mustCorpusCard(t, reg, "Rasaad yn Bashir"))
	if o := e.G.Obj(rasaad); o == nil || o.Zone != state.ZBattlefield || o.Controller != 0 {
		t.Fatalf("precondition: Rasaad must be controlled by seat 0 on the battlefield: %+v", o)
	}
	if e.G.HasInitiative {
		t.Fatal("precondition: the game already has an initiative holder")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{rasaad}})
	if got := observedTriggerCount(e, rasaad); got != 0 {
		t.Fatalf("Rasaad without initiative queued %d triggers, want 0", got)
	}
	e.emit(events.Event{Kind: events.InitiativeChange, Player: 0})
	if !e.G.IsInitiative(0) {
		t.Fatal("precondition: seat 0 did not take the initiative")
	}
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 0, IDs: []state.ObjID{rasaad}})
	requireOneEventTrigger(t, e, "Rasaad yn Bashir")

	e = layerEngine(t)
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	onBoardCard(t, e, 1, mustCorpusCard(t, reg, "Grizzly Bears"))
	mutiny := castOnStackAt(t, e, reg, "Mutiny", 0, state.Target{})
	sub := e.G.Obj(mutiny).Face().Abilities[0].Sub
	if sub == nil || sub.Params["TargetsWithDefinedController"] != "ParentTargetedController" {
		t.Fatalf("unexpected Mutiny sub-ability: %+v", sub)
	}
	if got := len(e.legalTargetCandidates(0, mutiny, mutiny, sub)); got != 2 {
		t.Fatalf("Mutiny's second-target offer = %d candidates, want both creatures", got)
	}
}
