package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Genesis Wave's shape: reveal the top three, put any number of permanent
// cards onto the battlefield, the rest into the graveyard. One of the picks
// has an as-enters choice (Heraldic Banner's "choose a color"), which
// suspends the Dig in the middle of its moves.
const waveSrc = "Name:WaveMe\nManaCost:G\nTypes:Sorcery\n" +
	"A:SP$ Dig | DigNum$ 3 | Reveal$ True | ChangeNum$ Any | ChangeValid$ Permanent | DestinationZone$ Battlefield | DestinationZone2$ Graveyard | SubAbility$ DBGain\n" +
	"SVar:DBGain:DB$ GainLife | Defined$ You | LifeAmount$ 3\nOracle:x\n"

const waveBanner = "Name:Bannerish\nManaCost:3\nTypes:Artifact\n" +
	"K:ETBReplacement:Other:ChooseColor\n" +
	"SVar:ChooseColor:DB$ ChooseColor | Defined$ You\nOracle:x\n"

const waveJunk = "Name:Junk\nManaCost:R\nTypes:Sorcery\nA:SP$ Draw | Defined$ You\nOracle:x\n"

// TestDigEntryChoiceStillFinishesTheDig pins the Dig's tail across an
// as-enters ask: every pick enters, and the unpicked card still goes to the
// graveyard rather than staying on top of the library.
func TestDigEntryChoiceStillFinishesTheDig(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 67, waveSrc, waveBanner, digCreature, waveJunk)
	addMana(t, e, 0, "G")
	named := map[string]state.ObjID{}
	for _, z := range []state.Zone{state.ZHand, state.ZLibrary} {
		for _, oid := range e.G.Zone(z, 0) {
			if n := e.G.Obj(oid).Face().Name; n == "Bannerish" || n == "Elk" || n == "Junk" {
				named[n] = oid
				if z == state.ZHand {
					e.emit(events.Event{Kind: events.MoveZone, Obj: oid, From: state.ZHand, To: state.ZLibrary, Player: 0})
				}
			}
		}
	}
	if len(named) != 3 {
		t.Fatalf("fixture cards found: %v", named)
	}
	want := []state.ObjID{named["Bannerish"], named["Elk"], named["Junk"]}
	for _, oid := range e.G.Zone(state.ZLibrary, 0) {
		if oid != want[0] && oid != want[1] && oid != want[2] {
			want = append(want, oid)
		}
	}
	e.emit(events.Event{Kind: events.LibraryOrder, Player: 0, IDs: want, Secret: true})

	d := castFixture(t, e, id, -1)
	if d == nil || d.Kind != decision.KChoose || len(d.Options) != 2 {
		t.Fatalf("expected the dig's pick over the two permanents, got %+v", d)
	}
	submitChoices(t, e, 0, 1)
	for i := 0; i < 20 && e.Pending() != nil && e.Pending().Kind != decision.KPriority; i++ {
		submitChoices(t, e, 0)
	}
	passUntilStackEmpty(t, e, 20)

	for _, n := range []string{"Bannerish", "Elk"} {
		if z := e.G.Obj(named[n]).Zone; z != state.ZBattlefield {
			t.Errorf("%s is in %s, want the battlefield", n, z)
		}
	}
	if z := e.G.Obj(named["Junk"]).Zone; z != state.ZGraveyard {
		t.Errorf("the unpicked Junk is in %s, want the graveyard", z)
	}
	if got := e.G.Players[0].Life; got != 23 {
		t.Errorf("life %d, want 23: the sub-ability chained after the dig did not run", got)
	}
	for _, ev := range e.L.Events {
		if ev.Kind == events.Note && ev.Text == "mid-resolution answer resumed with no sub-ability recorded" {
			t.Error("the dig's continuation was dropped across the as-enters ask")
		}
	}
}
