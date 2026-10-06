package rules

import (
	"sort"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// rememberRoots is every ability of a face the engine can resolve a body
// from: the A:/AB$ lines, the trigger Execute$ bodies and the replacement
// ReplaceWith$ bodies (each already linked to its SubAbility$ chain by
// cards.Face.link), PLUS every SVar body the face declares.
//
// The SVar half is why this is not just the linked roots: the engine reaches
// an SVar body through a PARAMETER that names it, not always through the
// baked Sub chain. A Saga chapter (K:Chapter:<n>:<svar>) is an SVar body the
// engine resolves at trigger time (rules/saga.go); an ImmediateTrigger's
// Execute$ and a DelayedTrigger's Execute$ name an SVar nested inside a
// trigger body; a GenericChoice/Charm Choices$ names its options by SVar
// (Urza, Academy Headmaster). The blanket walk is the ONE reachability rule
// for SVar-named chains (cards.Face.EachSVarAbility), the same one Face.Primitives
// (coverage) and rules' cardCensusLabels (param census) go through, so this
// census cannot disagree with them about which bodies exist. It is a
// conservative superset: a body only reachable through some other naming
// parameter still counts, which is what a ratchet wants -- a new carrier must
// fail loudly, never slip through.
func rememberRoots(f *cards.Face) []*cards.SA {
	var roots []*cards.SA
	roots = append(roots, f.Abilities...)
	for _, tr := range f.Triggers {
		roots = append(roots, tr.Effect)
	}
	for _, r := range f.Repls {
		roots = append(roots, r.With)
	}
	f.EachSVarAbility(func(sa *cards.SA) { roots = append(roots, sa) })
	return roots
}

// rememberTargetsCensus is every corpus card carrying an SA with
// `RememberTargets$ True`, grouped by the SA's API, over the linked ability
// chains (roots and their SubAbility$ links). It is the ratchet for the
// generic home: a new carrier (a new API, or a new card in an existing
// group) fails here until it is listed, so the parameter's coverage cannot
// silently drift. Measured 2026-10-03 at FORGE_REF.
var rememberTargetsCensus = map[string][]string{
	"AlterAttribute": {"You're in Command"},
	"Animate":        {"Flourishing Grapple"},
	"BecomesBlocked": {"Choking Vines"},
	"ChangeZone":     {"Acrobatic Maneuver", "Admonition Angel", "Aminatou, the Fateshifter", "Bloodhill Bastion", "Blur", "Brokers' Safeguard", "Cemetery Recruitment", "Cleric Class", "Cloudshift", "Dead Reckoning", "Diabolic Servitude", "Dimir Doppelganger", "Displace", "Eldrazi Displacer", "Emiel the Blessed", "Ephemerate", "Escape Protocol", "Essence Flux", "Extirpate", "Faceless Devourer", "Felidar Guardian", "Flicker", "Flicker of Fate", "Flickering Hound", "Gandalf, Shadow's Foe", "Gravegouger", "Hallowed Respite", "Hawkins National Laboratory", "Housemeld", "Illusionist's Stratagem", "Immersturm", "Journey to Nowhere", "Justiciar's Portal", "Leonin Relic-Warder", "Lie in Wait", "Momentary Blink", "Moratorium Stone", "Nephalia Smuggler", "Pegasus Guardian", "Personify", "Petradon", "Petravark", "Planar Incision", "Pyretic Rebirth", "Quantum Entanglement", "Restoration Angel", "Ruin Ghost", "Scrollshift", "Secret Salvage", "Skycoach Conductor", "Slip On the Ring", "Slithery Stalker", "Splash Portal", "Tamiyo, Inquisitive Student", "Tawnos Endures", "The Grand Tour", "Triad of Fates", "Turn to Mist", "Vengeful Rebirth", "Vindictive Triumph", "Wispweaver Angel", "Word of Undoing"},
	"ChangeZoneAll":  {"Trial"},
	"ControlSpell":   {"Chef's Kiss"},
	"Counter":        {"Arcane Denial"},
	"DealDamage":     {"Craterous Stomp", "Kaya, Orzhov Usurper"},
	"Destroy":        {"Afflicted Deserter", "Builder's Bane", "Sorin, Lord of Innistrad", "Soul of Emancipation", "Urza, Academy Headmaster", "Volcanic Eruption", "Vona de Iedo, the Antifex"},
	"LoseLife":       {"Laquatus's Champion", "Soul Scourge"},
	"Pump":           {"A-Fall of the Impostor", "A-Incriminate", "Alpha Brawl", "Become Anonymous", "Bile Blight", "Blow Your House Down", "Equipoise", "Fall of the Impostor", "Filigree Vector", "Retribution", "Silence the Believers", "Soul Nova", "Volcanic Offering"},
	"PutCounter":     {"Gore Vassal", "Outmuscle", "Sylvan Smite", "Torment of Venom"},
	"RevealHand":     {"Dreams of Steel and Oil", "Hint of Insanity", "Struggle for Sanity"},
	"Tap":            {"Winter Blast"},
	"TapAll":         {"Feint"},
	"Untap":          {"Freyalise, Skyshroud Partisan"},
}

// TestRememberTargetsCensus pins the class both ways: a carrier that drops
// out of the shape, or a new corpus card that enters it, fails here.
func TestRememberTargetsCensus(t *testing.T) {
	t.Parallel()
	reg := testutil.CorpusRegistry(t)
	got := map[string]map[string]bool{}
	for _, c := range reg.AllCards() {
		for _, f := range c.Faces {
			if f == nil {
				continue
			}
			for _, root := range rememberRoots(f) {
				for d, sa := 0, root; sa != nil && d < 64; d, sa = d+1, sa.Sub {
					if !strings.EqualFold(strings.TrimSpace(sa.Params["RememberTargets"]), "True") {
						continue
					}
					if got[sa.API] == nil {
						got[sa.API] = map[string]bool{}
					}
					got[sa.API][c.Faces[0].Name] = true
				}
			}
		}
	}
	measured := map[string][]string{}
	for api, names := range got {
		out := make([]string, 0, len(names))
		for n := range names {
			out = append(out, n)
		}
		sort.Strings(out)
		measured[api] = out
	}
	if len(measured) != len(rememberTargetsCensus) {
		t.Logf("measured census: %#v", measured)
		t.Fatalf("RememberTargets$ carriers: %d APIs, want %d -- update rememberTargetsCensus", len(measured), len(rememberTargetsCensus))
	}
	for api, names := range rememberTargetsCensus {
		gotNames := measured[api]
		if strings.Join(gotNames, "\n") != strings.Join(names, "\n") {
			t.Errorf("RememberTargets$ carriers for %s:\n got  %q\n want %q", api, gotNames, names)
		}
	}
}
