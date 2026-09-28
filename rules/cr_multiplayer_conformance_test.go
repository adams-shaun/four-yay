package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 800.4a: 12361-12367; 800.4e: 12408-12409;
// 800.6: 12452-12454; 800.7: 12456-12458; 802.4: 12673-12676.
// All cards/SAs come from the compiled corpus; each seat has a repo deck.
// I-1's departed unless-pay continuation belongs to the existing CR608 test,
// not these independent ownership, stack-cleanup and combat obligations.

import (
	"slices"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func crMultiplayerConfig(t *testing.T, reg *cards.Registry, deck string) Config {
	t.Helper()
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	for i := 0; i < 4; i++ {
		cfg.Names = append(cfg.Names, deck)
		cfg.Decks = append(cfg.Decks, testutil.RepoDeck(t, reg, deck))
	}
	return cfg
}

// Traverse every physical card in every Legacy repo deck, not just a hand-picked
// permanent. Distribute the departing seat's cards across ordinary zones;
// battlefield candidates must be permanents. The oracle is zone membership,
// not arena deletion (an implementation may retain inert historical records).
// crProbeAnswerAsks settles every non-priority decision the probe's raw
// entry emits suspend on, then returns once the engine is back at a priority
// decision. A Cavern of Souls moved onto the battlefield by the fixture asks
// its "as this enters" creature-type choice at entry (task ct1 made the
// mid-resolution ChooseType real; a raw MoveZone entry is not a cast, so no
// cast-time ask pre-recorded it), and the ask must be answered before the
// probe's own askPriority can post — the overwrite guard would panic
// otherwise. The probe answers the first legal option, the same
// deterministic shape botpolicy's clamp fallback gives a bot.
func crProbeAnswerAsks(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 40; i++ {
		d := e.Pending()
		if d == nil || d.Kind == decision.KPriority {
			return
		}
		if len(d.Options) == 0 {
			t.Fatalf("probe entry ask with no options: %+v", d)
		}
		if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: []int{d.Options[0].Index}}); err != nil {
			t.Fatalf("probe entry ask answer: %v", err)
		}
	}
	t.Fatal("probe entry asks never settled")
}

func TestCR800DepartedOwnersCardsLeaveEveryZone(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	zones := []state.Zone{state.ZLibrary, state.ZHand, state.ZGraveyard, state.ZExile, state.ZBattlefield}
	for _, deck := range testutil.LegacyDeckNames() {
		e := New(crMultiplayerConfig(t, reg, deck))
		var owned []state.ObjID
		for _, o := range e.G.Objs {
			if o.ID != 0 && o.Owner == 1 && o.Card != nil {
				owned = append(owned, o.ID)
			}
		}
		for i, id := range owned {
			o := e.G.Obj(id)
			to := zones[i%len(zones)]
			if to == state.ZBattlefield && (o.Face().IsInstant() || o.Face().IsSorcery()) {
				to = state.ZHand
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: o.Zone, To: to})
			crProbeAnswerAsks(t, e)
			checked++ // EXAMINED physical cards, never offending ones.
		}
		e.askPriority(1)
		start := len(e.L.Events)
		crAbortAnswer(t, e, deck, crAbortOption(t, e, deck, "concede", 0))
		if !e.G.Players[1].Lost || e.G.Over {
			t.Fatalf("CR 800.4a %s seq %d: fixture must leave three players in an ongoing game", deck, start)
		}
		// Report one representative per zone/deck rather than flooding the
		// lane with one error for every physical copy of a basic land.
		for _, z := range zones {
			for _, id := range e.G.Zone(z, 1) {
				if !slices.Contains(owned, id) {
					continue
				}
				t.Errorf("CR 800.4a %s (%s) seq %d: departed owner's card %d remains in %s; all owned objects must leave the game, not move to exile", e.G.Obj(id).Face().Name, deck, start, id, z)
				break
			}
		}
	}
	if checked == 0 {
		t.Fatal("CR 800.4a repo corpus seq 0: no owned cards examined")
	}
	t.Logf("MEASURED CR 800.4a examined=%d physical repo-deck cards across departure fixtures", checked)
}

func TestCR800DepartedControllersStackObjectsCease(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Lightning Bolt", "Azure Mage"} {
		t.Run(name, func(t *testing.T) {
			e := crResolutionEngine(t, []string{name}, nil, nil, nil)
			zone, kind, mana, amount := state.ZHand, "cast", "R", int32(1)
			if name == "Azure Mage" {
				zone, kind, mana, amount = state.ZBattlefield, "ability", "U", 4
			}
			id := crAbortMove(t, e, 0, name, zone)
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: mana, Amount: amount})
			e.askPriority(0)
			crAbortAnswer(t, e, name, crAbortOption(t, e, name, kind, id))
			if name == "Lightning Bolt" {
				crResolutionPlayerTarget(t, e, 2)
			}
			if len(e.G.Stack) != 1 || e.G.Obj(e.G.Stack[0]).Controller != 0 || e.Pending() == nil || e.Pending().Player != 0 {
				t.Fatalf("CR 800.4a %s seq %d: expected completed legal proposal and retained caster priority", name, len(e.L.Events))
			}
			stackID := e.G.Stack[0]
			if name == "Azure Mage" && (e.G.Obj(stackID).Ability == nil || e.G.Obj(stackID).Ability.API != "Draw") {
				t.Fatal("CR 800.4a Azure Mage seq 0: real activated Draw fixture changed")
			}
			start := len(e.L.Events)
			crAbortAnswer(t, e, name, crAbortOption(t, e, name, "concede", 0))
			if !e.G.Players[0].Lost || e.G.Over {
				t.Fatalf("CR 800.4a %s seq %d: concession did not leave three survivors", name, start)
			}
			if slices.Contains(e.G.Stack, stackID) {
				t.Errorf("CR 800.4a %s seq %d: departed controller's stack object %d remains on stack; it must leave immediately, without resolving", name, start, stackID)
			}
		})
	}
}

func TestCR800NoCombatDamageToDepartedDefender(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, []string{"Vampire Nighthawk"}, nil, nil, nil)
	id := crAbortMove(t, e, 0, "Vampire Nighthawk", state.ZBattlefield)
	if e.G.Obj(id).Face().PT != "2/3" || !e.HasKeyword(id, "Lifelink") {
		t.Fatal("CR 800.4e Vampire Nighthawk seq 0: compiled fixture changed")
	}
	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	e.emit(events.Event{Kind: events.DeclareAttackers, Player: 1, IDs: []state.ObjID{id}})
	// Concession can occur between declaration and damage (CR 104.3a).
	// Inject its existing event because the UI offers concede only at priority.
	e.pending = nil
	e.emit(events.Event{Kind: events.PlayerLost, Player: 1, Text: "conceded"})
	e.checkStateBased()
	before, start := e.G.Players[0].Life, len(e.L.Events)
	e.damageStep(false)
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Damage && ev.Obj == 0 && ev.Player == 1 {
			t.Errorf("CR 800.4e Vampire Nighthawk seq %d: dealt %d combat damage to departed seat 1; no damage may be assigned", ev.Seq, ev.Amount)
		}
	}
	if e.G.Players[0].Life != before {
		t.Errorf("CR 800.4e Vampire Nighthawk seq %d: lifelink changed survivor life %d -> %d without a living damage recipient", start, before, e.G.Players[0].Life)
	}
}

func TestCR802BlockDeclarationsFollowAPNAP(t *testing.T) {
	t.Parallel()
	for active := state.PlayerID(0); active < 4; active++ {
		e := crResolutionEngine(t, []string{"Memnite", "Memnite", "Memnite"}, []string{"Memnite", "Memnite", "Memnite"}, []string{"Memnite", "Memnite", "Memnite"}, []string{"Memnite", "Memnite", "Memnite"})
		var attackers []state.ObjID
		for p := state.PlayerID(0); p < 4; p++ {
			n := 1
			if p == active {
				n = 3
			}
			for i := 0; i < n; i++ {
				id := crAbortMove(t, e, p, "Memnite", state.ZBattlefield)
				if p == active {
					attackers = append(attackers, id)
				}
			}
		}
		e.emit(events.Event{Kind: events.TurnChange, Player: active, Amount: e.G.Turn + 1})
		var want []state.PlayerID
		for i, id := range attackers {
			defender := (active + state.PlayerID(i) + 1) % 4
			want = append(want, defender) // fixed seating/turn-order oracle
			e.emit(events.Event{Kind: events.DeclareAttackers, Player: defender, IDs: []state.ObjID{id}})
		}
		crCombatAt(e, state.StepDeclareBlockers)
		var got []state.PlayerID
		for len(got) < len(want) {
			d := e.Pending()
			if d == nil || d.Kind != decision.KBlockers {
				t.Fatalf("CR 802.4 Memnite seq %d: missing defender declaration, got %+v", len(e.L.Events), d)
			}
			got = append(got, d.Player)
			crAbortAnswer(t, e, "Memnite") // legal empty block declaration
		}
		if !slices.Equal(got, want) {
			t.Errorf("CR 802.4 Memnite seq %d: active=%d defender order=%v; want APNAP %v", len(e.L.Events), active, got, want)
		}
	}
	// This fixed four-seat scenario has no filtering that could go vacuous;
	// unlike the repo traversal, it needs no unconditional checked counter.
	t.Log("MEASURED CR 802.4 examined all four active-seat rotations")
}

func TestCR800MultiplayerFirstMulliganIsFree(t *testing.T) {
	t.Parallel()
	cfg := crMultiplayerConfig(t, testutil.CorpusRegistry(t), "ur-delver")
	cfg.Mulligans = 2
	e := New(cfg)
	e.Advance()
	crAbortAnswer(t, e, "ur-delver", crAbortOption(t, e, "ur-delver", "mulligan", 0))
	// Seat 0 has redrawn exactly once. Every seat now keeps. Accept any
	// ordinary ordering of the keep round, without asserting its timing.
	for steps := 0; steps < 16; steps++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("CR 800.6 ur-delver seq 0: missing pregame/priority decision")
		}
		if d.Kind != decision.KMulligan {
			if len(e.G.Zone(state.ZHand, 0)) != 7 {
				t.Errorf("CR 800.6 ur-delver seq %d: first mulligan must retain seven cards", d.Seq)
			}
			return
		}
		if len(d.Options) > 0 && d.Options[0].Kind == "bottom" {
			if d.Min != 0 || d.Max != 0 {
				t.Errorf("CR 800.6 ur-delver (%s) seq %d: first mulligan demands %d..%d cards on bottom; want no penalty", d.Options[0].Label, d.Seq, d.Min, d.Max)
			}
			return
		}
		crAbortAnswer(t, e, "ur-delver", crAbortOption(t, e, "ur-delver", "keep", 0))
	}
	t.Fatal("CR 800.6 ur-delver seq 0: keep round failed to finish within fixture bound")
}

func TestCR800StartingPlayerDrawsInMultiplayer(t *testing.T) {
	t.Parallel()
	e := New(crMultiplayerConfig(t, testutil.CorpusRegistry(t), "ur-delver"))
	e.Advance()
	before := len(e.G.Zone(state.ZHand, 0))
	start := len(e.L.Events)
	toMain1(t, e)
	if e.G.Turn != 1 || e.G.Active != 0 {
		t.Fatalf("CR 800.7 ur-delver seq %d: fixture skipped first main phase", start)
	}
	if got := len(e.G.Zone(state.ZHand, 0)); got != before+1 {
		t.Errorf("CR 800.7 ur-delver seq %d: starting player's hand %d -> %d; want one draw before first main phase in four-player free-for-all", start, before, got)
	}
}
