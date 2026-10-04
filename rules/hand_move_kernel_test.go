package rules

// Kernel-era restorations of the effects-package hand-origin ChangeZone
// tests the W3 legacy removal deleted (effects/hand_move_test.go). The
// legacy tests re-entered the resolution with Ctx.HandMove set by hand;
// here a real engine poses the hand_move (and hand_move_confirm) asks and
// the answers are submitted.

import (
	"testing"

	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	kr1HandBear = "Name:Bear\nManaCost:1 G\nTypes:Creature\nPT:2/2\nOracle:x\n"
	kr1Isle     = "Name:Isle\nTypes:Basic Land Island\nOracle:x\n"
)

// kr1HandBoard casts a sorcery whose body is the given ability line(s)
// with seat 0's hand holding exactly [Bear, Isle, Isle, Bear] (the legacy
// handAskFixture), and returns the engine, config, fixture id, the four
// hand ids and the first ask (nil when none).
func kr1HandBoard(t *testing.T, seed uint64, body string) (*Engine, Config, state.ObjID, []state.ObjID) {
	t.Helper()
	e, cfg, id := kr1New(t, seed, kr1Sorcery("HandMover", body), []string{kr1HandBear, kr1Isle, kr1Isle, kr1HandBear}, nil)
	addMana(t, e, 0, "B")
	kr1ClearHand(t, e, 0, id)
	ids := []state.ObjID{
		kr1Put(t, e, 0, "Bear", state.ZHand),
		kr1Put(t, e, 0, "Isle", state.ZHand),
		kr1Put(t, e, 0, "Isle", state.ZHand),
		kr1Put(t, e, 0, "Bear", state.ZHand),
	}
	return e, cfg, id, ids
}

// kr1NotesAfter returns the Note texts logged after mark.
func kr1NotesAfter(e *Engine, mark int) []string {
	var out []string
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note {
			out = append(out, ev.Text)
		}
	}
	return out
}

// TestKr1HandMoveChangeZoneMovesExactlyTheAnswer (was
// TestHandMoveChangeZoneReentryMovesExactlyTheAnswer): the Mandatory$ take
// offers only the two Isles; answering the SECOND moves exactly it.
func TestKr1HandMoveChangeZoneMovesExactlyTheAnswer(t *testing.T) {
	t.Parallel()
	e, cfg, id, ids := kr1HandBoard(t, 300, "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Mandatory$ True")
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 2 || d.Options[0].Obj != ids[1] || d.Options[1].Obj != ids[2] {
		t.Fatalf("hand ask = %+v, want the two Isles in hand order", d)
	}
	if p := kr1Pick(t, e, 1); p != nil {
		t.Fatalf("unexpected further ask %+v", p)
	}
	kr1Want(t, e, state.ZBattlefield, ids[2])
	kr1Want(t, e, state.ZHand, ids[0], ids[1], ids[3])
	replayCheck(t, e, cfg)
}

// TestKr1HandMoveChangeZoneExileCounters (was
// TestHandMoveChangeZoneExileAppliesTimeCounters,
// TestHandMoveChangeZoneExileDynamicCounterAmountIsLoud and
// TestHandMoveChangeZoneCounterlessDestinationStaysSilent): a hand move to
// exile carries WithCountersType$ TIME counters — a literal amount silently,
// an unresolvable X as the default 1 with the malformed-amount Note — while
// a counterless destination (graveyard) neither counts nor notes.
func TestKr1HandMoveChangeZoneExileCounters(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		name, dest, amount string
		zone               state.Zone
		want               int32
		note               string
	}{
		{"exile literal", "Exile", "3", state.ZExile, 3, ""},
		{"exile dynamic", "Exile", "X", state.ZExile, 1, "malformed WithCountersAmount X"},
		{"graveyard silent", "Graveyard", "X", state.ZGraveyard, 0, ""},
	} {
		tc := tc
		seed := uint64(301 + i)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Mandatory$ True: a markerless hand move now fails loud by design;
			// the counter contract is what is under test.
			e, cfg, id, ids := kr1HandBoard(t, seed, "A:SP$ ChangeZone | Origin$ Hand | Destination$ "+tc.dest+
				" | ChangeType$ Land | Mandatory$ True | WithCountersType$ TIME | WithCountersAmount$ "+tc.amount)
			mark := len(e.L.Events)
			d := kr1Cast(t, e, id)
			if d == nil || d.ResumeKind != "hand_move" {
				t.Fatalf("hand ask = %+v", d)
			}
			kr1Pick(t, e, kr1OptIndex(t, d, ids[1]))
			kr1Want(t, e, tc.zone, ids[1])
			if got := e.G.Obj(ids[1]).Counter("TIME"); got != tc.want {
				t.Fatalf("TIME counters = %d, want %d", got, tc.want)
			}
			sawCounter := false
			for _, ev := range e.L.Events[mark:] {
				if ev.Kind == events.CounterChange && ev.Obj == ids[1] {
					sawCounter = true
				}
			}
			if sawCounter != (tc.want > 0) {
				t.Fatalf("CounterChange logged = %v, want %v", sawCounter, tc.want > 0)
			}
			var counterNotes []string
			for _, n := range kr1NotesAfter(e, mark) {
				if kr1ContainsAny(n, "WithCounters", "malformed") {
					counterNotes = append(counterNotes, n)
				}
			}
			// The amount is read once per resolution: an ask answered in
			// place never re-emits the entry's loud Note, so a malformed
			// amount is reported exactly once.
			if tc.note == "" && len(counterNotes) != 0 || tc.note != "" && len(counterNotes) != 1 {
				t.Fatalf("counter Notes = %q, want exactly one %q", counterNotes, tc.note)
			}
			for _, n := range counterNotes {
				if n != tc.note {
					t.Fatalf("counter Note = %q, want %q", n, tc.note)
				}
			}
			replayCheck(t, e, cfg)
		})
	}
}

func kr1ContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// TestKr1HandMoveChangeZoneHonoursRememberChanged (was
// TestHandMoveChangeZoneHonoursRememberChanged): RememberChanged$ makes the
// moved card the resolution's Remembered (a Defined$ Remembered sub puts a
// counter on exactly it), and with no Tapped$ the land enters untapped.
func TestKr1HandMoveChangeZoneHonoursRememberChanged(t *testing.T) {
	t.Parallel()
	e, cfg, id, ids := kr1HandBoard(t, 304, "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Mandatory$ True | RememberChanged$ True | SubAbility$ DBMark\n"+
		"SVar:DBMark:DB$ PutCounter | Defined$ Remembered | CounterType$ P1P1 | CounterNum$ 1")
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "hand_move" {
		t.Fatalf("hand ask = %+v", d)
	}
	kr1Pick(t, e, kr1OptIndex(t, d, ids[2]))
	o := e.G.Obj(ids[2])
	if o.Zone != state.ZBattlefield || o.Tapped {
		t.Fatalf("moved land zone %s tapped %v, want an untapped permanent", o.Zone, o.Tapped)
	}
	if o.Counter("P1P1") != 1 || e.G.Obj(ids[1]).Counter("P1P1") != 0 {
		t.Fatal("the Remembered-reading sub did not mark exactly the moved land")
	}
	replayCheck(t, e, cfg)
}

// TestKr1HandMoveChangeZoneLibraryPlacement (was
// TestHandMoveChangeZonePutsBackOnTopInAnswerOrder,
// TestHandMoveChangeZoneLibraryPositionZeroIsTop and
// TestHandMoveChangeZoneLibraryPositionMinusOneIsBottom): a put-back to the
// library lands in the player's ANSWER order — on top by default (with a
// LibraryOrder placement) and for LibraryPosition$ 0, at the bottom for
// LibraryPosition$ -1 — and the unchosen cards stay in hand.
func TestKr1HandMoveChangeZoneLibraryPlacement(t *testing.T) {
	t.Parallel()
	for i, tc := range []struct {
		name, params string
		bottom       bool
	}{
		{"default top reorder", "ChangeNum$ 2 | Mandatory$ True | Reorder$ True", false},
		{"position zero", "ChangeType$ Card | ChangeNum$ 2 | LibraryPosition$ 0 | Mandatory$ True", false},
		{"position minus one", "LibraryPosition$ -1 | ChangeNum$ 2 | Mandatory$ True", true},
	} {
		tc := tc
		seed := uint64(305 + i)
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e, cfg, id, ids := kr1HandBoard(t, seed, "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | "+tc.params)
			d := kr1Cast(t, e, id)
			if d == nil || d.ResumeKind != "hand_move" || len(d.Options) != 4 {
				t.Fatalf("hand ask = %+v, want the whole hand offered", d)
			}
			mark := len(e.L.Events)
			kr1Pick(t, e, kr1OptIndex(t, d, ids[2]), kr1OptIndex(t, d, ids[1]))
			lib := e.G.Zone(state.ZLibrary, 0)
			got := lib[:2]
			if tc.bottom {
				got = lib[len(lib)-2:]
			}
			if got[0] != ids[2] || got[1] != ids[1] {
				t.Fatalf("library %s = %v, want [%d %d] in answer order", map[bool]string{false: "top", true: "bottom"}[tc.bottom], got, ids[2], ids[1])
			}
			if !tc.bottom {
				saw := false
				for _, ev := range e.L.Events[mark:] {
					if ev.Kind == events.LibraryOrder && ev.Player == 0 {
						saw = true
					}
				}
				if !saw {
					t.Fatal("no LibraryOrder placement event")
				}
			}
			kr1Want(t, e, state.ZHand, ids[0], ids[3])
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1HandMoveChangeZoneOptionalTakeAsksWithMinZero (was
// TestHandMoveChangeZoneOptionalTakeAsksWithMinZero): Optional$ poses the
// yes/no confirmation first; the accepted pick is Min 0 / Max 1 and an
// empty answer moves nothing, while Mandatory$ True keeps the pick's Min 1.
func TestKr1HandMoveChangeZoneOptionalTakeAsksWithMinZero(t *testing.T) {
	t.Parallel()
	for i, mandatory := range []bool{false, true} {
		mandatory := mandatory
		seed := uint64(308 + i)
		name := "optional"
		line := "A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | Optional$ You"
		if mandatory {
			name = "mandatory"
			line += " | Mandatory$ True"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e, cfg, id, ids := kr1HandBoard(t, seed, line)
			d := kr1Cast(t, e, id)
			if d == nil || d.ResumeKind != "hand_move_confirm" || d.Min != 1 || d.Max != 1 ||
				len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
				t.Fatalf("first ask = %+v, want the hand_move_confirm yes/no gate", d)
			}
			d = kr1Pick(t, e, 0)
			if d == nil || d.ResumeKind != "hand_move" || d.Max != 1 {
				t.Fatalf("accepted pick = %+v, want a Max 1 hand_move", d)
			}
			if mandatory {
				if d.Min != 1 {
					t.Fatalf("Mandatory$ accepted pick Min = %d, want 1", d.Min)
				}
				return
			}
			if d.Min != 0 {
				t.Fatalf("optional accepted pick Min = %d, want 0", d.Min)
			}
			kr1Pick(t, e)
			kr1Want(t, e, state.ZHand, ids...)
			replayCheck(t, e, cfg)
		})
	}
}

// TestKr1HandMoveChangeZoneShuffleParamShufflesTheLibrary (was
// TestHandMoveChangeZoneShuffleParamShufflesTheLibrary): Shuffle$ True on a
// put-back moves the card then shuffles, with no placement on top of it.
func TestKr1HandMoveChangeZoneShuffleParamShufflesTheLibrary(t *testing.T) {
	t.Parallel()
	e, cfg, id, ids := kr1HandBoard(t, 310, "A:SP$ ChangeZone | Origin$ Hand | Destination$ Library | ChangeType$ Card | Mandatory$ True | Shuffle$ True | RememberChanged$ True")
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "hand_move" {
		t.Fatalf("hand ask = %+v", d)
	}
	mark := len(e.L.Events)
	kr1Pick(t, e, kr1OptIndex(t, d, ids[1]))
	kr1Want(t, e, state.ZLibrary, ids[1])
	shuffle, order := false, false
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Shuffle && ev.Player == 0 {
			shuffle = true
		}
		if ev.Kind == events.LibraryOrder {
			order = true
		}
	}
	if !shuffle || order {
		t.Fatalf("shuffle %v placement %v, want a shuffle and no placement", shuffle, order)
	}
	replayCheck(t, e, cfg)
}

// TestKr1HERBIEScoutUnitHandMoveEntersTapped (was
// TestHERBIEScoutUnitHandMoveEntersTapped) runs the REAL H.E.R.B.I.E. Scout
// Unit: its ETB draws, then its hand mover (Tapped$ True) puts the chosen
// land onto the battlefield TAPPED with exactly one entry Tap, no retired
// "not implemented" Note, and not attacking.
func TestKr1HERBIEScoutUnitHandMoveEntersTapped(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	herbie, ok := reg.Lookup("H.E.R.B.I.E. Scout Unit")
	if !ok {
		t.Fatal("corpus has no H.E.R.B.I.E. Scout Unit")
	}
	e, cfg, id := kr1Build(t, 311, herbie, kr1Cards(t, []string{kr1Forest, kr1BearOne}), nil)
	addMana(t, e, 0, "CCCC")
	kr1ClearHand(t, e, 0, id)
	land := kr1Put(t, e, 0, "Forest", state.ZHand)
	kr1Top(t, e, 0, "Bear One") // the ETB draw takes a nonland
	d := kr1Cast(t, e, id)
	if d != nil && d.ResumeKind == "hand_move_confirm" {
		d = kr1Pick(t, e, kr1Opt(t, d, "yes"))
	}
	if d == nil || d.ResumeKind != "hand_move" {
		t.Fatalf("hand ask = %+v, want the land pick", d)
	}
	mark := len(e.L.Events)
	if p := kr1Pick(t, e, kr1OptIndex(t, d, land)); p != nil {
		t.Fatalf("unexpected further ask %+v", p)
	}
	o := e.G.Obj(land)
	if o.Zone != state.ZBattlefield || !o.Tapped || o.IsAttacking {
		t.Fatalf("land zone %s tapped %v attacking %v, want a tapped non-attacking permanent", o.Zone, o.Tapped, o.IsAttacking)
	}
	taps := 0
	for _, ev := range e.L.Events[mark:] {
		if ev.Kind == events.Note && kr1ContainsAny(ev.Text, "Tapped$ True on a hand ChangeZone is not implemented") {
			t.Fatalf("the retired hand Tapped$ fallback Note is back: %+v", ev)
		}
		if ev.Kind == events.Tap && ev.Obj == land {
			taps++
		}
	}
	if taps != 1 {
		t.Fatalf("entry Taps for the land = %d, want 1", taps)
	}
	replayCheck(t, e, cfg)
}

// TestKr1KastralMixedHandOriginOffersBothZones (was
// TestKastralMixedHandOriginOffersBothZones, Kastral's DBChangeZone shape):
// Optional$ You asks the search_confirm yes/no first; then ONE search
// offers the Bird in hand and the Bird in the graveyard, nothing moving
// before the answer; the answered graveyard Bird enters with its finality
// counter.
func TestKr1KastralMixedHandOriginOffersBothZones(t *testing.T) {
	t.Parallel()
	crow := "Name:Crow\nManaCost:U\nTypes:Creature Bird\nPT:1/2\nOracle:x\n"
	e, cfg, id := kr1New(t, 312, kr1Sorcery("KastralFx",
		"A:SP$ ChangeZone | Origin$ Hand,Graveyard | Destination$ Battlefield | ChangeType$ Creature.Bird+YouOwn | WithCountersType$ FINALITY | Optional$ You"),
		[]string{crow, crow}, nil)
	addMana(t, e, 0, "B")
	kr1ClearHand(t, e, 0, id)
	handBird := kr1Put(t, e, 0, "Crow", state.ZHand)
	graveBird := kr1Put(t, e, 0, "Crow", state.ZGraveyard)
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "search_confirm" || len(d.Options) != 2 || d.Options[0].Kind != "yes" || d.Options[1].Kind != "no" {
		t.Fatalf("first ask = %+v, want the Optional$ You yes/no gate", d)
	}
	d = kr1Pick(t, e, 0)
	if d == nil || d.ResumeKind != "search" {
		t.Fatalf("mixed-origin search = %+v", d)
	}
	kr1OptIndex(t, d, handBird)
	gi := kr1OptIndex(t, d, graveBird)
	kr1Want(t, e, state.ZHand, handBird)
	kr1Want(t, e, state.ZGraveyard, graveBird)
	kr1Pick(t, e, gi)
	kr1Want(t, e, state.ZBattlefield, graveBird)
	kr1Want(t, e, state.ZHand, handBird)
	if e.G.Obj(graveBird).Counter("FINALITY") != 1 {
		t.Fatal("the returned Bird has no finality counter")
	}
	replayCheck(t, e, cfg)
}

// kr1OwnersBoard is the legacy ownersFixture: seat 0's hand [Bear, Isle,
// Isle], seat 1's [Isle, Bear, Isle], under a Kynaios-shaped body.
func kr1OwnersBoard(t *testing.T, seed uint64, body string) (*Engine, Config, state.ObjID, []state.ObjID, []state.ObjID) {
	t.Helper()
	e, cfg, id := kr1New(t, seed, kr1Sorcery("Kynaios", body),
		[]string{kr1HandBear, kr1Isle, kr1Isle}, []string{kr1Isle, kr1HandBear, kr1Isle})
	addMana(t, e, 0, "B")
	kr1ClearHand(t, e, 0, id)
	kr1ClearHand(t, e, 1, 0)
	hand0 := []state.ObjID{kr1Put(t, e, 0, "Bear", state.ZHand), kr1Put(t, e, 0, "Isle", state.ZHand), kr1Put(t, e, 0, "Isle", state.ZHand)}
	hand1 := []state.ObjID{kr1Put(t, e, 1, "Isle", state.ZHand), kr1Put(t, e, 1, "Bear", state.ZHand), kr1Put(t, e, 1, "Isle", state.ZHand)}
	return e, cfg, id, hand0, hand1
}

// TestKr1HandMoveOwnersChainsOneAskPerPlayer (was
// TestHandMoveOwnersChainsOneAskPerPlayer and
// TestHandMoveOwnersSkipAnsweredOwnersOnReentry, Kynaios and Tiro's shape):
// each owner in turn gets its own confirmation then its own Min 0 / Max 1
// pick over only its own Isles; the answered lands move, each exactly once,
// and the Bears stay.
func TestKr1HandMoveOwnersChainsOneAskPerPlayer(t *testing.T) {
	t.Parallel()
	e, cfg, id, hand0, hand1 := kr1OwnersBoard(t, 313,
		"A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ Player | ChangeNum$ 1 | RememberChanged$ True | Optional$ True")
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 0 || len(d.Options) != 2 || d.Options[0].Kind != "yes" {
		t.Fatalf("first ask = %+v, want owner 0's confirmation", d)
	}
	d = kr1Pick(t, e, 0)
	if d == nil || d.ResumeKind != "hand_move" || d.Player != 0 || d.Min != 0 || d.Max != 1 ||
		len(d.Options) != 2 || d.Options[0].Obj != hand0[1] || d.Options[1].Obj != hand0[2] {
		t.Fatalf("owner 0 pick = %+v, want Min 0 / Max 1 over owner 0's two Isles", d)
	}
	d = kr1Pick(t, e, 1)
	kr1Want(t, e, state.ZBattlefield, hand0[2])
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 1 {
		t.Fatalf("second ask = %+v, want owner 1's confirmation", d)
	}
	d = kr1Pick(t, e, kr1Opt(t, d, "yes"))
	if d == nil || d.ResumeKind != "hand_move" || d.Player != 1 ||
		len(d.Options) != 2 || d.Options[0].Obj != hand1[0] || d.Options[1].Obj != hand1[2] {
		t.Fatalf("owner 1 pick = %+v, want owner 1's two Isles in hand order", d)
	}
	if p := kr1Pick(t, e, 1); p != nil {
		t.Fatalf("a third decision was posed: %+v", p)
	}
	kr1Want(t, e, state.ZBattlefield, hand0[2], hand1[2])
	kr1Want(t, e, state.ZHand, hand0[0], hand0[1], hand1[0], hand1[1])
	for _, c := range []state.ObjID{hand0[2], hand1[2]} {
		if n := countMoves(e.L.Events, c, state.ZBattlefield); n != 1 {
			t.Fatalf("land %d moved to the battlefield %d times, want exactly once", c, n)
		}
	}
	replayCheck(t, e, cfg)
}

// TestKr1HandMoveOwnersOptionalSingleEligibleCanDecline (was
// TestHandMoveOwnersOptionalSingleEligibleCanDecline): declining owner 0's
// confirmation poses no pick; owner 1, whose pool is empty, still gets its
// own confirmation, and accepting it completes with no pick and no move.
func TestKr1HandMoveOwnersOptionalSingleEligibleCanDecline(t *testing.T) {
	t.Parallel()
	e, cfg, id, hand0, hand1 := kr1OwnersBoard(t, 314,
		"A:SP$ ChangeZone | Origin$ Hand | Destination$ Battlefield | ChangeType$ Land | DefinedPlayer$ Player | ChangeNum$ 1 | Optional$ True")
	// Exactly one Isle for owner 0, no land for owner 1.
	e.emit(events.Event{Kind: events.MoveZone, Obj: hand0[0], From: state.ZHand, To: state.ZLibrary, Player: 0})
	e.emit(events.Event{Kind: events.MoveZone, Obj: hand0[2], From: state.ZHand, To: state.ZLibrary, Player: 0})
	e.emit(events.Event{Kind: events.MoveZone, Obj: hand1[0], From: state.ZHand, To: state.ZLibrary, Player: 1})
	e.emit(events.Event{Kind: events.MoveZone, Obj: hand1[2], From: state.ZHand, To: state.ZLibrary, Player: 1})
	d := kr1Cast(t, e, id)
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 0 {
		t.Fatalf("first ask = %+v, want owner 0's confirmation", d)
	}
	d = kr1Pick(t, e, kr1Opt(t, d, "no"))
	if d == nil || d.ResumeKind != "hand_move_confirm" || d.Player != 1 {
		t.Fatalf("owner 1 confirmation = %+v, want it despite the empty pool", d)
	}
	if p := kr1Pick(t, e, kr1Opt(t, d, "yes")); p != nil {
		t.Fatalf("an accepted empty-pool fetch posed a pick: %+v", p)
	}
	kr1Want(t, e, state.ZHand, hand0[1], hand1[1])
	replayCheck(t, e, cfg)
}
