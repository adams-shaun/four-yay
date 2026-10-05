package rules

// The aggregate tap trigger modes, end to end on the real compiled corpus
// (task cli-20261005T075020Z-05241a06): Mode$ TapAll (MSH Rewrite History,
// LCI Deeproot Pilgrimage) and Mode$ UntapAll (LCI The Millennium Calendar)
// are the "whenever one or more ... become tapped/untapped" batch readings of
// the ordinary Tap/Untap events. ONE tapping/untapping action fires the
// trigger once however many permanents it touched; two separate actions fire
// it twice.
//
// No Forge script text is committed: the three trigger cards are fetched from
// the corpus registry, and the permanents they observe are authored fixtures.

import (
	"sort"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// tapAllMerfolkSrc is an authored NONTOKEN Merfolk creature, so Deeproot
// Pilgrimage's ValidCards$ Merfolk.!token+YouCtrl admits it. It is never a
// committed Forge .txt (the licensing rule), and it is a creature so the tap
// effects below really tap it.
const tapAllMerfolkSrc = "Name:Meriad Trader\nManaCost:1\nTypes:Creature Merfolk\nPT:1/1\nOracle:x\n"

// tapAllRockSrc is an authored ordinary permanent (not a creature), the
// tapped body The Millennium Calendar's untap step untaps. Being an artifact
// it is also not a Merfolk, so it never perturbs Deeproot's filter.
const tapAllRockSrc = "Name:Fixture Rock\nTypes:Artifact\nOracle:x\n"

// tapAllBearSrc is an authored creature that is NOT a Merfolk: Deeproot
// Pilgrimage's ValidCards$ Merfolk.!token+YouCtrl must reject it, so tapping
// it alone can never fire the trigger.
const tapAllBearSrc = "Name:Fixture Bear\nTypes:Creature Bear\nPT:2/2\nOracle:x\n"

// tapAllEngine builds a two-seat game with seat 0 holding want (a real corpus
// trigger card, placed by the caller) over a Mountain deck, seat 1 on a
// Mountain deck, and the corpus token scripts available (Deeproot mints a
// real Merfolk token). It returns the engine and the registry.
func tapAllEngine(t *testing.T, seed uint64, want *cards.Card) (*Engine, *cards.Registry) {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	e, _ := tokenReplGame(t, seed, want)
	return e, reg
}

// resolveTapEffect resolves one api:TapAll/api:UntapAll primitive against the
// engine, exactly as a card's DB$ TapAll/UntapAll would. The triggers the
// action queued stay in pendingTriggers, so the caller can measure the count
// before promoting them with priorityRound.
func resolveTapEffect(t *testing.T, e *Engine, api, spec string) {
	t.Helper()
	effects.Resolve(e, &effects.Ctx{Controller: 0},
		&cards.SA{Kind: "DB", API: api, Params: map[string]string{"ValidCards": spec}})
}

// tapPendingFor counts the pending (queued, not yet pushed) trigger instances
// whose source is src -- the direct measure of how many times one action
// fired a trigger.
func tapPendingFor(e *Engine, src state.ObjID) int {
	n := 0
	for _, pt := range e.pendingTriggers {
		if pt.Source == src {
			n++
		}
	}
	return n
}

// TestTapAllDeeprootPilgrimageFiresOncePerTappingAction is the leaf on LCI
// Deeproot Pilgrimage: "Whenever one or more nontoken Merfolk you control
// become tapped, create a 1/1 blue Merfolk creature token with hexproof."
//
//   - ONE api:TapAll over two untapped Merfolk is ONE tapping action, so it
//     creates exactly ONE token (a per-permanent Taps alias creates two).
//   - A SECOND, separate tapping action the same turn creates a SECOND token,
//     so the batch is an action boundary and not a once-per-turn latch.
func TestTapAllDeeprootPilgrimageFiresOncePerTappingAction(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deeproot := mustCorpusCard(t, reg, "Deeproot Pilgrimage")
	e, _ := tapAllEngine(t, 8801, deeproot)

	// PRECONDITION: Deeproot is on seat 0's battlefield, so its
	// TriggerZones$ Battlefield trigger is live for the scan.
	deeprootID := onBoardCard(t, e, 0, deeproot)
	if o := e.G.Obj(deeprootID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Deeproot Pilgrimage id %d zone = %v, want battlefield (vacuous setup)", deeprootID, o)
	}
	// Two distinct nontoken Merfolk, untapped, on seat 0's battlefield.
	m1 := onBoardCard(t, e, 0, card(t, tapAllMerfolkSrc))
	m2 := onBoardCard(t, e, 0, card(t, tapAllMerfolkSrc))
	if m1 == m2 {
		t.Fatalf("the two Merfolk ids are equal (%d): a batch claim would be vacuous", m1)
	}
	for _, id := range []state.ObjID{m1, m2} {
		o := e.G.Obj(id)
		if o == nil || o.Zone != state.ZBattlefield || o.Tapped {
			t.Fatalf("Merfolk id %d = %+v, want an untapped battlefield creature (vacuous setup)", id, o)
		}
	}
	if o := e.G.Obj(m1); o == nil || o.IsToken {
		t.Fatalf("the fixture Merfolk is a token (object %+v); Deeproot's !token filter would exclude it", o)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Merfolk tokens (vacuous setup)", got)
	}

	// ONE tapping action over both Merfolk: one trigger, one token.
	resolveTapEffect(t, e, "TapAll", "Creature.YouCtrl")
	if !e.G.Obj(m1).Tapped || !e.G.Obj(m2).Tapped {
		t.Fatalf("after api:TapAll m1.Tapped=%v m2.Tapped=%v, want both tapped (the action must really tap them)",
			e.G.Obj(m1).Tapped, e.G.Obj(m2).Tapped)
	}
	if n := tapPendingFor(e, deeprootID); n != 1 {
		t.Fatalf("one api:TapAll over two Merfolk queued %d Deeproot triggers, want exactly 1 (one action, one trigger)", n)
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 1 {
		t.Fatalf("after one two-permanent tap action seat 0 holds %d Merfolk tokens, want 1 (batch fires once)", got)
	}

	// Untap the two Merfolk (a raw Untap is not a TapAll action) so a second
	// real tapping action has something to tap.
	for _, id := range []state.ObjID{m1, m2} {
		e.emit(events.Event{Kind: events.Untap, Obj: id})
	}
	e.pending = nil

	// A SECOND tapping action: a second trigger and a second token.
	resolveTapEffect(t, e, "TapAll", "Creature.YouCtrl")
	if n := tapPendingFor(e, deeprootID); n != 1 {
		t.Fatalf("the second api:TapAll queued %d Deeproot triggers, want exactly 1 (the action boundary reset)", n)
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 2 {
		t.Fatalf("after two separate tap actions seat 0 holds %d Merfolk tokens, want 2 (two actions, two triggers)", got)
	}
}

// TestTapAllDeclareAttackersFiresOnce pins the CR 508.1f boundary: the taps
// the declare-attackers turn-based action applies are ONE tapping action, so
// Deeproot creates ONE token for a declaration of two Merfolk, not two.
func TestTapAllDeclareAttackersFiresOnce(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deeproot := mustCorpusCard(t, reg, "Deeproot Pilgrimage")
	e, _ := tapAllEngine(t, 8802, deeproot)

	deeprootID := onBoardCard(t, e, 0, deeproot)
	if o := e.G.Obj(deeprootID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Deeproot Pilgrimage id %d zone = %v, want battlefield (vacuous setup)", deeprootID, o)
	}
	m1 := onBoardCard(t, e, 0, card(t, tapAllMerfolkSrc))
	m2 := onBoardCard(t, e, 0, card(t, tapAllMerfolkSrc))
	if m1 == m2 {
		t.Fatalf("the two Merfolk ids are equal (%d): a batch claim would be vacuous", m1)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Merfolk tokens (vacuous setup)", got)
	}

	// The real declaration tail: it emits the DeclareAttackers events and taps
	// the non-Vigilance attackers as one turn-based action.
	e.finishAttackers([]decision.Option{{Obj: m1, Player: 1}, {Obj: m2, Player: 1}}, 0)
	if !e.G.Obj(m1).IsAttacking || !e.G.Obj(m2).IsAttacking {
		t.Fatalf("after the declaration m1.IsAttacking=%v m2.IsAttacking=%v, want both attacking (vacuous setup)",
			e.G.Obj(m1).IsAttacking, e.G.Obj(m2).IsAttacking)
	}
	if !e.G.Obj(m1).Tapped || !e.G.Obj(m2).Tapped {
		t.Fatalf("the declaration did not tap both Merfolk (m1=%v m2=%v)", e.G.Obj(m1).Tapped, e.G.Obj(m2).Tapped)
	}
	if n := tapPendingFor(e, deeprootID); n != 1 {
		t.Fatalf("declaring two Merfolk attacking queued %d Deeproot triggers, want exactly 1 (one turn-based action)", n)
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 1 {
		t.Fatalf("after the declaration seat 0 holds %d Merfolk tokens, want 1 (one action, one trigger)", got)
	}
}

// TestUntapAllMillenniumCalendarFiresOncePerUntapStep is the leaf on LCI The
// Millennium Calendar: "Whenever you untap one or more permanents during your
// untap step, put that many time counters on CARDNAME."
//
//   - ONE untap step that untaps three permanents is ONE untapping action, so
//     it queues exactly ONE trigger, whose TriggerCount$Amount is 3 and so
//     puts exactly THREE time counters (a per-permanent Untaps alias queues
//     three triggers and puts one counter each).
//   - A SECOND untap step queues a second trigger, so the batch is an action
//     boundary, not a once-per-turn latch.
func TestUntapAllMillenniumCalendarFiresOncePerUntapStep(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	calendar := mustCorpusCard(t, reg, "The Millennium Calendar")
	e, _ := tapAllEngine(t, 8803, calendar)

	calID := onBoardCard(t, e, 0, calendar)
	if o := e.G.Obj(calID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("The Millennium Calendar id %d zone = %v, want battlefield (vacuous setup)", calID, o)
	}
	r1 := onBoardCard(t, e, 0, card(t, tapAllRockSrc))
	r2 := onBoardCard(t, e, 0, card(t, tapAllRockSrc))
	if r1 == r2 {
		t.Fatalf("the two Rock ids are equal (%d): a batch claim would be vacuous", r1)
	}
	// Tap all three permanents so the untap step really untaps three (one
	// action), and the count is unambiguous.
	for _, id := range []state.ObjID{calID, r1, r2} {
		e.emit(events.Event{Kind: events.Tap, Obj: id})
		if o := e.G.Obj(id); o == nil || !o.Tapped {
			t.Fatalf("precondition: permanent %d did not tap", id)
		}
	}
	e.pending = nil

	// One untap step: three untaps, one trigger, three time counters.
	e.G.Step, e.G.Active = state.StepUntap, 0
	e.finishUntapStep(0)
	if e.G.Obj(calID).Tapped || e.G.Obj(r1).Tapped || e.G.Obj(r2).Tapped {
		t.Fatalf("the untap step left permanents tapped (cal=%v r1=%v r2=%v); the action must really untap them",
			e.G.Obj(calID).Tapped, e.G.Obj(r1).Tapped, e.G.Obj(r2).Tapped)
	}
	if n := tapPendingFor(e, calID); n != 1 {
		t.Fatalf("one untap step that untapped three permanents queued %d Calendar triggers, want exactly 1 (one action)", n)
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := timeCountersOn(e, calID); got != 3 {
		t.Fatalf("after one untap step of three permanents the Calendar has %d time counters, want 3 (TriggerCount$Amount = batch size)", got)
	}

	// Tap all three again and run a SECOND untap step: a second trigger and
	// three more counters.
	for _, id := range []state.ObjID{calID, r1, r2} {
		e.emit(events.Event{Kind: events.Tap, Obj: id})
	}
	e.pending = nil
	e.G.Step, e.G.Active = state.StepUntap, 0
	e.finishUntapStep(0)
	if n := tapPendingFor(e, calID); n != 1 {
		t.Fatalf("the second untap step queued %d Calendar triggers, want exactly 1 (the action boundary reset)", n)
	}
	e.priorityRound()
	drainMillTrigger(t, e, 40)
	if got := timeCountersOn(e, calID); got != 6 {
		t.Fatalf("after two untap steps the Calendar has %d time counters, want 6 (two actions, two triggers)", got)
	}
}

// TestTapAllDeeprootValidCardsFilterRejectsNonMerfolk pins the plural
// ValidCards$ filter: a tapping action that touches only non-Merfolk
// creatures must not fire Deeproot at all, so the batch's "fires once" claim
// in the test above is a real filter match and not an unconditional tap.
func TestTapAllDeeprootValidCardsFilterRejectsNonMerfolk(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	deeproot := mustCorpusCard(t, reg, "Deeproot Pilgrimage")
	e, _ := tapAllEngine(t, 8804, deeproot)

	deeprootID := onBoardCard(t, e, 0, deeproot)
	if o := e.G.Obj(deeprootID); o == nil || o.Zone != state.ZBattlefield {
		t.Fatalf("Deeproot Pilgrimage id %d zone = %v, want battlefield (vacuous setup)", deeprootID, o)
	}
	bear := onBoardCard(t, e, 0, card(t, tapAllBearSrc))
	if o := e.G.Obj(bear); o == nil || o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("Fixture Bear id %d = %+v, want an untapped battlefield creature (vacuous setup)", bear, o)
	}
	if got := countTokensNamedOnSeat(t, e, 0, "Merfolk Token"); got != 0 {
		t.Fatalf("seat 0 already holds %d Merfolk tokens (vacuous setup)", got)
	}

	resolveTapEffect(t, e, "TapAll", "Creature.YouCtrl")
	if !e.G.Obj(bear).Tapped {
		t.Fatal("the api:TapAll did not tap the only creature (the action must really tap)")
	}
	if n := tapPendingFor(e, deeprootID); n != 0 {
		t.Fatalf("tapping a non-Merfolk Bear queued %d Deeproot triggers, want 0 (ValidCards$ Merfolk must reject it)", n)
	}
}

// timeCountersOn counts the TIME counters on obj. It fails if obj is gone, so
// a removed carrier cannot make the count silently zero.
func timeCountersOn(e *Engine, obj state.ObjID) int32 {
	o := e.G.Obj(obj)
	if o == nil {
		return -1
	}
	for _, c := range o.Counters {
		if c.Kind == "TIME" {
			return c.N
		}
	}
	return 0
}

// TestAggregateTapModeCensus pins the corpus population of the two modes: at
// the FORGE_REF pin only three cards carry them -- Rewrite History and
// Deeproot Pilgrimage on TapAll, The Millennium Calendar on UntapAll. A new
// carrier fails here so its batch boundary is reviewed with it.
func TestAggregateTapModeCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string][]string{"TapAll": {}, "UntapAll": {}}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, tr := range f.Triggers {
				if tr.Mode == "TapAll" || tr.Mode == "UntapAll" {
					got[tr.Mode] = append(got[tr.Mode], f.Name)
				}
			}
		}
	}
	for mode := range got {
		sort.Strings(got[mode])
	}
	want := map[string][]string{
		"TapAll":   {"Deeproot Pilgrimage", "Rewrite History"},
		"UntapAll": {"The Millennium Calendar"},
	}
	for mode, names := range want {
		if len(got[mode]) != len(names) {
			t.Fatalf("corpus Mode$ %s carriers = %v, want %v (a new carrier must be reviewed)", mode, got[mode], names)
		}
		for i, name := range names {
			if got[mode][i] != name {
				t.Fatalf("corpus Mode$ %s carriers = %v, want %v", mode, got[mode], names)
			}
		}
	}
	// The cards must also be admitted as supported, or the census above would
	// pass while the coverage ratchet still called them unplayable.
	supported := effects.Supported()
	if !supported["trig:TapAll"] || !supported["trig:UntapAll"] {
		t.Fatalf("effects.Supported() missing trig:TapAll/trig:UntapAll: %v", supported)
	}
}
