package rules

// The Karn, the Great Creator concern (task sideboards, fix round 2): the
// -2's real corpus ability is a compound DIRECT Origin$ — `Origin$
// Sideboard,Exile`, no OriginAlternative$, no object selector — so it must
// route through the hidden-origin search like Burning Wish's exact
// `Origin$ Sideboard` does: the chooser picks from the union of the owner's
// private sideboard and their public exile in one option list. Every real
// card here is compiled from the corpus; no Forge script text is committed.

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

func TestChangeZoneKarnCompoundSideboardExile(t *testing.T) {
	t.Parallel()
	reg := searchTestRegistry(t)
	karn := searchCorpusCard(t, reg, "Karn, the Great Creator")
	cage := searchCorpusCard(t, reg, "Grafdigger's Cage")
	chalice := searchCorpusCard(t, reg, "Chalice of the Void")
	e, cfg := searchEngine(t, reg, karn.Faces[0].Name, chalice.Faces[0].Name)
	cfg.Sideboards = [][]*cards.Card{{cage}, nil}
	e = New(cfg)
	e.Advance()
	toMain1(t, e)
	testutil.CheckInvariants(t, e.G, e.Pending(), "genesis with sideboard")

	// Precondition: the sideboard card really sits in the owner's private
	// sideboard zone at genesis.
	var cageID state.ObjID
	for _, id := range e.G.Zone(state.ZSideboard, 0) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == cage.Faces[0].Name {
			cageID = id
		}
	}
	if cageID == 0 {
		t.Fatal("the sideboard does not hold the cage at genesis")
	}

	start := len(e.L.Events)
	karnID := searchMoveByName(t, e, karn.Faces[0].Name, state.ZBattlefield)
	// The Exile leg: put an artifact owned by seat 0 into exile so the
	// compound origin's union is observable in one option list.
	chaliceID := searchMoveByName(t, e, chalice.Faces[0].Name, state.ZHand)
	e.emit(events.Event{Kind: events.MoveZone, Obj: chaliceID, From: state.ZHand, To: state.ZExile})
	e.pending = nil
	e.priorityRound()

	idx := changeZoneAbilityIndexBy(t, e, karnID, "Sideboard,Exile")
	opt := abilityOption(t, e, karnID, idx)
	submitChoices(t, e, opt.Index)
	d := passUntilNonPriority(t, e, 20)
	if d == nil || d.Kind != decision.KChoose || d.ResumeKind != "search" {
		t.Fatalf("Karn -2 pending = %+v, want a search KChoose", d)
	}
	if len(d.Options) != 2 {
		t.Fatalf("Karn -2 offered %d options, want the union of the owner's sideboard and exile: %+v",
			len(d.Options), d.Options)
	}
	byName := map[string]state.ObjID{}
	for _, o := range d.Options {
		byName[o.Label] = o.Obj
	}
	if byName[cage.Faces[0].Name] != cageID || byName[chalice.Faces[0].Name] != chaliceID {
		t.Fatalf("Karn -2 options do not pair both origins: %+v (cage %d, chalice %d)",
			d.Options, cageID, chaliceID)
	}
	// Take the SIDEBOARD card: the private-origin half is what the old
	// object path could never offer.
	for _, o := range d.Options {
		if o.Obj == cageID {
			submitChoices(t, e, o.Index)
		}
	}
	passUntilStackEmpty(t, e, 20)
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Note &&
			(strings.Contains(ev.Text, "unrecognised ChangeZone Origin") ||
				strings.Contains(ev.Text, "no engine host")) {
			t.Fatalf("the compound sideboard/exile search degraded: %+v", ev)
		}
	}
	revealed := false
	for _, ev := range e.L.Events[start:] {
		if ev.Kind == events.Note && len(ev.IDs) == 1 && ev.IDs[0] == cageID {
			revealed = true
		}
	}
	if !revealed {
		t.Fatal("Reveal$ True did not publicly reveal the moved sideboard card")
	}
	if o := e.G.Obj(cageID); o == nil || o.Zone != state.ZHand || o.EnteredFrom != state.ZSideboard {
		t.Fatalf("wished artifact = %+v, want the sideboard card in hand", o)
	}
	testutil.CheckInvariants(t, e.G, e.Pending(), "after Karn -2 wish")
	replayCheck(t, e, cfg)
}
