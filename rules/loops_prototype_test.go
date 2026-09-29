package rules

// Research only: corpus-backed scripts, deterministic event-based setup and
// ordinary Submit calls. No shortcuts or production behavior are installed.
import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func loopCorpus(t *testing.T) *cards.Registry {
	t.Helper()
	if _, err := os.Stat("../.cards/cardsfolder"); err != nil {
		t.Fatal("loop research requires the real .cards corpus:", err)
	}
	return testutil.CorpusRegistry(t)
}

func TestLoopPrototypeCatalog(t *testing.T) {
	raw, err := os.ReadFile("testdata/loop-combos.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Schema   string
		Revision int
		Combos   []struct {
			ID     string
			Pieces []struct {
				Name   string
				Filter string
				Role   string
			}
			Body   []string
			Breaks []string
			Bound  string
			Test   string
		}
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Schema != "gorge.combo-research.v1" || catalog.Revision != 1 || len(catalog.Combos) != 10 {
		t.Fatal("unexpected research catalog version/size")
	}
	reg := loopCorpus(t)
	seen := map[string]bool{}
	for _, c := range catalog.Combos {
		if c.ID == "" || seen[c.ID] || len(c.Body) == 0 || len(c.Breaks) == 0 || c.Bound == "" || c.Test == "" {
			t.Fatalf("incomplete/duplicate combo %s", c.ID)
		}
		seen[c.ID] = true
		for _, p := range c.Pieces {
			if p.Role == "" || (p.Name == "" && p.Filter == "") {
				t.Fatal("unbound piece", c.ID)
			}
			if p.Name != "" {
				if _, ok := reg.Lookup(p.Name); !ok {
					t.Fatal("unknown catalog card", p.Name)
				}
			}
		}
	}
}

func TestLoopPrototypeCoverage(t *testing.T) {
	reg := loopCorpus(t)
	raw, err := os.ReadFile("testdata/scam-exe.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Commander string
		Cards     []struct {
			Name  string
			Count int
		}
	}
	if err = json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	scan := scanPackages(t)
	reads := scan.derived()
	total := 0
	var names []string
	for _, ent := range file.Cards {
		names = append(names, ent.Name)
		total += ent.Count
	}
	if total != 100 || len(names) != 100 {
		t.Fatalf("deck=%d cards/%d names", total, len(names))
	}
	names = append(names, "Dualcaster Mage", "Molten Duplication", "Saw in Half", "Cathodion", "Nine-Lives Familiar", "The One Ring", "Blood Artist", "Mayhem Devil", "Altar of Dementia")
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatal(name)
		}
		t.Logf("CARD | %s | primitives=%v | params=%v", name, reg.Unsupported(c, effects.Supported()), cardCensusLabels(c, reads, nil))
	}
}

// Same fixture idiom as toMain1 / submitChoices elsewhere in rules. The
// production corpus cards are moved once; iterations never inject resources.
func loopBoard(t *testing.T, names []string, zones []state.Zone) (*Engine, []state.ObjID) {
	t.Helper()
	reg := loopCorpus(t)
	deck := mountainDeck(t, 80)
	for _, name := range names {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatal(name)
		}
		deck = append(deck, c)
	}
	e := New(Config{Seed: 17, Names: []string{"pilot", "opponent"}, Decks: [][]*cards.Card{deck, mountainDeck(t, 80)}, Tokens: reg.Tokens})
	e.Advance()
	toMain1(t, e)
	ids := make([]state.ObjID, len(names))
	for i, name := range names {
		c, _ := reg.Lookup(name)
		for j := range e.G.Objs {
			o := &e.G.Objs[j]
			if o.Owner == 0 && o.Card == c {
				ids[i] = o.ID
				break
			}
		}
		if ids[i] == 0 {
			t.Fatal("missing object", name)
		}
		e.emit(events.Event{Kind: events.MoveZone, Obj: ids[i], From: e.G.Obj(ids[i]).Zone, To: zones[i]})
	}
	e.priorityRound()
	return e, ids
}
func loopPick(t *testing.T, e *Engine, match func(decision.Option) bool) {
	t.Helper()
	d := e.Pending()
	if d == nil {
		t.Fatal("no pending decision")
	}
	for _, o := range d.Options {
		if match(o) {
			submitChoices(t, e, o.Index)
			return
		}
	}
	t.Fatalf("no matching option: kind=%s resume=%s options=%+v", d.Kind, d.ResumeKind, d.Options)
}
func loopAction(t *testing.T, e *Engine, id state.ObjID, kind string) {
	t.Helper()
	loopPick(t, e, func(o decision.Option) bool { return o.Obj == id && o.Kind == kind })
}
func loopDrain(t *testing.T, e *Engine, sac, target state.ObjID, player state.PlayerID) {
	t.Helper()
	for n := 0; n < 200; n++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no pending decision during drain")
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 && d.Player == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			loopPick(t, e, func(o decision.Option) bool { return o.Kind == "pass" })
		case decision.KTarget:
			loopPick(t, e, func(o decision.Option) bool {
				if target != 0 && d.Source == target {
					return o.Obj == target
				}
				if target != 0 && o.Kind != "player" {
					return o.Obj == target
				}
				return o.Kind == "player" && o.Player == player
			})
		case decision.KTriggerOrder:
			var xs []int
			for _, o := range d.Options {
				if o.Obj != target {
					xs = append(xs, o.Index)
				}
			}
			for _, o := range d.Options {
				if o.Obj == target {
					xs = append(xs, o.Index)
				}
			}
			submitChoices(t, e, xs...)
		case decision.KChoose:
			switch {
			case len(d.Options) > 0 && d.Options[0].Kind == "x" && target != 0:
				loopPick(t, e, func(o decision.Option) bool { return o.Amount == int(e.G.Obj(target).Face().ManaValue()) })
			case len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_decline":
				loopPick(t, e, func(o decision.Option) bool { return o.Kind == "trigger_cost_decline" })
			case len(d.Options) > 0 && d.Options[0].Kind == "trigger_cost_pay":
				loopPick(t, e, func(o decision.Option) bool {
					if strings.Contains(d.Prompt, "Sephiroth") {
						return o.Kind == "trigger_cost_decline"
					}
					return o.Kind == "trigger_cost_pay"
				})
			case len(d.Options) > 0 && d.Options[0].Kind == "sacrifice":
				loopPick(t, e, func(o decision.Option) bool { return o.Obj == sac })
			case len(d.Options) > 0 && d.Options[0].Kind == "mana":
				var xs []int
				for _, o := range d.Options {
					if (o.ManaSymbol == "B" || o.Label == "B" || o.Label == "Add B") && len(xs) < d.Min {
						xs = append(xs, o.Index)
					}
				}
				if len(xs) != d.Min {
					t.Fatal("no complete black mana allocation")
				}
				submitChoices(t, e, xs...)
			default:
				t.Fatalf("unexpected choose %+v", d)
			}
		default:
			t.Fatalf("unexpected decision %+v", d)
		}
	}
	t.Fatal("drain exceeded 200 decisions")
}

// Replay the recorded intents from the event-seeded fixture boundary. Unlike
// a second scripted run, this verifies the actual submitted intent timeline.
// The setup itself is not an ordinary match Config and cannot use cmd/repro.
func loopBegin(t *testing.T, e *Engine) int {
	t.Helper()
	before := e.Clone()
	ni := len(e.L.Intents)
	t.Cleanup(func() {
		if t.Failed() {
			return
		}
		for _, in := range e.L.Intents[ni:] {
			if err := before.Submit(in); err != nil {
				t.Fatal("fixture-boundary replay:", err)
			}
		}
		if before.L.Head() != e.L.Head() || !reflect.DeepEqual(before.L.Events, e.L.Events) {
			t.Fatal("fixture-boundary replay diverged")
		}
	})
	return len(e.L.Events)
}
func loopRecord(t *testing.T, e *Engine, label string, n, start int) {
	t.Helper()
	t.Logf("%s N=%d events=%d intents=%d life=%d/%d pool=%v head=%s", label, n, len(e.L.Events)-start, len(e.L.Intents), e.G.Players[0].Life, e.G.Players[1].Life, e.G.Players[0].Pool, e.L.Head())
	if dir := os.Getenv("GORGE_LOOP_TRANSCRIPTS"); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(e.L, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, label+".json"), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// loopGuardStrict, while true, makes loopBlocked fail instead of logging.
// The Scam.EXE coverage guard (loop_coverage_guard_test.go) sets it around
// every prototype it runs: the ratchet treats a catalog line as executable
// only when the prototype genuinely passes, and a blocked prototype returns
// without reaching its assertion, so counting that return as a pass is the
// exact false positive the guard exists to prevent. A standalone prototype
// run leaves the flag false and keeps the research default (a known-blocked
// reproducer logs and passes).
//
// It is a plain bool rather than a parameter because the prototype tests are
// ordinary func(*testing.T) values; the guard writes it on the parent
// goroutine before t.Run starts the subtest goroutine and reads no result
// until that goroutine has ended, so there is no concurrent access.
var loopGuardStrict bool

// loopGuardBlocked records whether loopBlocked took the failure branch on
// the last guard run. executeLoopPrototypes resets it before each t.Run and
// reads it after the subtest goroutine has ended, so a blocked prototype is
// distinguishable from a plain assertion failure. Same single-goroutine
// happens-before argument as loopGuardStrict applies. It is only meaningful
// while loopGuardStrict is set; the standalone logging path leaves it alone.
var loopGuardBlocked bool

// loopBlockedFails reports whether a blocked line must fail rather than log.
// It is the single home of that decision: loopBlocked calls it, and the
// coverage guard's demonstration asserts the strict mode it relies on
// without running a real failing subtest (which would fail the
// demonstration's own parent test).
func loopBlockedFails() bool {
	return loopGuardStrict || os.Getenv("GORGE_LOOP_STRICT") == "1"
}

// GORGE_LOOP_STRICT=1 exposes known failures in other prototype lines as red
// regression reproducers; the Thug line is asserted directly in both modes.
// loopGuardStrict adds a second, in-process mode the coverage guard uses so
// a blocked line can never be mistaken for a passing one.
func loopBlocked(t *testing.T, message string) {
	t.Helper()
	if loopBlockedFails() {
		loopGuardBlocked = true
		t.Fatal(message)
	}
	t.Log("BLOCKED: " + message + " (GORGE_LOOP_STRICT=1 asserts the intended behavior)")
}

func TestLoopPrototypeThug(t *testing.T) {
	for _, n := range []int{1, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e, ids := loopBoard(t, []string{"Rakdos, the Muscle", "Ashnod's Altar", "Golgari Thug", "Poxwalkers"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield, state.ZGraveyard})
			loopDrain(t, e, 0, 0, 0)
			start := loopBegin(t, e)
			clock := time.Now()
			for i := 0; i < n; i++ {
				loopAction(t, e, ids[1], "activate")
				loopDrain(t, e, ids[2], ids[2], 0)
				if e.G.Obj(ids[2]).Zone != state.ZExile {
					t.Fatalf("Golgari Thug should be exiled by Rakdos after its death trigger; got zone %s", e.G.Obj(ids[2]).Zone)
				}
				loopAction(t, e, ids[2], "cast")
				loopDrain(t, e, 0, 0, 0)
				if e.G.Obj(ids[3]).Zone != state.ZBattlefield {
					t.Fatal("Poxwalkers did not return on exile cast")
				}
				loopAction(t, e, ids[1], "activate")
				loopDrain(t, e, ids[3], 0, 1)
				if e.G.Obj(ids[2]).Zone != state.ZBattlefield || e.G.Obj(ids[3]).Zone != state.ZGraveyard {
					t.Fatal("cycle zones not restored")
				}
			}
			wantEvents, wantIntents := 101, 23
			if n == 20 {
				wantEvents, wantIntents = 2020, 384
			}
			if got := len(e.L.Events) - start; got != wantEvents {
				t.Errorf("events=%d, want %d", got, wantEvents)
			}
			if got := len(e.L.Intents); got != wantIntents {
				t.Errorf("intents=%d, want %d", got, wantIntents)
			}
			wantPool := state.Mana{state.MC: int32(2 * n)}
			if got := e.G.Players[0].Pool; got != wantPool {
				t.Errorf("pool=%v, want %v", got, wantPool)
			}
			loopRecord(t, e, "thug-"+fmt.Sprint(n), n, start)
			t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(n))
		})
	}
}

func TestLoopPrototypeNightmare(t *testing.T) {
	for _, n := range []int{1, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e, ids := loopBoard(t, []string{"Chthonian Nightmare", "Priest of Gix", "Priest of Urabrask"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZGraveyard})
			loopDrain(t, e, 0, 0, 0)
			start := loopBegin(t, e)
			clock := time.Now()
			for i := 0; i < n; i++ {
				for _, pair := range [][2]int{{1, 2}, {2, 1}} {
					loopAction(t, e, ids[0], "ability")
					loopDrain(t, e, ids[pair[0]], ids[pair[1]], 0)
					if e.G.Obj(ids[0]).Zone != state.ZHand {
						t.Fatal("Nightmare return cost missing")
					}
					loopAction(t, e, ids[0], "cast")
					loopDrain(t, e, 0, 0, 0)
				}
				if e.G.Players[0].Counter("ENERGY") != 3 {
					t.Fatal("energy is not conserved")
				}
			}
			if e.G.Players[0].Pool.Total() != int32(3+2*n) || e.G.Obj(ids[1]).Zone != state.ZBattlefield || e.G.Obj(ids[2]).Zone != state.ZGraveyard {
				t.Fatal("Nightmare cycle net resources/zones wrong")
			}
			loopRecord(t, e, "nightmare-"+fmt.Sprint(n), n, start)
			t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(n))
		})
	}
}

func TestLoopPrototypeBreach(t *testing.T) {
	for _, n := range []int{1, 20} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e, ids := loopBoard(t, []string{"Underworld Breach", "Altar of Dementia", "Poxwalkers", "Lion's Eye Diamond"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZGraveyard, state.ZGraveyard})
			// Initial fuel only: three real basic lands, not synthetic spell effects.
			hand := append([]state.ObjID(nil), e.G.Zone(state.ZHand, 0)...)
			for _, id := range hand[:3] {
				e.emit(events.Event{Kind: events.MoveZone, Obj: id, From: state.ZHand, To: state.ZGraveyard})
			}
			e.priorityRound()
			start := loopBegin(t, e)
			clock := time.Now()
			lib := len(e.G.Zone(state.ZLibrary, 0))
			for i := 0; i < n; i++ {
				loopAction(t, e, ids[3], "cast")
				d := e.Pending()
				if d == nil || d.Kind != decision.KChoose {
					t.Fatal("escape cost missing")
				}
				var xs []int
				for _, o := range d.Options {
					if e.G.Obj(o.Obj).Face().Name == "Mountain" && len(xs) < 3 {
						xs = append(xs, o.Index)
					}
				}
				if len(xs) != 3 {
					t.Fatal("insufficient escape fuel")
				}
				submitChoices(t, e, xs...)
				loopDrain(t, e, 0, 0, 0)
				if e.G.Obj(ids[2]).Zone != state.ZBattlefield || !e.G.Obj(ids[2]).Tapped {
					t.Fatal("Poxwalkers return missing")
				}
				loopAction(t, e, ids[3], "activate")
				loopDrain(t, e, 0, 0, 0)
				loopAction(t, e, ids[1], "ability")
				loopDrain(t, e, ids[2], 0, 0)
				if len(e.G.Zone(state.ZLibrary, 0)) != lib-3*(i+1) {
					t.Fatal("mill not three per cycle")
				}
			}
			loopRecord(t, e, "breach-"+fmt.Sprint(n), n, start)
			t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(n))
		})
	}
}

// Copy scripts keep the original spell on the stack. Each new Mage's trigger
// copies it; each copy retargets a Mage. Finish by retargeting Blood Pet, whose
// lack of an ETB copy trigger terminates the sequence deliberately.
func TestLoopPrototypeDualcaster(t *testing.T) {
	for _, spell := range []string{"Molten Duplication", "Saw in Half"} {
		t.Run(spell, func(t *testing.T) {
			e, ids := loopBoard(t, []string{spell, "Dualcaster Mage", "Blood Pet"}, []state.Zone{state.ZHand, state.ZHand, state.ZBattlefield})
			addMana(t, e, 0, "BBBBRRRR")
			loopAction(t, e, ids[0], "cast")
			loopPick(t, e, func(o decision.Option) bool { return o.Obj == ids[2] })
			loopAction(t, e, ids[1], "cast")
			start := loopBegin(t, e)
			clock := time.Now()
			copies := 0
			goal := 20
			for step := 0; step < 2000; step++ {
				d := e.Pending()
				if d == nil {
					t.Fatal("no pending decision")
				}
				if d.Kind == decision.KPriority && len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 {
					break
				}
				switch d.Kind {
				case decision.KPriority:
					loopPick(t, e, func(o decision.Option) bool { return o.Kind == "pass" })
				case decision.KTriggerOrder:
					var xs []int
					for _, o := range d.Options {
						xs = append(xs, o.Index)
					}
					submitChoices(t, e, xs...)
				case decision.KTarget:
					if d.ResumeKind == "copy_targets" {
						target := ids[2]
						if copies < goal {
							for _, id := range e.G.Zone(state.ZBattlefield, 0) {
								if e.G.Obj(id).Face().Name == "Dualcaster Mage" {
									target = id
								}
							}
							if target == ids[2] {
								loopRecord(t, e, "dualcaster-blocked", copies, start)
								loopBlocked(t, "Saw in Half produced no live Dualcaster to repeat")
								return
							}
							copies++
						}
						// Once the seed creature dies, retain the inherited (now illegal) target;
						// this copy fizzles rather than restarting the declared loop.
						found := false
						for _, o := range d.Options {
							if o.Obj == target {
								submitChoices(t, e, o.Index)
								found = true
								break
							}
						}
						if !found {
							submitChoices(t, e)
						}
					} else {
						loopPick(t, e, func(o decision.Option) bool { return o.Obj == ids[0] })
					}
				default:
					t.Fatalf("unexpected %s %s", d.Kind, d.Prompt)
				}
				if step == 1999 {
					t.Fatal("copy script exceeded 2000 decisions")
				}
			}
			if copies != goal {
				loopBlocked(t, fmt.Sprintf("%s made %d/%d cycles", spell, copies, goal))
				return
			}
			mages := 0
			for _, id := range e.G.Zone(state.ZBattlefield, 0) {
				if e.G.Obj(id).Face().Name == "Dualcaster Mage" {
					mages++
				}
			}
			if mages != 21 {
				t.Fatalf("%s ended with %d Mages, want 21", spell, mages)
			}
			loopRecord(t, e, "dualcaster-"+spell, copies, start)
			t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(copies))
		})
	}
}

func loopMonkReturns(t *testing.T, e *Engine, burnt, saw state.ObjID) {
	t.Helper()
	picks := 0
	for step := 0; step < 100; step++ {
		d := e.Pending()
		if d == nil {
			t.Fatal("no monk decision")
		}
		if d.Kind == decision.KPriority && len(e.G.Stack) == 0 && len(e.pendingTriggers) == 0 && d.Player == 0 {
			return
		}
		switch d.Kind {
		case decision.KPriority:
			loopPick(t, e, func(o decision.Option) bool { return o.Kind == "pass" })
		case decision.KTriggerOrder:
			var xs []int
			for _, o := range d.Options {
				xs = append(xs, o.Index)
			}
			submitChoices(t, e, xs...)
		case decision.KTarget:
			id := burnt
			if picks > 0 {
				id = saw
			}
			picks++
			loopPick(t, e, func(o decision.Option) bool { return o.Obj == id })
		default:
			t.Fatalf("unexpected monk decision %s %s", d.Kind, d.Prompt)
		}
	}
	t.Fatal("monk return budget exceeded")
}
func TestLoopPrototypeMonk(t *testing.T) {
	e, ids := loopBoard(t, []string{"Pinnacle Monk", "Burnt Offering", "Saw in Half"}, []state.Zone{state.ZBattlefield, state.ZGraveyard, state.ZHand})
	// Resolve the setup Monk's entry (it returns Offering); put Offering back
	// once for the bootstrap Saw. None of these fixture moves are iterations.
	loopMonkReturns(t, e, ids[1], ids[2])
	e.emit(events.Event{Kind: events.MoveZone, Obj: ids[1], From: state.ZHand, To: state.ZGraveyard})
	addMana(t, e, 0, "BBB")
	loopAction(t, e, ids[2], "cast")
	loopPick(t, e, func(o decision.Option) bool { return o.Obj == ids[0] })
	loopMonkReturns(t, e, ids[1], ids[2])
	// The bootstrap consumes 3 mana. One seed B starts the steady-state cycle.
	addMana(t, e, 0, "B")
	start := loopBegin(t, e)
	clock := time.Now()
	for i := 0; i < 20; i++ {
		var monks []state.ObjID
		for _, id := range e.G.Zone(state.ZBattlefield, 0) {
			if e.G.Obj(id).Face().Name == "Pinnacle Monk" {
				monks = append(monks, id)
			}
		}
		if len(monks) != 2 {
			loopRecord(t, e, "monk-blocked", i, start)
			loopBlocked(t, fmt.Sprintf("Saw made %d live Monks, want 2", len(monks)))
			return
		}
		loopAction(t, e, ids[1], "cast")
		loopDrain(t, e, monks[0], 0, 0)
		loopAction(t, e, ids[2], "cast")
		loopPick(t, e, func(o decision.Option) bool { return o.Obj == monks[1] })
		loopMonkReturns(t, e, ids[1], ids[2])
		if e.G.Players[0].Pool.Total() != int32(i+2) {
			t.Fatalf("monk net mana at iteration %d = %d want %d", i+1, e.G.Players[0].Pool.Total(), i+2)
		}
	}
	loopRecord(t, e, "monk-20", 20, start)
	t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/20000)
}

func TestLoopPrototypeSephiroth(t *testing.T) {
	e, ids := loopBoard(t, []string{"Sephiroth, Fabled SOLDIER", "Phyrexian Altar", "Forsaken Miner"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield})
	loopDrain(t, e, 0, 0, 1)
	start := loopBegin(t, e)
	for i := 0; i < 10; i++ {
		loopAction(t, e, ids[1], "activate")
		loopDrain(t, e, ids[2], 0, 1)
		if i == 3 && e.G.Obj(ids[0]).Face().Name != "Sephiroth, One-Winged Angel" {
			loopRecord(t, e, "sephiroth-blocked", i+1, start)
			loopBlocked(t, "Sephiroth did not transform after fourth drain resolution")
			return
		}
		if e.G.Obj(ids[2]).Zone != state.ZBattlefield || e.G.Players[1].Life != int32(19-i) {
			loopRecord(t, e, "sephiroth-blocked", i, start)
			loopBlocked(t, fmt.Sprintf("Sephiroth iteration %d: Miner=%s opponent life=%d (expected %d)", i+1, e.G.Obj(ids[2]).Zone, e.G.Players[1].Life, 19-i))
			return
		}
	}
	loopRecord(t, e, "sephiroth-10", 10, start)
}

func TestLoopPrototypeSoultrader(t *testing.T) {
	for _, variant := range []string{"rakdos", "sephiroth", "both"} {
		t.Run(variant, func(t *testing.T) {
			names := []string{"Rakdos, the Muscle", "Warren Soultrader", "Forsaken Miner"}
			zones := []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield}
			if variant == "sephiroth" {
				names[0] = "Sephiroth, Fabled SOLDIER"
			}
			if variant == "both" {
				names = append(names, "Sephiroth, Fabled SOLDIER")
				zones = append(zones, state.ZBattlefield)
			}
			e, ids := loopBoard(t, names, zones)
			loopDrain(t, e, 0, 0, 1)
			// Seed B bridges the crime trigger resolving before the Treasure ability.
			addMana(t, e, 0, "B")
			start := loopBegin(t, e)
			for i := 0; i < 10; i++ {
				loopAction(t, e, ids[1], "ability")
				loopDrain(t, e, ids[2], 0, 1)
				var treasure state.ObjID
				for _, id := range e.G.Zone(state.ZBattlefield, 0) {
					if e.G.Obj(id).Face().Name == "Treasure Token" {
						treasure = id
						break
					}
				}
				if treasure == 0 {
					t.Fatal("no Soultrader Treasure")
				}
				loopAction(t, e, treasure, "activate")
				loopDrain(t, e, 0, 0, 1)
				life, opp := int32(19-i), int32(20)
				if variant != "rakdos" {
					life = 20
					opp = int32(19 - i)
				}
				if e.G.Obj(ids[2]).Zone != state.ZBattlefield || e.G.Players[0].Life != life || e.G.Players[1].Life != opp || e.G.Players[0].Pool.Total() != 1 {
					t.Fatal("Soultrader cycle mismatch")
				}
			}
			loopRecord(t, e, "soultrader-"+variant+"-10", 10, start)
		})
	}
}

// loopProfileN returns the iteration counts TestLoopPrototypeMiner runs. The
// default is the historical fixed set; GORGE_LOOP_MINER_N overrides it with a
// comma-separated list so a profile run can target one N.
func loopProfileN() []int {
	if s := os.Getenv("GORGE_LOOP_MINER_N"); s != "" {
		var out []int
		for _, f := range strings.Split(s, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(f))
			if err != nil || n <= 0 {
				panic("bad GORGE_LOOP_MINER_N: " + s)
			}
			out = append(out, n)
		}
		return out
	}
	return []int{1, 20, 100}
}

// loopProfileDisableVerify turns off the rules test binary's verification-only
// checks for one profile run and returns a restore func. The verify modes only
// add re-derivations and panics; they never change the engine's emitted events
// or answers, so turning them off for a profile yields the production cost
// shape instead of the instrumented one. Gated by env, so the default run is
// byte-for-byte unchanged.
//
// The restore MUST be registered with t.Cleanup rather than deferred:
// loopBegin registers a cleanup that replays every recorded intent on a
// clone, and t.Cleanup runs LIFO (the last registered runs first). A deferred
// restore would run before that replay and re-enable verify for it, which is
// most of the loop's second pass and would swamp the profile. Registering the
// restore first keeps verify off through loopBegin's replay too.
func loopProfileDisableVerify() func() {
	if os.Getenv("GORGE_LOOP_NO_VERIFY") != "1" {
		return func() {}
	}
	ptrs := []*bool{
		&layerInertVerify, &layer4PrecheckVerify, &derivedMemoVerify,
		&sacrificeCardnameVerify, &pricedCandidatesVerify, &castsOnlyWalkVerify,
		&potentialMembersVerify, &walkCacheVerify, &trigZoneSkipVerify,
		&manaPayFastVerify, &activeSummaryVerify, &manaSAFactsVerify,
		&faceScanVerify, &livelockCandVerify, &priorityFlowVerify,
		&replZoneSkipVerify, &sbaQuietVerify, &provenanceGateVerify,
		&specDerivedVerify, &staticZoneSkipVerify,
	}
	old := make([]bool, len(ptrs))
	for i, p := range ptrs {
		old[i] = *p
		*p = false
	}
	return func() {
		for i, p := range ptrs {
			*p = old[i]
		}
	}
}

func TestLoopPrototypeMiner(t *testing.T) {
	for _, n := range loopProfileN() {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Cleanup(loopProfileDisableVerify())
			var previous *events.Log
			for run := 0; run < 2; run++ {
				e, ids := loopBoard(t, []string{"Rakdos, the Muscle", "Phyrexian Altar", "Forsaken Miner"}, []state.Zone{state.ZBattlefield, state.ZBattlefield, state.ZBattlefield})
				loopDrain(t, e, 0, 0, 1)
				start := loopBegin(t, e)
				clock := time.Now()
				library := len(e.G.Zone(state.ZLibrary, 1))
				for i := 0; i < n; i++ {
					loopAction(t, e, ids[1], "activate")
					loopDrain(t, e, ids[2], 0, 1)
					if e.G.Obj(ids[2]).Zone != state.ZBattlefield {
						t.Fatalf("iteration %d: Miner in %v", i+1, e.G.Obj(ids[2]).Zone)
					}
					if e.G.Players[0].Pool.Total() != 0 || len(e.G.Zone(state.ZLibrary, 1)) != max(0, library-i-1) || e.G.Players[1].Life != 20 || e.G.Over {
						t.Fatal("Miner cycle net resources wrong")
					}
				}
				if run == 0 {
					loopRecord(t, e, "miner-"+fmt.Sprint(n), n, start)
					t.Logf("ms/iteration=%.3f", float64(time.Since(clock).Microseconds())/1000/float64(n))
					previous = e.L
				} else if previous.Head() != e.L.Head() || !reflect.DeepEqual(previous.Events, e.L.Events) {
					t.Fatal("deterministic rerun diverged")
				}
			}
		})
	}
}
