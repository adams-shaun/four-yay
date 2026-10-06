package rules

// Opt-in CR 733 exit audit. All cards are compiled corpus cards. The base
// decks are repo decks; Dread Return and Treasure Cruise are explicit corpus
// supplements so the sacrifice/delve probes are NONCREATURE spells while an
// active Young Pyromancer remains on the battlefield. No scripts or SAs are
// manufactured. Direct entries are defensive-path probes, not client claims.

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func crAbortEngine(t *testing.T, reg *cards.Registry, deckName string, extra ...string) *Engine {
	t.Helper()
	deck := append([]*cards.Card{}, testutil.RepoDeck(t, reg, deckName)...)
	if deckName != "ur-delver" {
		deck = append(deck, testutil.RepoDeck(t, reg, "ur-delver")...)
	}
	for _, name := range extra {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("CR 733.1 fixture %s seq 0: missing corpus supplement", name)
		}
		deck = append(deck, c)
	}
	e := New(Config{Seed: 42, Names: []string{"caster", "opponent"}, Decks: [][]*cards.Card{deck, testutil.RepoDeck(t, reg, "death-n-taxes")}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	return e
}

func crAbortMove(t *testing.T, e *Engine, p state.PlayerID, name string, to state.Zone) state.ObjID {
	t.Helper()
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, id := range e.G.Zone(z, p) {
			if e.G.Obj(id).Face().Name == name {
				if z != to {
					e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: z, To: to})
				}
				return id
			}
		}
	}
	t.Fatalf("CR 733.1 fixture %s seq %d: card absent from seat %d's repo/supplement deck", name, len(e.L.Events), p)
	return 0
}

func crAbortPyromancer(t *testing.T, e *Engine) state.ObjID {
	t.Helper()
	id := crAbortMove(t, e, 0, "Young Pyromancer", state.ZBattlefield)
	checked := 0
	for _, tr := range e.G.Obj(id).Face().Triggers {
		checked++
		if tr.Mode == "SpellCast" && tr.Params["ValidCard"] == "Instant,Sorcery" {
			return id
		}
	}
	t.Fatalf("CR 733.1 Young Pyromancer seq %d: examined %d triggers, missing audited instant/sorcery probe", len(e.L.Events), checked)
	return 0
}

func crAbortAnswer(t *testing.T, e *Engine, name string, choices ...int) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatalf("CR 601.2/733.1 %s seq %d: no decision to answer", name, len(e.L.Events))
	}
	if err := e.Submit(decision.Intent{Seq: d.Seq, Player: d.Player, Choices: choices}); err != nil {
		t.Fatalf("CR 601.2/733.1 %s seq %d: %v", name, d.Seq, err)
	}
}

func crAbortOption(t *testing.T, e *Engine, name, kind string, id state.ObjID) int {
	t.Helper()
	if d := e.Pending(); d != nil {
		for _, opt := range d.Options {
			if opt.Kind == kind && (id == 0 || opt.Obj == id) {
				return opt.Index
			}
		}
	}
	t.Fatalf("CR 601.2/733.1 %s seq %d: missing %s option for %d", name, len(e.L.Events), kind, id)
	return 0
}

// Snapshot oracle: original zones/order, hand sizes, pools, life, object
// characteristics (including tapped/counters/cast flags/choices), stack and
// priority. No target-discovery or cost implementation computes expectations.
func crAbortUnchanged(t *testing.T, e *Engine, before *state.Game, start int, name string, pyro state.ObjID) {
	t.Helper()
	for p := range before.Players {
		if !reflect.DeepEqual(e.G.Players[p], before.Players[p]) {
			t.Errorf("CR 733.1 %s seq %d: player %d changed: before=%+v after=%+v", name, start, p, before.Players[p], e.G.Players[p])
		}
		for _, z := range []state.Zone{state.ZHand, state.ZLibrary, state.ZBattlefield, state.ZGraveyard, state.ZExile} {
			if !slices.Equal(before.Zone(z, state.PlayerID(p)), e.G.Zone(z, state.PlayerID(p))) {
				t.Errorf("CR 733.1 %s seq %d: seat %d %s changed: %v -> %v", name, start, p, z, before.Zone(z, state.PlayerID(p)), e.G.Zone(z, state.PlayerID(p)))
			}
		}
	}
	for _, old := range before.Objs {
		got := e.G.Obj(old.ID)
		if got == nil {
			t.Errorf("CR 733.1 %s seq %d: object %d disappeared", name, start, old.ID)
		} else {
			// LatestCast* is append-only event history: PutOnStack remains in
			// the log after CR 733.1 reverses the proposal, so the folded pair
			// intentionally matches the latest logged cast rather than rolling
			// back with the object's reversible characteristics.
			oldState, gotState := old, *got
			oldState.LatestCastFrom, oldState.LatestCastBy, oldState.HasLatestCast = 0, 0, false
			gotState.LatestCastFrom, gotState.LatestCastBy, gotState.HasLatestCast = 0, 0, false
			if !reflect.DeepEqual(oldState, gotState) {
				t.Errorf("CR 733.1 %s seq %d: object %d (%s) not restored (chosen number %d -> %d)", name, start, old.ID, old.Face().Name, old.ChosenNumber, got.ChosenNumber)
			}
		}
	}
	if !slices.Equal(before.Stack, e.G.Stack) || len(before.Objs) != len(e.G.Objs) || e.cast != nil || e.choosing != chooseNone || len(e.pendingTriggers) != 0 || e.drainAwaitsTarget {
		t.Errorf("CR 733.1 %s seq %d: stack %v -> %v objects %d -> %d cast=%t choosing=%d queue=%d drain=%t", name, start, before.Stack, e.G.Stack, len(before.Objs), len(e.G.Objs), e.cast != nil, e.choosing, len(e.pendingTriggers), e.drainAwaitsTarget)
	}
	if d := e.Pending(); d == nil || d.Kind != decision.KPriority || d.Player != before.Priority || e.G.Priority != before.Priority || e.G.Over {
		t.Errorf("CR 733.2 %s seq %d: original priority %d not restored: pending=%+v priority=%d over=%t", name, start, before.Priority, d, e.G.Priority, e.G.Over)
	}
	pushes := 0
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Note {
			t.Logf("MEASURED %s seq %d: Note %q", name, ev.Seq, ev.Text)
		}
		if ev.Kind == events.TriggerPush && ev.Obj == pyro {
			pushes++
			t.Errorf("CR 733.1 %s seq %d: Young Pyromancer TriggerPush at seq %d", name, start, ev.Seq)
		}
		if ev.Kind == events.Resolve {
			t.Errorf("CR 601.2i/733.1 %s seq %d: aborted proposal produced kind=%v at seq %d", name, start, ev.Kind, ev.Seq)
		}
	}
	t.Logf("MEASURED %s seq %d: source resources checked, Pyromancer pushes=%d queue=%d suppressed=%v", name, start, pushes, len(e.pendingTriggers), e.suppressedCast)
}

// TestCR733EarlyAbortSitesPreserveResources covers four sites that are
// CORRECT, which refutes the premise that sent the audit looking for defects in
// them. cast.go:437 (sacrifice unpayable), cast.go:800 (spell mana unpayable),
// cast.go:747 (activation mana unpayable) and cast.go:708 (source moved) each
// preserve every zone, pool, life total, object characteristic, the stack and
// priority -- and fire NO Young Pyromancer trigger, which is the measurement
// that killed an earlier cheap fix for the illegal-cast path. They belong in the
// ordinary suite, where a commitCast refactor would trip them.
//
// spell_mana_after_choice was a fifth site and is deliberately NOT here: it
// was a real defect (Sanctum Prelate's ChosenNumber survived a mana abort)
// now fixed in abortCast and graduated separately below, so this test keeps
// its original four correct sites.
func TestCR733EarlyAbortSitesPreserveResources(t *testing.T) {
	t.Parallel()
	crAbortSites(t, []string{"sacrifice", "spell_mana", "activation_mana", "source_moved"})
}

func TestCR733AbortAfterAsEntersChoiceKeepsChosenNumber(t *testing.T) {
	t.Parallel()
	// Graduated: passes with the conformance flag on; runs in the ordinary lane.
	// It pins CR 733.1's undo of an as-enters choice: Sanctum Prelate's number
	// choice is recorded, then a mana abort must return the object to its
	// pre-proposal choice state (abortCast emits reverse Choose events, only
	// when a choice was recorded during the proposal), while the whole-object
	// snapshot oracle confirms every other resource is restored too. It shares
	// its probe with the four correct sites above so the two cannot drift apart.
	crAbortSites(t, []string{"spell_mana_after_choice"})
}

// TestCR733AbortRestoresZoneEntryHistory pins the derived state that the
// reverse stack move itself overwrites. The TurnChange makes the before value
// deliberately false (with its prior provenance retained), so both fields must
// be restored exactly after Lightning Bolt's real insufficient-mana abort.
// TestTurnChangeResetsZoneEntryAndDamageHistory remains the committed-move
// control: ordinary moves must still record true and their actual departure.
func TestCR733AbortRestoresZoneEntryHistory(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e := crAbortEngine(t, reg, "uw-control")
	id := crAbortMove(t, e, 0, "Lightning Bolt", state.ZHand)

	e.emit(events.Event{Kind: events.TurnChange, Player: 0, Amount: e.G.Turn + 1})
	before := *e.G.Obj(id)
	if before.EnteredThisTurn || before.EnteredFrom != state.ZLibrary {
		t.Fatalf("CR 733.1 Lightning Bolt seq %d: fixture history = %t/%s, want false/library", len(e.L.Events), before.EnteredThisTurn, before.EnteredFrom)
	}
	e.askPriority(0)
	e.pending = nil // Direct proposal exercises the real insufficient-mana abort site.
	e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
	e.Advance()

	got := e.G.Obj(id)
	if got.EnteredThisTurn != before.EnteredThisTurn || got.EnteredFrom != before.EnteredFrom {
		t.Fatalf("CR 733.1 Lightning Bolt seq %d: abort history = %t/%s, want %t/%s", len(e.L.Events), got.EnteredThisTurn, got.EnteredFrom, before.EnteredThisTurn, before.EnteredFrom)
	}
}

func crAbortSites(t *testing.T, sites []string) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, site := range sites {
		t.Run(site, func(t *testing.T) {
			e := crAbortEngine(t, reg, "uw-control", "Dread Return", "Stoneforge Mystic", "Sanctum Prelate")
			pyro := crAbortPyromancer(t, e)
			name := "Lightning Bolt"
			switch site {
			case "sacrifice":
				name = "Dread Return"
			case "activation_mana":
				name = "Stoneforge Mystic"
			case "source_moved":
				name = "Entreat the Angels"
			case "spell_mana_after_choice":
				name = "Sanctum Prelate"
			}
			zone := state.ZHand
			if site == "sacrifice" {
				zone = state.ZGraveyard
			} else if site == "activation_mana" {
				zone = state.ZBattlefield
			}
			id := crAbortMove(t, e, 0, name, zone)
			if site == "sacrifice" {
				if cost, ok := e.G.Obj(id).Face().KeywordParam("Flashback"); !ok || cost != "Sac<3/Creature>" || len(e.G.Zone(state.ZBattlefield, 0)) != 1 {
					t.Fatalf("CR 733.1 %s seq %d: fixture needs three sacrifices but only Pyromancer available", name, len(e.L.Events))
				}
			}
			if site == "activation_mana" {
				// CR 603.5: the genuine ETB trigger now goes on the stack and its
				// yes/no is asked AS IT RESOLVES, not at placement. It is not an
				// abort consequence, so let the seats pass so the ability reaches
				// its resolution ask, then decline it there — the probe then
				// starts from the same clean board the placement decline used to
				// produce.
				e.pending = nil
				e.Advance()
				for n := 0; e.Pending() != nil && e.Pending().Kind == decision.KPriority && n < 40; n++ {
					crAbortAnswer(t, e, name, crAbortOption(t, e, name, "pass", 0))
				}
				if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
					t.Fatalf("CR 733.1 %s seq %d: expected the ETB optional resolution ask, got %+v", name, len(e.L.Events), d)
				}
				crAbortAnswer(t, e, name, crAbortOption(t, e, name, "no", 0))
			}
			e.askPriority(0)
			before, start := e.G.Clone(), len(e.L.Events)
			e.pending = nil // Direct proposal: emulate Submit's consumption, NOT Game mutation.
			checked++
			switch site {
			case "sacrifice":
				e.beginCast(0, decision.Option{Kind: "cast", Obj: id, Mode: "flashback"})
			case "spell_mana":
				e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
			case "spell_mana_after_choice":
				// The choice now belongs to the entry boundary. This proposal is
				// rejected before the permanent can enter, so no number decision
				// may be pending during the CR 733.1 abort probe.
				e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
				if d := e.Pending(); d != nil && d.Kind == decision.KChoose && d.ResumeKind == "etb" {
					t.Fatalf("CR 733.1 %s seq %d: entry choice was posed before entry", name, start)
				}
			case "activation_mana":
				idx := -1
				for i, ab := range e.G.Obj(id).Face().Abilities {
					if ab.Kind == "AB" && ab.Params["Cost"] == "1 W T" {
						idx = i
					}
				}
				if idx < 0 {
					t.Fatalf("CR 602.2b/733.1 %s seq %d: missing real activation", name, start)
				}
				e.beginActivation(0, decision.Option{Kind: "ability", Obj: id, Ability: idx})
			case "source_moved":
				e.beginCast(0, decision.Option{Kind: "cast", Obj: id})
				if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Options[0].Kind != "x" {
					t.Fatalf("CR 733.1 %s seq %d: missing X suspension", name, start)
				}
				// Explicit fault injection, NOT a reachable opponent action
				// during casting. Only continuation effects should be undone;
				// the external move is excluded from the snapshot oracle.
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile})
				before, start = e.G.Clone(), len(e.L.Events)
				crAbortAnswer(t, e, name, 0)
			}
			e.Advance()
			crAbortUnchanged(t, e, before, start, name, pyro)
		})
	}
	if checked == 0 {
		t.Fatal("CR 733.1 abort-site vacuity guard: examined zero cases (seq 0)")
	}
}

func TestCR733UnderDelveReversalAllowsLegalRetry(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, paid := range []int{0, 1, 7} {
		t.Run(string(rune('0'+paid))+"_exiles", func(t *testing.T) {
			// F05-2 (CR 733.2) closed this: the first no-progress under-delve
			// abort no longer suppresses the cast option, so the reversed
			// action may be redone legally. The guard comes off and these
			// leaves join the ordinary suite; the 7_exiles full-payment
			// control stays opt-in-free as it always was.
			e := crAbortEngine(t, reg, "ur-delver", "Treasure Cruise")
			pyro := crAbortPyromancer(t, e)
			id := crAbortMove(t, e, 0, "Treasure Cruise", state.ZHand)
			if _, ok := e.G.Obj(id).Face().KeywordParam("Delve"); !ok || e.G.Obj(id).Face().ManaCost != "7 U" {
				t.Fatalf("CR 733.1 Treasure Cruise seq %d: fixture changed", len(e.L.Events))
			}
			for i := 0; i < 7; i++ {
				gy := e.G.Zone(state.ZLibrary, 0)[0]
				e.emit(events.Event{Kind: events.MoveZone, Obj: gy, From: state.ZLibrary, To: state.ZGraveyard})
			}
			e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "U", Amount: 1})
			e.askPriority(0)
			before, start := e.G.Clone(), len(e.L.Events)
			checked++
			crAbortAnswer(t, e, "Treasure Cruise", crAbortOption(t, e, "Treasure Cruise", "cast", id))
			if d := e.Pending(); d == nil || d.Kind != decision.KChoose || d.Min != 0 || d.Max != 7 || len(d.Options) != 7 || d.Options[0].Kind != "exile" {
				t.Fatalf("CR 601.2h/733.1 Treasure Cruise seq %d: missing seven-card delve choice: %+v", start, d)
			}
			choices := []int{}
			for i := 0; i < paid; i++ {
				choices = append(choices, i)
			}
			crAbortAnswer(t, e, "Treasure Cruise", choices...)
			if paid == 7 {
				pushes := 0
				for _, ev := range e.L.Events[start:] {
					if ev.Kind == events.TriggerPush && ev.Obj == pyro {
						pushes++
					}
				}
				if e.G.Obj(id).Zone != state.ZStack || len(e.G.Zone(state.ZGraveyard, 0)) != 0 || len(e.G.Zone(state.ZExile, 0)) != 7 || e.G.Players[0].Pool.Total() != 0 || pushes != 1 {
					t.Errorf("CR 601.2h-i Treasure Cruise seq %d: U plus seven exiles control failed: zone=%s graveyard=%v exile=%v pool=%v Pyromancer=%d", start, e.G.Obj(id).Zone, e.G.Zone(state.ZGraveyard, 0), e.G.Zone(state.ZExile, 0), e.G.Players[0].Pool, pushes)
				}
				t.Logf("MEASURED Treasure Cruise seq %d: full-payment control Pyromancer pushes=%d", start, pushes)
				return
			}
			crAbortUnchanged(t, e, before, start, "Treasure Cruise", pyro)
			// Fixed oracle: the SAME U and SEVEN graveyard cards still pay
			// {7}{U} with delve. Repeating with all seven is legal, CR 733.2.
			found := false
			for _, opt := range e.legalActions(0) {
				if opt.Kind == "cast" && opt.Obj == id {
					found = true
				}
			}
			if !found {
				t.Errorf("CR 733.2 Treasure Cruise seq %d: cannot redo reversed action legally using U + seven graveyard cards; cast option suppressed", start)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 733.2 Treasure Cruise seq 0: no under-delve cases examined")
	}
}

// All three arms are green in the ordinary suite: the miracle entry check, the
// early return and the cast-trigger control. Each arm was checked against a
// deliberate production mutation before the former opt-in guard came off:
// dropping the o.Zone != state.ZHand check in miracle.go's castMiracle turns
// stale_card red, and suppressing the CastInfo emit at cast.go:810 turns
// paid_control red.
func TestCR733MiracleAbortAndTriggerControl(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	checked := 0
	for _, arm := range []string{"unpayable", "stale_card", "paid_control"} {
		t.Run(arm, func(t *testing.T) {
			e := crAbortEngine(t, reg, "uw-control")
			pyro := crAbortPyromancer(t, e)
			id := crAbortMove(t, e, 0, "Terminus", state.ZLibrary)
			if mc, ok := e.G.Obj(id).Face().KeywordParam("Miracle"); !ok || mc != "W" {
				t.Fatalf("CR 702.94a/733.1 Terminus seq %d: real miracle cost changed", len(e.L.Events))
			}
			if arm == "paid_control" {
				e.emit(events.Event{Kind: events.ManaAdd, Player: 0, Counter: "W", Amount: 1})
			}
			// Setup draw precedes the proposal. It is NOT reversed, and this
			// is not a claim that the library exceptions in 733.1 are undone.
			e.emit(events.Event{Kind: events.Draw, Player: 0, Obj: id, From: state.ZLibrary, To: state.ZHand, Secret: true})
			e.pending = nil
			e.Advance()
			if d := e.Pending(); d == nil || d.Kind != decision.KTriggerOptional {
				t.Fatalf("CR 702.94a Terminus seq %d: missing real first-draw offer: %+v", len(e.L.Events), d)
			}
			if arm == "stale_card" {
				// Fault injection of the guard's documented stale-queue case.
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZExile})
			}
			before, start := e.G.Clone(), len(e.L.Events)
			checked++
			crAbortAnswer(t, e, "Terminus", crAbortOption(t, e, "Terminus", "yes", 0))
			pushes, reveals := 0, 0
			for _, ev := range e.L.Events[start:] {
				if ev.Kind == events.TriggerPush && ev.Obj == pyro {
					pushes++
					t.Logf("MEASURED Terminus %s seq %d: Young Pyromancer TriggerPush", arm, ev.Seq)
				}
				if ev.Kind == events.Note && strings.Contains(ev.Text, "(miracle)") {
					reveals++
				}
			}
			if arm == "paid_control" {
				if e.G.Obj(id).Zone != state.ZStack || pushes != 1 || e.G.Players[0].Pool.Total() != 0 || e.G.Obj(id).CastFlags&state.FlagMiracle == 0 {
					t.Errorf("CR 601.2i/702.94a Terminus seq %d: payable control failed: zone=%s pushes=%d pool=%v flags=%v", start, e.G.Obj(id).Zone, pushes, e.G.Players[0].Pool, e.G.Obj(id).CastFlags)
				}
			} else {
				crAbortUnchanged(t, e, before, start, "Terminus", pyro)
			}
			if arm == "stale_card" && reveals != 0 {
				t.Errorf("CR 702.94a Terminus seq %d: stale card incorrectly reached reveal/cast entry", start)
			}
		})
	}
	if checked == 0 {
		t.Fatal("CR 733.1 Terminus seq 0: no miracle cases examined")
	}
}
