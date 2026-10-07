package rules

// Reference: Magic: The Gathering Comprehensive Rules, 2026-08-07.
// CR 608.2b (5485-5498): recheck legality, not merely the target's zone.
// CR 608.2n (5599-5601): the final graveyard move (NOT 608.2m).
// Scenario guards below assert real fixtures and completed operations; there
// are deliberately no unconditional "checked++" vacuity counters.

import (
	"strconv"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Each seat starts with a real repo deck, plus only compiled corpus supplements.
func crResolutionEngine(t *testing.T, extras ...[]string) *Engine {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	cfg := Config{Seed: 42, Tokens: reg.Tokens}
	for p, ns := range extras {
		name := "ur-delver"
		if p == 1 {
			name = "death-n-taxes"
		}
		cfg.Names = append(cfg.Names, name)
		deck := append([]*cards.Card{}, testutil.RepoDeck(t, reg, name)...)
		for _, n := range ns {
			c, ok := reg.Lookup(n)
			if !ok {
				t.Fatalf("CR 608 fixture %s seq 0: missing corpus card", n)
			}
			deck = append(deck, c)
		}
		cfg.Decks = append(cfg.Decks, deck)
	}
	// Seat 0 (the caster) is the protagonist; seatZeroStart advances the
	// seed until the CR 103.1 toss starts seat 0. It runs here, after the
	// decks are appended -- on the deck-less Config it would be a no-op
	// (New with no Names draws no toss).
	cfg = seatZeroStart(cfg)
	e := New(cfg)
	e.Advance()
	toMain1(t, e)
	return e
}

func crResolutionRound(t *testing.T, e *Engine) {
	t.Helper()
	n := e.G.AliveCount()
	for i := 0; i < n; i++ {
		if d := e.Pending(); d == nil || d.Kind != decision.KPriority {
			t.Fatalf("CR 608 fixture seq %d: expected priority, got %+v", len(e.L.Events), d)
		}
		crAbortAnswer(t, e, "resolution", crAbortOption(t, e, "resolution", "pass", 0))
	}
}

func crResolutionPlayerTarget(t *testing.T, e *Engine, p state.PlayerID) {
	t.Helper()
	if d := e.Pending(); d != nil && d.Kind == decision.KTarget {
		for _, o := range d.Options {
			if o.Kind == "player" && o.Player == p {
				crAbortAnswer(t, e, "resolution", o.Index)
				return
			}
		}
	}
	t.Fatalf("CR 608 fixture seq %d: player %d not offered", len(e.L.Events), p)
}

// This corpus invariant examines single-face, required-target, literal burn
// SAs without sub-abilities. It isolates resolution from casting: normal hand
// origin, no alternative permission, no flashback/copy flags. Thus Eelectrocute's
// graveyard-casting replacement must NOT exile either an ordinary resolution
// or an all-targets-illegal spell. Expectations use zones/order, never the
// engine's target or resting-zone helpers. Counter counts EXAMINED SAs.
func TestCR608CorpusOrdinaryBurnFinishesInGraveyard(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, c := range reg.AllCards() {
		if len(c.Faces) != 1 {
			continue
		}
		f := c.Faces[0]
		sa := f.SpellAbility()
		if f.IsPermanent() || f.IsLand() || sa == nil || sa.Kind != "SP" || sa.API != "DealDamage" || sa.Sub != nil {
			continue
		}
		switch sa.Params["ValidTgts"] {
		case "Any", "Creature", "Creature,Player":
		default:
			continue
		}
		if sa.Params["TargetMin"] != "" && sa.Params["TargetMin"] != "1" {
			continue
		}
		n, err := strconv.Atoi(sa.Params["NumDmg"])
		if err != nil || n < 1 || n > 20 {
			continue
		}
		// Guard movement replacements rather than silently asserting graveyard
		// for a future card with a legitimate self-exile/shuffle rule. Counter
		// and damage replacements do not change this resting-zone oracle.
		for _, r := range f.Repls {
			if r.Event == "Moved" && (f.Name != "Eelectrocute" || r.Params["ValidLKI"] != "Card.CastSa Spell.MayPlaySource") {
				t.Fatalf("CR 608.2n %s seq 0: new movement replacement needs an independent resting-zone oracle", f.Name)
			}
		}
		checked++
		for _, departed := range []bool{false, true} {
			e := crAbortEngine(t, reg, "ur-delver", f.Name)
			id := crAbortMove(t, e, 0, f.Name, state.ZHand)
			target := crAbortMove(t, e, 0, "Delver of Secrets", state.ZBattlefield)
			e.emit(events.Event{Kind: events.PutOnStack, Obj: id, Player: 0, From: state.ZHand, To: state.ZStack})
			e.emit(events.Event{Kind: events.TargetsChosen, Obj: id, IDs: []state.ObjID{target}})
			if departed {
				e.emit(events.Event{Kind: events.MoveZone, Obj: target, From: state.ZBattlefield, To: state.ZGraveyard})
			}
			start := len(e.L.Events)
			e.pending = nil
			e.resolveTop()
			// A burn SA may carry an UnlessCost$ (Blazing Salvo's "deals 3
			// damage unless that creature's controller has CARDNAME deal 5
			// damage to them"): the unless-pay ask suspends the resolution
			// mid-way and the resting zone is only reached once it is
			// answered. Answer every pending ask with its LAST option — the
			// decline arm of the pay/decline KModes — the conservative answer
			// that lets the effect's own body run and the spell rest. Nothing
			// else in this population asks.
			for i := 0; i < 4 && e.pending != nil; i++ {
				d := e.pending
				if d.Kind != decision.KModes || d.ResumeKind != "unless_pay" {
					break // the resolution finished; what is pending is not this ask
				}
				if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player,
					Choices: []int{d.Options[len(d.Options)-1].Index}}); err != nil {
					t.Fatalf("CR 608.2b %s: answer the pending ask: %v", f.Name, err)
				}
			}
			resolves, resolved, moved := 0, -1, -1
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.Resolve && ev.Obj == id {
					resolves++
					resolved = int(ev.Seq)
				}
				if ev.Kind == events.MoveZone && ev.Obj == id && ev.From == state.ZStack {
					moved = int(ev.Seq)
				}
				if departed && ev.Kind == events.Damage {
					t.Errorf("CR 608.2b %s seq %d: all targets departed but damage ran", f.Name, ev.Seq)
				}
			}
			wantResolves := 1
			if departed {
				wantResolves = 0
			}
			if resolves != wantResolves || moved <= resolved || e.G.Obj(id).Zone != state.ZGraveyard || len(e.G.Stack) != 0 {
				t.Errorf("CR 608.2b/n %s seq %d: departed=%t resolves=%d finalMove=%d zone=%s stack=%v; want resolves=%d then graveyard", f.Name, start, departed, resolves, moved, e.G.Obj(id).Zone, e.G.Stack, wantResolves)
			}
		}
	}
	if checked == 0 {
		t.Fatal("CR 608.2b/n corpus seq 0: no compiled burn SAs examined")
	}
	t.Logf("MEASURED CR 608.2b/n examined=%d compiled single-face literal burn SAs, live/departed target cases each", checked)
}

func TestCR608ResolutionRechecksVinesTargetRestriction(t *testing.T) {
	t.Parallel()
	e := crResolutionEngine(t, nil, []string{"Vines of Vastwood"})
	bolt := crAbortMove(t, e, 0, "Lightning Bolt", state.ZHand)
	vines := crAbortMove(t, e, 1, "Vines of Vastwood", state.ZHand)
	target := crAbortMove(t, e, 1, "Serra Avenger", state.ZBattlefield)
	vsa := e.G.Obj(vines).Face().SpellAbility()
	if vsa == nil || vsa.Sub == nil || vsa.Sub.API != "Effect" || vsa.Sub.Params["StaticAbilities"] != "STCantTarget" {
		t.Fatal("CR 608.2b Vines of Vastwood seq 0: restriction fixture changed")
	}
	e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "R", Amount: 1})
	e.emit(events.Event{Kind: events.ManaAdd, Player: 1, Counter: "G", Amount: 1})
	e.askPriority(0)
	crAbortAnswer(t, e, "Lightning Bolt", crAbortOption(t, e, "Lightning Bolt", "cast", bolt))
	crAbortAnswer(t, e, "Lightning Bolt", crAbortOption(t, e, "Lightning Bolt", "permanent", target))
	crAbortAnswer(t, e, "response", crAbortOption(t, e, "response", "pass", 0))
	crAbortAnswer(t, e, "Vines of Vastwood", crAbortOption(t, e, "Vines of Vastwood", "cast", vines))
	crAbortAnswer(t, e, "Vines of Vastwood", crAbortOption(t, e, "Vines of Vastwood", "permanent", target))
	crResolutionRound(t, e)
	if e.G.Obj(vines).Zone != state.ZGraveyard || e.G.Obj(target).Damage != 0 || len(e.G.Stack) != 1 {
		t.Fatalf("CR 608.2b Vines of Vastwood seq %d: response did not finish cleanly", len(e.L.Events))
	}
	start := len(e.L.Events)
	crResolutionRound(t, e)
	// Oracle: Vines controlled by seat 1 forbids seat 0's Bolt from targeting
	// this creature for the rest of the turn. No engine targeting helper is an oracle.
	if e.G.Obj(target).Zone != state.ZBattlefield || e.G.Obj(target).Damage != 0 {
		t.Errorf("CR 608.2b Lightning Bolt/Vines of Vastwood seq %d: illegal target affected, zone=%s damage=%d", start, e.G.Obj(target).Zone, e.G.Obj(target).Damage)
	}
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Resolve && ev.Obj == bolt {
			t.Errorf("CR 608.2b Lightning Bolt seq %d: all targets illegal but Resolve emitted", ev.Seq)
		}
	}
}
