package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// Glorious Purpose's Dig/Play/remainder chain: the Dig-imprinted exiled spell
// must be offered to Play, and the cards not cast must still be available to
// the following Defined$ Imprinted ChangeZone.
const digImprintSource = "Name:Imprint Dig\nManaCost:G\nTypes:Sorcery\n" +
	"A:SP$ Dig | Defined$ You | DigNum$ 4 | ChangeNum$ All | DestinationZone$ Exile | Imprint$ True | SubAbility$ DBPlay\n" +
	"SVar:DBPlay:DB$ Play | Valid$ Card.nonLand+IsImprinted | ValidZone$ Exile | ValidSA$ Spell | Controller$ You | WithoutManaCost$ True | Optional$ True | Amount$ All | SubAbility$ DBRest\n" +
	"SVar:DBRest:DB$ ChangeZone | Defined$ Imprinted | Origin$ Exile | Destination$ Hand | SubAbility$ DBCleanup\n" +
	"SVar:DBCleanup:DB$ Cleanup | ClearImprinted$ True\n" +
	"Oracle:Exile the top four cards, cast a spell among them for free, then put the rest into your hand.\n"

const digImprintSpell = "Name:Imprint Spell\nManaCost:U\nTypes:Instant\n" +
	"A:SP$ Draw | NumCards$ 1 | Defined$ You\nOracle:Draw a card.\n"

func TestDigImprintPlaysExiledCardAndMovesRemainderToHand(t *testing.T) {
	t.Parallel()
	e, cfg, source := newFixtureDeck(t, 7901, digImprintSource, digImprintSpell)
	before := digReorder(t, e, "Imprint Spell", "Mountain", "Mountain", "Mountain")
	if len(before) < 4 {
		t.Fatalf("precondition: library has %d cards, need a four-card dig window", len(before))
	}
	spell := before[0]
	if e.G.Obj(spell).Face().Name != "Imprint Spell" {
		t.Fatalf("precondition: first library card is %q, want Imprint Spell", e.G.Obj(spell).Face().Name)
	}
	for _, id := range before[:4] {
		if e.G.Obj(id).Zone != state.ZLibrary {
			t.Fatalf("precondition: dig window object %d is in %s, want Library", id, e.G.Obj(id).Zone)
		}
	}

	d := digCast(t, e, source, "G", true)
	if d == nil || d.Kind != decision.KModes || d.ResumeKind != "play" {
		t.Fatalf("pending decision = %+v, want Play election over the imprinted exiled spell", d)
	}
	spellOption := -1
	for _, option := range d.Options {
		if option.Obj == spell {
			spellOption = option.Index
		}
	}
	if spellOption < 0 {
		t.Fatalf("Play options %+v do not include imprinted spell %d", d.Options, spell)
	}
	submitChoices(t, e, spellOption)
	// The spell is free-cast from exile and resolves before the continuation
	// sends the other three imprinted cards to hand.
	passUntilStackEmpty(t, e, 30)
	if o := e.G.Obj(spell); o == nil || o.Zone != state.ZGraveyard {
		t.Fatalf("imprinted spell zone = %v, want Graveyard after free cast and resolution", o)
	}
	for _, id := range before[1:4] {
		if o := e.G.Obj(id); o == nil || o.Zone != state.ZHand {
			t.Fatalf("uncast dug card %d zone = %v, want Hand", id, o)
		}
	}
	if o := e.G.Obj(source); o == nil || len(o.Imprinted) != 0 {
		t.Fatalf("source imprint after chained Cleanup = %v, want cleared", o.Imprinted)
	}
	imprinted := make(map[state.ObjID]bool)
	for _, ev := range e.L.Events {
		if ev.Kind == events.Imprint && ev.Obj == source && ev.Text == "" {
			for _, id := range ev.IDs {
				imprinted[id] = true
			}
		}
	}
	if len(imprinted) != 4 {
		t.Fatalf("Dig event-backed imprint IDs = %v, want all four dug cards", imprinted)
	}
	replayCheck(t, e, cfg)
}
