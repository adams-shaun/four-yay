// Choosing the entering object for an "a <filter> you control enters" trigger
// (trigger.etb-other). The trigger's ValidCard$ names what must enter, and the
// one cause the recipe had -- a cast Grizzly Bears -- fires only when the
// filter accepts a vanilla 2/2 Bear. Everything else played through cleanly
// and the requirement was skipped "trigger did not fire".
//
// The approach is deliberately NOT a second filter parser: it offers a short,
// ordered list of probe cards (a land to play, or a spell to cast) and lets
// gorge's own trigger matcher decide which one satisfies the filter. A probe
// that does not satisfy it simply does not fire and the next one is tried, so
// the engine remains the only authority on the filter's meaning.
//
// The one part that is not left to the engine is a filter whose requirement
// no probe card can meet by construction (a token, a face-down permanent, a
// chosen type, an opponent's permanent): those return a NAMED skip naming the
// filter, never the bare "did not fire".
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// etbLandProbes are lands to play. The typed basics come first so a filter
// naming one of them is served by the matching basic; Gate and Cave lands
// follow for the subtype filters. None carries a cost.
var etbLandProbes = []string{"Plains", "Island", "Swamp", "Mountain", "Forest", "Heap Gate", "Promising Vein"}

// etbCastProbes is the ordered cast list: enchantments, auras, artifacts,
// vehicles, equipment, then the creature subtypes the corpus's etb filters
// name, and then the qualifier creatures (legendary, power>=4, mana value >=5).
// These are tried before the vanilla Bear fallback (see etbProbeCauses).
// Gorge's matcher, including the requested trigger slot, accepts or rejects
// each candidate; this table does not interpret the filter.
var etbCastProbes = []string{
	// Enchantments (a Shrine is an enchantment subtype).
	"Glorious Anthem", "Ghostly Prison",
	// Auras need a bearer: cast on the Grizzly Bears placed for them.
	"Rancor", "Pacifism",
	// Artifacts, including a mana value >=3 artifact and a Vehicle/Equipment/Food.
	"Ornithopter", "Darksteel Ingot", "Chromatic Lantern",
	"Smuggler's Copter", "Consulate Dreadnought", "Bone Saw", "Short Sword", "Gingerbrute",
	// Creature subtypes named by the etb filters.
	"Extremis Elite", "Dragon Hatchling", "Go-Shintai of Boundless Vigor",
	"Expedition Envoy", "Carrion Feeder", "Canopy Spider", "Bog Rats",
	"Inkrise Infiltrator", "Bulwark Ox", "Cloud Sprite", "Air Elemental",
	"Boreal Druid", "Snubhorn Sentry", "Bruce Banner",
	"Aven Skirmisher", "Spore Frog", "Zodiac Rabbit", "Cabaretti Initiate",
	"Bonecache Overseer", "Assassin Initiate",
	// Conjunction and qualifier probes: a legendary Elf (Elf.Legendary), a
	// legendary creature (Legendary), a power>=4 creature (powerGE4), and a
	// mana value >=5 creature (cmcGE5).
	"Abomination of Llanowar", "Adelbert Steiner", "Anara, Wolvid Familiar", "Abbey Gargoyles",
}

// etbAuraProbes are the cast probes that need a bearer on the battlefield.
var etbAuraProbes = map[string]bool{"Rancor": true, "Pacifism": true}

// etbUnservableFilter reports the qualifier that makes filter unservable by
// any cast or played probe, or "". The filter's comma-separated alternatives
// are each tested, and only a filter every alternative of which is
// unservable is reported: "Creature.YouCtrl,Land.OppCtrl" is still served by
// a creature. In one alternative, token means a TOKEN must enter (a cast card
// is never a token; a ChangesZoneAll filter a token maker serves is handled
// by etbTokenServed), faceDown a face-down permanent, ChosenType a permanent
// of a type chosen earlier, and OppCtrl an opponent's permanent. A negated
// qualifier (!token) is satisfied by an ordinary non-token probe, so it is
// stripped before the test.
func etbUnservableFilter(filter string) string {
	bad := ""
	for _, alt := range strings.Split(filter, ",") {
		b := etbUnservableAlternative(alt)
		if b == "" {
			return ""
		}
		if bad == "" {
			bad = b
		}
	}
	return bad
}

func etbUnservableAlternative(alt string) string {
	rest := strings.ReplaceAll(strings.ToLower(alt), "!token", "")
	switch {
	case strings.Contains(rest, "token"):
		return "token"
	case strings.Contains(rest, "facedown"):
		return "faceDown"
	case strings.Contains(rest, "chosentype"):
		return "ChosenType"
	case strings.Contains(rest, "oppctrl"):
		return "OppCtrl"
	}
	return ""
}

// etbTokenServed reports whether some alternative of a ChangesZoneAll filter
// is a creature token the player controls, which a token-maker probe supplies:
// a token needs a plain Card/Creature/Permanent base and no qualifier no
// token probe can meet. "Artifact.token+YouCtrl" stays unservable.
func etbTokenServed(filter string) bool {
	for _, alt := range strings.Split(filter, ",") {
		rest := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(alt), "!token", ""), "nontoken", "")
		base, _, _ := strings.Cut(rest, ".")
		if strings.Contains(rest, "token") && !strings.Contains(rest, "oppctrl") &&
			!strings.Contains(rest, "facedown") && !strings.Contains(rest, "chosentype") &&
			(base == "card" || base == "creature" || base == "permanent") {
			return true
		}
	}
	return false
}

// etbProbeCauses builds the candidate causes for an etb-other trigger, or the
// named reason no probe can serve its filter.
func etbProbeCauses(reg *cards.Registry, name string, t *cards.Trigger) ([]triggerCause, string) {
	filter := levelb.ZoneChangeFilter(t)
	mode := t.ModeKind()
	if mode == cards.TriggerChangesZoneAll {
		if causes, why, ok := etbHistoryProvenanceCauses(reg, name, t, filter); ok {
			return causes, why
		}
	}
	if causes, ok := etbSpecialFilterCauses(reg, name, mode, filter); ok {
		return causes, ""
	}
	tokenServed := mode == cards.TriggerChangesZoneAll && etbTokenServed(filter)
	// Try the specific land and spell probes before the generic fallback. The
	// old vanilla Bear remains last so it can still serve broad creature filters.
	var out []triggerCause
	for _, p := range etbLandProbes {
		if p == name || !oraclegen.XMageKnown(p) {
			continue
		}
		if _, ok := reg.Lookup(p); !ok {
			continue
		}
		out = append(out, triggerCause{
			hand:  []string{p},
			steps: []oraclegen.Step{{Op: "play", Seat: 0, Card: "p0:" + p}},
		})
	}
	for _, p := range etbCastProbes {
		if p == name || !oraclegen.XMageKnown(p) {
			continue
		}
		if etbAuraProbes[p] {
			// An Aura must target a permanent; the standard Bear is placed
			// first and enchanted.
			c, ok := castCause(reg, name, p, "p0:"+bearsProbe)
			if !ok {
				continue
			}
			c.battlefield = append(c.battlefield, bearsProbe)
			out = append(out, c)
			continue
		}
		if c, ok := castCause(reg, name, p); ok {
			out = append(out, c)
		}
	}
	if c, ok := castCause(reg, name, bearsProbe); ok {
		out = append(out, c)
	}
	// The curated list misses a filter that names a creature subtype or
	// qualifier it does not carry (a Detective, a tribe the corpus grew after
	// it was written). Let gorge's own matcher pick a creature probe from the
	// corpus, exactly as the dies recipe picks a victim: the probe that enters
	// and fires the trigger is whichever card the filter accepts.
	if filter != "" {
		fp := newFilterProbe(filter, state.ZBattlefield)
		if fp.decided {
			for _, p := range fp.victimProbes(reg, name, 4) {
				if c, ok := castCause(reg, name, p); ok {
					out = append(out, c)
				}
			}
		}
	}
	if tokenServed {
		tokens, _ := tokenCauses(reg, name)
		out = append(out, tokens...)
	}
	if len(out) == 0 {
		return nil, "probe not in corpus"
	}
	return out, ""
}

// tokenCauses casts a one-token maker (tokenProbes).
func tokenCauses(reg *cards.Registry, name string) ([]triggerCause, string) {
	var out []triggerCause
	for _, p := range tokenProbes {
		if c, ok := castCause(reg, name, p); ok {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, "probe not in corpus"
	}
	return out, ""
}
