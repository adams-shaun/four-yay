package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// Geth's Summons declares the sub-link's Graveyard target zone explicitly,
// unlike Cathartic Parting's inferred Graveyard zone. Both are cast targets.
func TestGethsSummonsExplicitGraveyardSubIsAnnounceable(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	var root, sub *cards.SA
	for _, c := range reg.Cards {
		if len(c.Faces) > 0 && c.Faces[0] != nil && c.Faces[0].Name == "Geth's Summons" {
			for _, ability := range c.Faces[0].Abilities {
				if ability != nil && ability.Sub != nil && ability.Sub.API == "ChangeZone" {
					root, sub = ability, ability.Sub
					break
				}
			}
		}
	}
	if sub == nil {
		t.Fatal("precondition: Geth's Summons has a ChangeZone sub-link")
	}
	tp := effects.TargetsOf(sub)
	if !tp.Targeted() || tp.ZoneText != "Graveyard" || !effects.ChangeZoneOf(sub).OriginExactly(state.ZGraveyard) {
		t.Fatalf("precondition: expected explicit graveyard targets on graveyard-origin link: %+v", tp)
	}
	if zs := targetZones(sub); len(zs) != 1 || zs[0] != state.ZGraveyard {
		t.Fatalf("precondition: cast census must search only Graveyard, got %v", zs)
	}
	if asks := (&Engine{}).collectSubTargetPreAsks(root); len(asks) != 1 || asks[0] != sub {
		t.Fatalf("Geth's Summons cast-time chained asks = %v, want its graveyard sub-link", asks)
	}
}

func TestChangeZoneExplicitGraveyardAnnouncementRequiresMatchingObjectZone(t *testing.T) {
	for _, tc := range []struct {
		name, origin, zone, spec string
		want                     bool
	}{
		{"matching", "Graveyard", "Graveyard", "Card.YouOwn", true},
		{"wrong zone", "Graveyard", "Battlefield", "Card.YouOwn", false},
		{"wrong origin", "Hand", "Graveyard", "Card.YouOwn", false},
		{"mixed zones", "Graveyard", "Graveyard,Battlefield", "Card.YouOwn", false},
		{"unknown zone", "Graveyard", "Unknown", "Card.YouOwn", false},
		{"mixed targets", "Graveyard", "Graveyard", "Player,Card", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sa := kr0SA(t, "DB$ ChangeZone | Origin$ "+tc.origin+" | Destination$ Library | TgtZone$ "+tc.zone+" | ValidTgts$ "+tc.spec)
			if got := castSubChangeZoneAnnounceable(sa); got != tc.want {
				t.Fatalf("castSubChangeZoneAnnounceable = %v, want %v", got, tc.want)
			}
		})
	}
}
