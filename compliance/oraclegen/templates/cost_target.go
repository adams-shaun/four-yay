package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// costTargetProbe builds the permanent and exact target needed by a target-
// dependent reduction. Filters outside a battlefield fixture stay named gaps;
// in particular, combat-state targets cannot be made true by merely placing a
// creature during setup.
func costTargetProbe(reg *cards.Registry, f *cards.Face, st cards.Static, p costProbe) (costProbe, string) {
	filter := strings.TrimSpace(st.Params["ValidTarget"])
	if filter == "" {
		return p, ""
	}
	lower := strings.ToLower(filter)
	if strings.Contains(lower, "attacking") {
		creature := "Grizzly Bears"
		p.opponentBattlefield = appendUnique(p.opponentBattlefield, creature)
		p.pre = append(p.pre,
			oraclegen.Step{Op: "pass_to", Step: "declare-attackers", Active: "p1"},
			oraclegen.Step{Op: "attack", Seat: 1, Defender: "p0", Attackers: []string{"p1:" + creature}},
			oraclegen.Step{Op: "pass", Seat: 1},
		)
		p.targets = []string{"p1:" + creature}
		return p, ""
	}
	if strings.Contains(lower, "blocking") {
		return p, "combat target fixture unavailable (" + filter + ")"
	}
	if strings.Contains(lower, "spell.creature") {
		spell := "Grizzly Bears"
		p.opponent = false
		p.opponentBattlefield = nil
		p.pre = append(p.pre, oraclegen.Step{Op: "pass_to", Step: "main1", Active: "p1"})
		p.first = &oraclegen.Step{Op: "cast", Seat: 1, Card: "p1:" + spell, Mana: "CG"}
		p.firstNoResolve = true
		p.skipReason = "opponent creature-spell response fixture unavailable"
		p.targets = []string{"p1:" + spell}
		return p, ""
	}
	if strings.Contains(lower, "spell.") {
		return p, "spell target fixture unavailable (" + filter + ")"
	}
	owner, zone := "p1", "Battlefield"
	if strings.Contains(lower, "youctrl") {
		owner = "p0"
	}
	for _, group := range strings.Split(filter, ",") {
		candidates := activationGroupCandidates(reg, strings.TrimSpace(group), zone, 1)
		for _, candidate := range candidates {
			if len(candidate.battlefield) == 0 {
				continue
			}
			name := candidate.battlefield[0]
			if owner == "p0" {
				p.battlefield = appendUnique(p.battlefield, name)
			} else {
				p.opponentBattlefield = appendUnique(p.opponentBattlefield, name)
			}
			p.targets = []string{owner + ":" + name}
			return p, ""
		}
	}
	return p, "target fixture unavailable (" + filter + ")"
}

// elfBeholdFixture chooses a known, printable Elf from the corpus rather than
// requiring each BeholdExile type to have a card-specific entry.
func elfBeholdFixture(reg *cards.Registry) []string {
	cardsInOrder := append([]*cards.Card(nil), reg.Cards...)
	sort.Slice(cardsInOrder, func(i, j int) bool {
		return cardsInOrder[i].Faces[0].Name < cardsInOrder[j].Faces[0].Name
	})
	for _, c := range cardsInOrder {
		for _, face := range c.Faces {
			if !oraclegen.XMageKnown(face.Name) {
				continue
			}
			for _, typ := range face.Types {
				if strings.EqualFold(typ, "Elf") {
					return []string{face.Name}
				}
			}
		}
	}
	return nil
}
