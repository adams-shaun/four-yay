package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/decision"
	"github.com/adams-shaun/gorge/state"
)

const targetMinChoiceCensusSrc = "Name:Target Census\nManaCost:1 W\nTypes:Instant\n" +
	"A:SP$ Draw | Defined$ You | ValidTgts$ Creature.YouCtrl | TargetMin$ 1 | " +
	"Choices$ AnyColor | NumCards$ 1 | Oracle:x\n"

const charmTargetCensusSrc = "Name:Charm Census\nManaCost:1 W\nTypes:Instant\n" +
	"A:SP$ Charm | CharmNum$ 1 | Choices$ TargetMode\n" +
	"SVar:TargetMode:DB$ Draw | Defined$ You | ValidTgts$ Creature.YouCtrl | " +
	"TargetMin$ 1 | NumCards$ 1 | Oracle:x\n"

func TestCastOfferCensusRejectsMandatoryTargetlessChoices(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 6012, targetMinChoiceCensusSrc)
	addMana(t, e, 0, "1W")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil {
		t.Fatalf("precondition: target census spell is not a face-up hand card: %+v", o)
	}
	sa := o.Face().SpellAbility()
	if sa == nil || sa.Params["TargetMin"] != "1" || sa.Params["Choices"] == "" {
		t.Fatalf("precondition: fixture lost TargetMin/Choices shape: %+v", sa)
	}
	if got := len(e.legalTargetCandidates(0, id, id, sa)); got != 0 {
		t.Fatalf("precondition: target census found %d legal creatures, want none", got)
	}
	if castOffered(e, id) {
		t.Fatal("TargetMin$ 1 spell with Choices$ was offered without a legal target")
	}
}

func TestCastOfferCensusKeepsMandatoryTargetWithCandidate(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 6013, targetMinChoiceCensusSrc, testBearSrc)
	bear := putCreature(t, e, 0, testBearSrc)
	addMana(t, e, 0, "1W")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand {
		t.Fatalf("precondition: target census spell is not in hand: %+v", o)
	}
	sa := o.Face().SpellAbility()
	candidates := e.legalTargetCandidates(0, id, id, sa)
	if len(candidates) != 1 || candidates[0].obj != bear {
		t.Fatalf("precondition: legal target census = %+v, want only Bear %d", candidates, bear)
	}
	if !castOffered(e, id) {
		t.Fatal("mandatory target spell was withheld despite its legal target")
	}
	var cast decision.Option
	for _, option := range e.Pending().Options {
		if option.Kind == "cast" && option.Obj == id {
			cast = option
			break
		}
	}
	if cast.Obj != id {
		t.Fatal("precondition: cast option disappeared before submission")
	}
	submitChoices(t, e, cast.Index)
	d := e.Pending()
	if d == nil || d.Kind != decision.KTarget || len(d.Options) != 1 || d.Options[0].Obj != bear {
		t.Fatalf("target ask = %+v, want the one legal Bear target", d)
	}
}

func TestCastOfferCensusRejectsTargetlessCharmAnnouncement(t *testing.T) {
	t.Parallel()
	e, _, id := newFixtureDeck(t, 6014, charmTargetCensusSrc)
	addMana(t, e, 0, "1W")
	o := e.G.Obj(id)
	if o == nil || o.Zone != state.ZHand || o.Face() == nil {
		t.Fatalf("precondition: Charm census spell is not a hand card: %+v", o)
	}
	sa := o.Face().SpellAbility()
	mode := cardsResolveSVar(o, "TargetMode")
	if sa == nil || sa.API != "Charm" || mode == nil || mode.Params["TargetMin"] != "1" {
		t.Fatalf("precondition: fixture lost Charm target mode: outer=%+v mode=%+v", sa, mode)
	}
	if got := len(e.legalTargetCandidates(0, id, id, mode)); got != 0 {
		t.Fatalf("precondition: Charm mode found %d legal targets, want none", got)
	}
	if castOffered(e, id) {
		t.Fatal("Charm was offered although its only announced mode has no legal target")
	}
}

// cardsResolveSVar keeps this test's setup assertion readable without exposing
// parser details in the test body.
func cardsResolveSVar(o *state.Object, name string) *cards.SA {
	if o == nil || o.Face() == nil {
		return nil
	}
	return cards.ResolveSVar(o.Face().SVars, name)
}
