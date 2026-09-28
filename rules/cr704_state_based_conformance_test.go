package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 704.3: lines 10441-10449 (priority, fixed point, simultaneous event).
// CR 704.4: lines 10450-10458 (no SBA during resolution).
// CR 704.5d/h/j/m/q: lines 10470, 10482-10484, 10488-10490,
// 10497-10498, 10508-10510. CR 702.16c: lines 7931-7933.
// These are engine-boundary probes with compiled cards, never fabricated SAs.
// Every leaf here passes with the conformance flag on, so all run in the
// ordinary lane.

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestCR704NoLifeSBAInsideSmallpoxDiscard(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Smallpox"}, nil, nil)
	id := crAbortMove(t, e, 0, "Smallpox", state.ZHand)
	sa := e.G.Obj(id).Face().SpellAbility()
	if sa == nil || sa.API != "LoseLife" || sa.Params["LifeAmount"] != "1" || sa.Sub == nil || sa.Sub.API != "Discard" || sa.Sub.Params["Mode"] != "TgtChoose" {
		t.Fatal("CR 704.4 Smallpox seq 0: real life-loss/discard chain changed")
	}
	// The first discard chooser stays alive. Seat 1 must still participate
	// at zero life until this ENTIRE spell, including sacrifices, finishes.
	// This avoids I-1's departed-chooser continuation entirely.
	e.emit(events.Event{Kind: events.LifeChange, Player: 1, Amount: 1 - e.G.Players[1].Life})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "B", Amount: 2})
	e.askPriority(0)
	crAbortAnswer(t, e, "Smallpox", crAbortOption(t, e, "Smallpox", "cast", id))
	start := len(e.L.Events)
	crResolutionRound(t, e)
	d := e.Pending()
	if d == nil || d.Kind != decision.KModes || d.Player != 0 || d.ResumeKind != "discard" || e.G.Obj(id).Zone != state.ZStack || e.G.Players[1].Life != 0 {
		t.Fatalf("CR 704.4 Smallpox seq %d: expected in-resolution discard at life zero; pending=%+v zone=%s life=%d", start, d, e.G.Obj(id).Zone, e.G.Players[1].Life)
	}
	if e.G.Players[1].Lost {
		t.Errorf("CR 704.3/704.4/704.5a Smallpox seq %d: seat 1 eliminated during seat 0's discard ask, before spell completion; life zero must wait for the next priority boundary", start)
	}
	if len(d.Options) == 0 {
		t.Fatalf("CR 704.4 Smallpox seq %d: discard decision has no options", d.Seq)
	}
	crAbortAnswer(t, e, "Smallpox", d.Options[0].Index)
	// Smallpox discards from EACH player. The first answer now suspends at
	// seat 1's own hand rather than finishing the spell; SBAs must remain
	// deferred through this second, independent choice as well.
	d = e.Pending()
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "discard" || d.Player != 1 || e.G.Obj(id).Zone != state.ZStack || e.G.Players[1].Lost {
		t.Fatalf("CR 704.4 Smallpox seq %d: expected seat 1's in-resolution discard while still alive; pending=%+v lost=%v", len(e.L.Events), d, e.G.Players[1].Lost)
	}
	if len(d.Options) == 0 {
		t.Fatal("CR 704.4 Smallpox: seat 1 discard has no options")
	}
	// The fixture has three seats; answer any later discard/sacrifice asks
	// until the entire spell completes. An SBA at any intermediate ask would
	// eliminate seat 1 too early, whereas checking immediately after seat 1's
	// discard would incorrectly demand an SBA inside resolution.
	for i := 0; i < 12 && e.G.Obj(id).Zone == state.ZStack; i++ {
		if e.G.Players[1].Lost {
			t.Fatalf("CR 704.4 Smallpox seq %d: seat 1 eliminated during resolution", len(e.L.Events))
		}
		if d == nil || d.Kind == decision.KPriority || len(d.Options) == 0 {
			t.Fatalf("CR 704.4 Smallpox seq %d: cannot finish suspended resolution; pending=%+v", len(e.L.Events), d)
		}
		crAbortAnswer(t, e, "Smallpox", d.Options[0].Index)
		d = e.Pending()
	}
	if e.G.Obj(id).Zone == state.ZStack {
		t.Fatal("CR 704.4 Smallpox: spell never completed after bounded answers")
	}
	if !e.G.Players[1].Lost {
		t.Errorf("CR 704.3/704.5a Smallpox seq %d: seat 1 survived at zero life after spell completion; deferred SBA must run before priority", len(e.L.Events))
	}
}

func TestCR704SimultaneousDeathsPreserveBloodArtistWitness(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	e := crResolutionEngine(t, []string{"Blood Artist"}, nil)
	artist := crAbortMove(t, e, 0, "Blood Artist", state.ZBattlefield)
	delver := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
	triggers := e.G.Obj(artist).Face().Triggers
	if len(triggers) != 1 || triggers[0].Mode != "ChangesZone" || triggers[0].Params["ValidCard"] != "Card.Self,Creature.Other" || triggers[0].Params["Origin"] != "Battlefield" || triggers[0].Params["Destination"] != "Graveyard" {
		t.Fatal("CR 704.3 Blood Artist seq 0: death-witness fixture changed")
	}
	// Both creatures already have lethal damage at the SAME check. Merely
	// emitting two MoveZone records is not the oracle: the observable witness
	// set must be as if both died simultaneously, irrespective of log encoding.
	e.emit(events.Event{Kind: events.Damage, Obj: artist, Amount: 1})
	e.emit(events.Event{Kind: events.Damage, Obj: delver, Amount: 1})
	start, before := len(e.L.Events), len(e.pendingTriggers)
	e.checkStateBased()
	if e.G.Obj(artist).Zone != state.ZGraveyard || e.G.Obj(delver).Zone != state.ZGraveyard {
		t.Fatalf("CR 704.3 Blood Artist/Delver of Secrets seq %d: both lethal creatures must die", start)
	}
	witnessed := 0
	for _, tr := range e.pendingTriggers[before:] {
		if tr.Source == artist {
			witnessed++
		}
	}
	if witnessed != 2 {
		t.Errorf("CR 704.3 Blood Artist/Delver of Secrets seq %d: queued %d Blood Artist triggers; want 2 for the simultaneous deaths", start, witnessed)
	}
}

// Narrow corpus invariant: single-face legendary creatures with positive
// literal toughness and no printed rules other than the legend supertype.
// No fixture can change name/control or exempt itself from the legend rule.
// The assertion allows a real choice to suspend the boundary: it forbids
// priority with both legends, but does NOT certify a future chooser's options.
func TestCR704CorpusLegendDuplicatesCannotReachPriority(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, c := range reg.Cards {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		_, def, ok := strings.Cut(f.PT, "/")
		toughness, err := strconv.Atoi(def)
		if !ok || err != nil || toughness <= 0 || !f.IsCreature() || !slices.Contains(f.Types, "Legendary") || len(f.Abilities)+len(f.Triggers)+len(f.Statics)+len(f.Repls)+len(f.Keywords) != 0 {
			continue
		}
		checked++ // EXAMINED fixtures, independent of failure.
		e := crAbortEngine(t, reg, "ur-delver", f.Name, f.Name)
		a := crAbortMove(t, e, 0, f.Name, state.ZBattlefield)
		b := crAbortMove(t, e, 0, f.Name, state.ZBattlefield)
		if a == b {
			t.Fatalf("CR 704.5j %s seq %d: fixture did not create distinct objects", f.Name, len(e.L.Events))
		}
		e.pending = nil
		e.Advance()
		d := e.Pending()
		if d == nil || (d.Kind == decision.KPriority && e.G.Obj(a).Zone == state.ZBattlefield && e.G.Obj(b).Zone == state.ZBattlefield) {
			t.Errorf("CR 704.3/704.5j %s seq %d: duplicate legends survived to priority without a keep choice; zones=%s/%s", f.Name, len(e.L.Events), e.G.Obj(a).Zone, e.G.Obj(b).Zone)
		}
	}
	if checked == 0 {
		t.Fatal("CR 704.5j corpus seq 0: no vanilla legendary creature pairs examined")
	}
	t.Logf("MEASURED CR 704.5j examined=%d compiled vanilla legendary creature pairs", checked)
}

func TestCR704RepoCreatureOppositeCountersAnnihilate(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	seen := map[string]bool{}
	checked := 0
	for _, deck := range testutil.LegacyDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, deck) {
			f := c.Faces[0]
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			_, def, ok := strings.Cut(f.PT, "/")
			n, err := strconv.Atoi(def)
			// Leave counter-trigger/replacement interactions to a separate
			// oracle. Zero-base creatures need an entry-counter fixture too.
			if !ok || err != nil || n <= 0 || !f.IsCreature() || len(f.Triggers)+len(f.Repls) != 0 {
				continue
			}
			checked++
			e := crAbortEngine(t, reg, "ur-delver", f.Name)
			id := crAbortMove(t, e, 0, f.Name, state.ZBattlefield)
			e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "P1P1", Amount: 3})
			e.emit(events.Event{Kind: events.CounterChange, Obj: id, Counter: "M1M1", Amount: 2})
			start := len(e.L.Events)
			e.checkStateBased()
			o := e.G.Obj(id)
			if o.Zone != state.ZBattlefield || o.Counter("P1P1") != 1 || o.Counter("M1M1") != 0 {
				t.Errorf("CR 704.5q %s seq %d: zone=%s counters=(%d,%d); want battlefield and (1,0), not merely net +1/+1", f.Name, start, o.Zone, o.Counter("P1P1"), o.Counter("M1M1"))
			}
		}
	}
	if checked == 0 {
		t.Fatal("CR 704.5q repo decks seq 0: no positive-toughness creature without triggers/replacements examined")
	}
	t.Logf("MEASURED CR 704.5q examined=%d distinct repo-deck creatures", checked)
}

func TestCR704DepartedTokenIsNotAnExiledObject(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Raise the Alarm", "Unsummon"}, nil)
	raise := crAbortMove(t, e, 0, "Raise the Alarm", state.ZHand)
	f := e.G.Obj(raise).Face()
	sa := f.SpellAbility()
	if sa == nil || sa.API != "Token" || sa.Params["TokenScript"] != "w_1_1_soldier" || sa.Params["TokenAmount"] != "2" {
		t.Fatal("CR 704.5d Raise the Alarm seq 0: token fixture changed")
	}
	e.resolveAbility(raise, 0, nil, sa, f.SVars)
	var tokens []state.ObjID
	for _, id := range e.G.Zone(state.ZBattlefield, 0) {
		if e.G.Obj(id).IsToken {
			tokens = append(tokens, id)
		}
	}
	if len(tokens) != 2 {
		t.Fatalf("CR 704.5d Raise the Alarm seq %d: expected 2 real tokens, got %v", len(e.L.Events), tokens)
	}
	bounce := crAbortMove(t, e, 0, "Unsummon", state.ZHand)
	bf := e.G.Obj(bounce).Face()
	e.resolveAbility(bounce, 0, []state.Target{{Obj: tokens[0]}}, bf.SpellAbility(), bf.SVars)
	if e.G.Obj(tokens[0]).Zone != state.ZHand {
		t.Fatalf("CR 704.5d Unsummon seq %d: token did not leave battlefield for hand", len(e.L.Events))
	}
	start := len(e.L.Events)
	e.checkStateBased()
	// Retaining an arena tombstone for replay is fine; being a member of a
	// game zone is not. Exile is an actual game zone, not a deletion marker.
	for z := state.ZLibrary; z <= state.ZCommand; z++ {
		for p := range e.G.Players {
			if slices.Contains(e.G.Zone(z, state.PlayerID(p)), tokens[0]) {
				t.Errorf("CR 704.5d Raise the Alarm/Unsummon seq %d: departed token %d remains in %s; want absent from all game zones", start, tokens[0], z)
			}
		}
	}
}

func TestCR704NoncombatDeathtouchDestroysDamagedCreature(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Prodigal Pyromancer", "Lace with Moonglove"}, nil)
	pyro := crAbortMove(t, e, 0, "Prodigal Pyromancer", state.ZBattlefield)
	lace := crAbortMove(t, e, 0, "Lace with Moonglove", state.ZHand)
	victim := crAbortMove(t, e, 1, "Serra Avenger", state.ZBattlefield)
	lf := e.G.Obj(lace).Face()
	e.resolveAbility(lace, 0, []state.Target{{Obj: pyro}}, lf.SpellAbility(), lf.SVars)
	f := e.G.Obj(pyro).Face()
	if len(f.Abilities) != 1 || f.Abilities[0].API != "DealDamage" || f.Abilities[0].Params["NumDmg"] != "1" || !e.HasKeyword(pyro, "Deathtouch") {
		t.Fatal("CR 704.5h Prodigal Pyromancer/Lace with Moonglove seq 0: real 1-damage deathtouch fixture changed")
	}
	// Resolve the real AB without the unrelated activation payment gates.
	e.resolveAbility(pyro, 0, []state.Target{{Obj: victim}}, f.Abilities[0], f.SVars)
	if e.G.Obj(victim).Damage != 1 {
		t.Fatalf("CR 704.5h Prodigal Pyromancer seq %d: damage did not occur", len(e.L.Events))
	}
	start := len(e.L.Events)
	e.checkStateBased()
	if e.G.Obj(victim).Zone != state.ZGraveyard {
		t.Errorf("CR 704.5h Prodigal Pyromancer/Lace with Moonglove/Serra Avenger seq %d: 1 deathtouch damage left a 3/3 in %s; want graveyard", start, e.G.Obj(victim).Zone)
	}
}

func TestCR704AuraBecomesIllegalAfterProtection(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Rancor", "Dominaria's Judgment", "Plains", "Island", "Swamp", "Mountain", "Forest"}, nil)
	bearer := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
	aura := crAbortMove(t, e, 0, "Rancor", state.ZBattlefield)
	e.emit(events.Event{Kind: events.Attach, Obj: aura, IDs: []state.ObjID{bearer}})
	for _, name := range []string{"Plains", "Island", "Swamp", "Mountain", "Forest"} {
		crAbortMove(t, e, 0, name, state.ZBattlefield)
	}
	id := crAbortMove(t, e, 0, "Dominaria's Judgment", state.ZHand)
	f := e.G.Obj(id).Face()
	e.resolveAbility(id, 0, nil, f.SpellAbility(), f.SVars)
	if e.G.Obj(aura).AttachedTo != bearer || !e.HasKeyword(bearer, "Protection from green") {
		t.Fatalf("CR 704.5m Rancor/Dominaria's Judgment seq %d: expected attached green Aura and newly protected bearer", len(e.L.Events))
	}
	start := len(e.L.Events)
	e.checkStateBased()
	if e.G.Obj(aura).Zone != state.ZGraveyard {
		t.Errorf("CR 704.5m (702.16c) Rancor/Dominaria's Judgment seq %d: Aura stayed in %s after bearer gained protection from green; want graveyard before its return trigger resolves", start, e.G.Obj(aura).Zone)
	}
}
