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
	if strings.HasPrefix(lower, "spell.") {
		// A target that names a spell on the stack ("this spell costs less
		// if it targets a creature spell") is satisfied the way counterSpell
		// satisfies its own stack slot: cast a probe spell and hold priority,
		// then cast the discounted spell targeting it (CR 117.3c). No
		// opponent turn or response window is needed, so this holds for any
		// Spell.<type> filter the precast table covers.
		pre, reason := costStackPrecast(filter)
		if reason != "" {
			return p, reason
		}
		p.opponent = false
		p.opponentBattlefield = nil
		p.precast = pre
		return p, ""
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

// costStackPrecast picks the spell p0 casts before the discounted probe so a
// stack-target requirement is satisfied: the first precast whose type the
// filter admits (a creature filter wants a creature spell, an instant one an
// instant). It is the same precast table counterSpell uses, so a new
// stack-target shape is a new row there, not a per-card branch here.
func costStackPrecast(filter string) (*precast, string) {
	for i := range precasts {
		p := precasts[i]
		if precastFits(spellFilterForPrecast(filter), p) {
			return &p, ""
		}
	}
	return nil, "no stack-spell fixture for " + filter
}

// spellFilterForPrecast strips a static's "Spell." prefix so precastFits can
// read the type word: "Spell.Creature" admits a creature spell, and
// "Spell.Instant,Spell.Sorcery" either. A @Zone suffix is preserved.
func spellFilterForPrecast(filter string) string {
	zone := ""
	if i := strings.LastIndexByte(filter, '@'); i >= 0 {
		zone = filter[i:]
		filter = filter[:i]
	}
	var out []string
	for _, alt := range strings.Split(filter, ",") {
		parts := strings.Split(strings.TrimSpace(alt), ".")
		if len(parts) > 1 && strings.EqualFold(parts[0], "Spell") {
			parts = parts[1:]
		}
		out = append(out, strings.Join(parts, "."))
	}
	return strings.Join(out, ",") + zone
}

// stackTargetFilter is the filter of the face's first target slot that draws
// from the stack (a counterspell's "Card"+TargetType$ Spell), or "" when the
// face targets no spell.
func stackTargetFilter(f *cards.Face) string {
	for _, s := range oraclegen.SlotSpecs(f) {
		if oraclegen.SlotIsStack(s.Filter) {
			return s.Filter
		}
	}
	return ""
}

// elfBeholdFixture chooses a known, printable Elf from the corpus rather than
// requiring each BeholdExile type to have a card-specific entry.
func elfBeholdFixture(reg *cards.Registry) []string {
	cardsInOrder := append([]*cards.Card(nil), reg.AllCards()...)
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
