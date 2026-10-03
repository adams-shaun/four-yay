// RepeatEach's ChangeZoneTable$ True (task agent-20260922T090929Z-07378594).
//
// Forge's RepeatEachEffect creates a CardZoneTable when the parameter is
// present, accumulates every iteration's zone changes and calls
// triggerChangesZoneAll ONCE after the loop -- so a Mode$ ChangesZoneAll
// "one or more" payoff observes the whole loop as one batch, while Mode$
// ChangesZone keeps observing each individual move. Before the fix gorge
// exposed each move separately (rules/trigmatch/zone.go registered
// ChangesZoneAll to the same per-object matcher as ChangesZone,
// "batch-of-one"), so a ChangesZoneAll payoff fired N times for an N-move
// loop.
//
// The pin is the real corpus carrier Organ Harvest --
//
//	A:SP$ RepeatEach | RepeatSubAbility$ DBSac | RepeatPlayers$ NonOpponent |
//	    ChangeZoneTable$ True | ...
//	SVar:DBSac:DB$ Sacrifice | Defined$ Remembered | Amount$ SacX |
//	    SacValid$ Creature | RememberSacrificed$ True | Optional$ True | ...
//
// -- whose loop body sacrifices the caster's creatures: three bears are
// three MoveZone events inside one RepeatEach bracket. The observers are two
// real corpus cards on the board:
//
//   - Simic Slaw (Mode$ ChangesZoneAll, artifact): "Whenever one or more
//     creatures die, put a charge counter on CARDNAME" -- ONE firing for the
//     whole batch, one CHARGE counter;
//   - Black Market (Mode$ ChangesZone, enchantment): "Whenever a creature
//     dies, put a charge counter on CARDNAME" -- the per-move control, N
//     firings, N CHARGE counters.
//
// Both assertions are driven off charge counters, and both cards' trigger
// lines are precondition-asserted, so a silent scan cannot make either count
// vacuous.
package rules

import (
	"maps"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// zoneTableBoard builds the board both tests share: seat 0 holds Simic Slaw
// (the ChangesZoneAll observer), Organ Harvest in hand (the carrier) and
// three Grizzly Bears; seat 1 holds Black Market (the per-move control). All
// three battlefield permanents enter through logged MoveZone events and the
// setup leaves no pending trigger behind.
func zoneTableBoard(t *testing.T, reg *cards.Registry) (e *Engine, cfg Config,
	organ, slaw, market state.ObjID, bears []state.ObjID) {
	t.Helper()
	organCard := mustCorpusCard(t, reg, "Organ Harvest")
	slawCard := mustCorpusCard(t, reg, "Simic Slaw")
	marketCard := mustCorpusCard(t, reg, "Black Market")
	bearCard := searchCorpusCard(t, reg, "Grizzly Bears")

	deck0 := append([]*cards.Card{slawCard, organCard, bearCard, bearCard, bearCard},
		mountainDeck(t, 35)...)
	deck1 := append([]*cards.Card{marketCard}, mountainDeck(t, 39)...)
	cfg = seatZeroStart(Config{Seed: 4408, Names: []string{"harvester", "watcher"},
		Decks: [][]*cards.Card{deck0, deck1}, Tokens: reg.Tokens})
	e = New(cfg)
	e.Advance()

	place := func(c *cards.Card, to state.Zone, p state.PlayerID, all bool) []state.ObjID {
		var out []state.ObjID
		for i := range e.G.Objs {
			o := &e.G.Objs[i]
			if o.Owner != p || o.Card != c {
				continue
			}
			e.emit(events.Event{Kind: events.MoveZone, Obj: o.ID, From: o.Zone, To: to})
			out = append(out, o.ID)
			if !all {
				break
			}
		}
		if len(out) == 0 {
			t.Fatalf("deck object for %q not found", c.Faces[0].Name)
		}
		return out
	}
	slaw = place(slawCard, state.ZBattlefield, 0, false)[0]
	market = place(marketCard, state.ZBattlefield, 1, false)[0]
	bears = place(bearCard, state.ZBattlefield, 0, true)
	organ = place(organCard, state.ZHand, 0, false)[0]
	if len(bears) != 3 {
		t.Fatalf("test precondition: %d bears dealt in, want 3", len(bears))
	}
	if e.G.Obj(organ).Zone != state.ZHand {
		t.Fatalf("test precondition: Organ Harvest in %s, want Hand", e.G.Obj(organ).Zone)
	}
	for _, id := range append([]state.ObjID{slaw, market}, bears...) {
		if e.G.Obj(id).Zone != state.ZBattlefield {
			t.Fatalf("test precondition: %s not on the battlefield", e.G.Obj(id).Face().Name)
		}
	}
	// Precondition: the observers really carry the trigger lines the counts
	// ride on -- a silent scan cannot make either charge count vacuous.
	if f := e.G.Obj(slaw).Face(); f == nil || len(f.Triggers) == 0 || f.Triggers[0].Mode != "ChangesZoneAll" {
		t.Fatalf("test precondition: Simic Slaw face %+v, want a ChangesZoneAll trigger", f)
	}
	if f := e.G.Obj(market).Face(); f == nil || len(f.Triggers) == 0 || f.Triggers[0].Mode != "ChangesZone" {
		t.Fatalf("test precondition: Black Market face %+v, want a ChangesZone trigger", f)
	}
	if len(e.pendingTriggers) != 0 {
		t.Fatalf("test precondition: %d triggers queued by setup, want 0", len(e.pendingTriggers))
	}
	return e, cfg, organ, slaw, market, bears
}

// sacrificeAllBears drives the carrier's Optional$ sacrifice ask: Organ
// Harvest's loop body is a non-strict optional sacrifice of the loop
// subject's creatures, so seat 0 is asked which permanents to hand over. The
// three bears are the whole eligible pool.
func sacrificeAllBears(t *testing.T, e *Engine, bears []state.ObjID) {
	t.Helper()
	d := e.Pending()
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "sacrifice" {
		t.Fatalf("pending = %+v, want the loop body's sacrifice ask", d)
	}
	var choices []int
	for _, o := range d.Options {
		if o.Kind == "sacrifice" && o.Obj != 0 && e.G.Obj(o.Obj).Face() != nil &&
			e.G.Obj(o.Obj).Face().Name == "Grizzly Bears" {
			choices = append(choices, o.Index)
		}
	}
	if len(choices) != len(bears) {
		t.Fatalf("sacrifice options for the bears = %v, want one per bear: %+v", choices, d.Options)
	}
	submitChoices(t, e, choices...)
}

func chargeCount(e *Engine, id state.ObjID, kind string) int {
	if o := e.G.Obj(id); o != nil {
		return int(o.Counter(kind))
	}
	return -1
}

// drainTriggers pushes the resolution's queued triggers onto the stack and
// resolves them (the direct-Resolve tests have no cast flow to do it),
// stopping when nothing is left. Black Market and Simic Slaw's bodies are
// askless PutCounters, so the drain needs no choice of its own.
func drainTriggers(t *testing.T, e *Engine) {
	t.Helper()
	for i := 0; i < 25; i++ {
		if len(e.pendingTriggers) == 0 && len(e.G.Stack) == 0 {
			return
		}
		if d := e.Pending(); d != nil {
			// The multiple simultaneous triggers pose an order ask: answer it
			// with the offered order (the identity permutation), the
			// deterministic queue order.
			switch d.Kind {
			case decision.KTriggerOrder:
				choices := make([]int, 0, len(d.Options))
				for _, o := range d.Options {
					choices = append(choices, o.Index)
				}
				submitChoices(t, e, choices...)
				continue
			case decision.KPriority:
				for _, o := range d.Options {
					if o.Kind == "pass" {
						submitChoices(t, e, o.Index)
						break
					}
				}
				continue
			default:
				t.Fatalf("unexpected decision %v while draining", d.Kind)
			}
		}
		if len(e.pendingTriggers) > 0 {
			e.putTriggersOnStack()
			continue
		}
		e.resolveTop()
	}
	t.Fatalf("triggers did not settle: %d pending, %d on the stack", len(e.pendingTriggers), len(e.G.Stack))
}

func TestRepeatEachChangeZoneTableBatchesChangesZoneAll(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	e, cfg, organ, slaw, market, bears := zoneTableBoard(t, reg)

	// The carrier's compiled spell SA: precondition that it really is the
	// RepeatEach carrying ChangeZoneTable$ True.
	sa := corpusSA(t, reg, "Organ Harvest", "")
	if sa.API != "RepeatEach" {
		t.Fatalf("precondition: Organ Harvest spell API = %q, want RepeatEach", sa.API)
	}
	if !strings.EqualFold(strings.TrimSpace(sa.Params["ChangeZoneTable"]), "True") {
		t.Fatalf("precondition: DBRepeat carries ChangeZoneTable = %q, want True", sa.Params["ChangeZoneTable"])
	}

	effects.Resolve(e, &effects.Ctx{Source: organ, Controller: 0,
		SVars: e.G.Obj(organ).Face().SVars}, sa)
	if e.Pending() == nil {
		t.Fatal("the loop body's optional sacrifice never asked")
	}
	sacrificeAllBears(t, e, bears)
	drainTriggers(t, e)

	// Precondition: the loop body really moved the three distinct bears to
	// the graveyard.
	for _, id := range bears {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("test precondition: bear %d in %v, want Graveyard (the loop body's sacrifice did not move it)", id, o)
		}
	}
	if n := chargeCount(e, market, "CHARGE"); n != len(bears) {
		t.Fatalf("Black Market charge counters = %d, want %d (Mode$ ChangesZone must fire per move)", n, len(bears))
	}
	if n := chargeCount(e, slaw, "CHARGE"); n != 1 {
		t.Fatalf("Simic Slaw charge counters = %d, want 1 (ChangeZoneTable$ True must present the whole loop as ONE ChangesZoneAll batch)", n)
	}
	replayCheck(t, e, cfg)
}

// TestRepeatEachWithoutChangeZoneTableKeepsPerMoveChangesZoneAll is the
// control: the same carrier, the same board, the same three-move loop, with
// the ChangeZoneTable parameter deleted from the compiled SA. Without the
// parameter the loop is NOT batch-scoped, so the ChangesZoneAll observer
// sees every move separately (the pre-fix batch-of-one reading) -- three
// charge counters. This pins that the fix scopes the batching to the
// parameter instead of changing ordinary RepeatEach loops.
func TestRepeatEachWithoutChangeZoneTableKeepsPerMoveChangesZoneAll(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	e, _, organ, slaw, market, bears := zoneTableBoard(t, reg)

	// Delete the parameter from a COPY of the compiled SA: the registry is
	// shared by every test in the binary, and the batching test above reads
	// the very same SA.
	orig := corpusSA(t, reg, "Organ Harvest", "")
	cp := *orig
	cp.Params = maps.Clone(orig.Params)
	sa := &cp
	delete(sa.Params, "ChangeZoneTable")
	if _, present := sa.Params["ChangeZoneTable"]; present {
		t.Fatal("control precondition: ChangeZoneTable still present on the compiled SA")
	}

	effects.Resolve(e, &effects.Ctx{Source: organ, Controller: 0,
		SVars: e.G.Obj(organ).Face().SVars}, sa)
	if e.Pending() == nil {
		t.Fatal("the loop body's optional sacrifice never asked")
	}
	sacrificeAllBears(t, e, bears)
	drainTriggers(t, e)

	for _, id := range bears {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZGraveyard {
			t.Fatalf("control precondition: bear %d in %v, want Graveyard", id, o)
		}
	}
	if n := chargeCount(e, market, "CHARGE"); n != len(bears) {
		t.Fatalf("control: Black Market charge counters = %d, want %d", n, len(bears))
	}
	if n := chargeCount(e, slaw, "CHARGE"); n != len(bears) {
		t.Fatalf("control: Simic Slaw charge counters = %d, want %d (no ChangeZoneTable: the batch-of-one per-move reading stands)", n, len(bears))
	}
}
