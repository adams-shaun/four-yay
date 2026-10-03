package rules

// TestControlRedirectCensus measures the CR 722 "control another player"
// class corpus-wide. Every card in it reaches the engine through one of the
// two decision redirects -- api:ControlPlayer (folded into
// state.Game.ControlledBy, read by controlPlayerRedirect) or the
// ControlOpponentsSearchingLibrary$ static (searchControlRedirect) -- and both
// record the asked-of seat with Decision.RedirectTo, so Submit's actingView
// runs every validator and handler for that seat
// (controlplayer_actor_test.go pins the behaviour on Mindslaver).
//
// It is a two-direction ratchet (Ruling R-20): the measured class must equal
// controlRedirectClass. A new corpus card that grants control of a player
// fails here, so whoever bumps FORGE_REF checks it reaches one of the two
// redirects (a new delivery shape would answer as the controller and act for
// the wrong seat -- the cardfuzz "no activatable mana ability" class); a card
// that leaves the class fails as stale.

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

const (
	redirectControlTurn = "api:ControlPlayer"
	redirectSearch      = "static:ControlOpponentsSearchingLibrary"
)

// controlRedirectClass is the measured class at the FORGE_REF pin: card name
// -> the redirect its control reaches.
var controlRedirectClass = map[string]string{
	"Construct a Cosmic Cube":   redirectControlTurn,
	"Cruel Entertainment":       redirectControlTurn,
	"Emrakul, the Promised End": redirectControlTurn,
	"Mindslaver":                redirectControlTurn,
	"Opposition Agent":          redirectSearch,
	"Secret of Bloodbending":    redirectControlTurn,
	"Sorin Markov":              redirectControlTurn,
	"The Dominion Bracelet":     redirectControlTurn,
	"Urza, Academy Headmaster":  redirectControlTurn,
	"Worst Fears":               redirectControlTurn,
}

// saUsesControlPlayer walks an ability and its resolved SubAbility$ chain.
func saUsesControlPlayer(sa *cards.SA) bool {
	for ; sa != nil; sa = sa.Sub {
		if sa.API == "ControlPlayer" {
			return true
		}
	}
	return false
}

// controlRedirectOf classifies one card: "" when it is outside the class.
// SVar bodies are read as text because a granted ability (The Dominion
// Bracelet's AddAbility$) or a GenericChoice branch (Urza) lives only there.
func controlRedirectOf(c *cards.Card) string {
	for _, f := range c.Faces {
		if f == nil {
			continue
		}
		for _, st := range f.Statics {
			if _, ok := st.Params["ControlOpponentsSearchingLibrary"]; ok {
				return redirectSearch
			}
		}
		for _, sa := range f.Abilities {
			if saUsesControlPlayer(sa) {
				return redirectControlTurn
			}
		}
		for _, tr := range f.Triggers {
			if saUsesControlPlayer(tr.Effect) {
				return redirectControlTurn
			}
		}
		for _, body := range f.SVars {
			if strings.Contains(body, "$ ControlPlayer") {
				return redirectControlTurn
			}
			if strings.Contains(body, "ControlOpponentsSearchingLibrary$") {
				return redirectSearch
			}
		}
	}
	return ""
}

func TestControlRedirectCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	got := map[string]string{}
	for _, c := range reg.Cards {
		if c == nil || len(c.Faces) == 0 || c.Faces[0] == nil {
			continue
		}
		if r := controlRedirectOf(c); r != "" {
			got[c.Faces[0].Name] = r
		}
	}
	var drift []string
	for name, r := range got {
		if want, ok := controlRedirectClass[name]; !ok {
			drift = append(drift, "NEW "+name+" ("+r+"): confirm Decision.RedirectTo covers it, then add it")
		} else if want != r {
			drift = append(drift, "CHANGED "+name+": "+want+" -> "+r)
		}
	}
	for name := range controlRedirectClass {
		if _, ok := got[name]; !ok {
			drift = append(drift, "STALE "+name+": no longer in the class")
		}
	}
	sort.Strings(drift)
	for _, d := range drift {
		t.Error(d)
	}
	t.Logf("CR 722 control-redirect class: %d cards", len(got))
}
