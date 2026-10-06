package templates

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/state"
)

// Victims and destroy probes for a "whenever <another permanent> dies"
// trigger. The filter names the victim's side, card type and qualifiers, so
// the victim is whichever card gorge's own matcher accepts, placed on the
// side the filter names, and the destroy probe is chosen by the victim's
// type.
var (
	// diesTypedVictims are tried, in order, after the Grizzly Bears: an
	// artifact creature with no abilities (Ornithopter), a vanilla
	// enchantment (Glorious Anthem has no creature to buff, so it does
	// nothing), and a Food (Gingerbrute, an artifact creature).
	diesTypedVictims = []string{"Ornithopter", "Glorious Anthem", "Gingerbrute"}
	// Disenchant and Naturalize ({1}{W} / {1}{G}, destroy target artifact or
	// enchantment / destroy target artifact or enchantment) take a noncreature
	// permanent; Shatter ({1}{R}) takes an artifact only.
	enchantmentDestroyProbes = []string{"Disenchant", "Naturalize"}
	artifactDestroyProbes    = []string{"Shatter", "Disenchant"}
)

// diesVictim is one permanent the dies-other cause places and destroys.
type diesVictim struct {
	name    string
	side    int  // the seat that controls it
	counter bool // it carries a +1/+1 counter (HasCounters, modified)
}

// target is the victim as a step target.
func (v diesVictim) target() string { return "p" + strconv.Itoa(v.side) + ":" + v.name }

// diesDestroyProbes are the spells that destroy a victim of face's type.
func diesDestroyProbes(face *cards.Face) []string {
	switch {
	case face.IsCreature():
		return destroyProbes
	case oraclegen.HasType(face, "Enchantment"):
		return enchantmentDestroyProbes
	}
	return artifactDestroyProbes
}

// diesVictims lists the permanents on side the filter accepts: the Grizzly
// Bears first (bare, then with a counter), then the typed victims, then the
// matcher-chosen creatures with no abilities. An empty result means no victim
// the setup can place satisfies the filter on this side.
func diesVictims(reg *cards.Registry, fp *filterProbe, skip string, side int) []diesVictim {
	fp.controller = side
	defer func() { fp.controller, fp.p1p1 = 0, false }()
	counters := []bool{false}
	if side == 0 {
		// A counter is a p0 setup fixture, so only p0's victim carries one.
		counters = append(counters, true)
	}
	for _, counter := range counters {
		fp.p1p1 = counter
		var out []string
		if bears, ok := reg.Lookup(bearsProbe); ok && skip != bearsProbe && fp.accepts(bears) {
			out = []string{bearsProbe}
		}
		for _, n := range diesTypedVictims {
			if len(out) > 0 {
				break
			}
			if card, ok := reg.Lookup(n); ok && n != skip && oraclegen.XMageKnown(n) && fp.accepts(card) {
				out = append(out, n)
			}
		}
		if len(out) == 0 {
			out = fp.victimProbes(reg, skip, 4)
		}
		if len(out) > 0 {
			victims := make([]diesVictim, len(out))
			for i, n := range out {
				victims[i] = diesVictim{name: n, side: side, counter: counter}
			}
			return victims
		}
	}
	return nil
}

// diesOtherRecipe builds the causes of "whenever <another permanent> dies"
// (ChangesZone and ChangesZoneAll, Battlefield to Graveyard, not the card). An
// aura's trigger names the enchanted creature and an Equipment's the equipped
// one: the aura is cast on the Bears first (setup cannot place an unattached
// aura) and the Equipment is attached to them by a prelude step. Any other
// filter gets the victim diesVictims picks, on the side its OppCtrl or YouCtrl
// names, and the Bears on p0 when the matcher cannot decide the filter or no
// victim is accepted (the trigger then does not fire and diesVictimSkip names
// the qualifier).
func diesOtherRecipe(reg *cards.Registry, f *cards.Face, name string, t *cards.Trigger) (causes []triggerCause, why string) {
	if name == bearsProbe {
		return nil, "dies-other probe is the card"
	}
	filter := levelb.ZoneChangeFilter(t)
	var pre, prelude []oraclegen.Step
	aura := strings.Contains(filter, "AttachedBy") || strings.Contains(filter, "EnchantedBy")
	equip := strings.Contains(filter, "EquippedBy")
	var victims []diesVictim
	switch {
	case aura:
		st, ok := castProbe(reg, name, "p0:"+bearsProbe)
		if !ok {
			return nil, "aura has no mana pool"
		}
		pre = []oraclegen.Step{st, {Op: "resolve"}}
	case equip:
		if !oraclegen.HasType(f, "Equipment") {
			return nil, "equipped filter on a card that is not Equipment"
		}
		prelude = []oraclegen.Step{{Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + bearsProbe}}
	default:
		fp := newFilterProbe(filter, state.ZBattlefield)
		sides := []int{0, 1}
		if strings.Contains(filter, "OppCtrl") {
			sides = []int{1, 0}
		}
		if fp.decided {
			for _, side := range sides {
				if victims = diesVictims(reg, fp, name, side); len(victims) > 0 {
					break
				}
			}
		}
	}
	if len(victims) == 0 {
		victims = []diesVictim{{name: bearsProbe}}
	}
	for _, v := range victims {
		card, ok := reg.Lookup(v.name)
		if !ok || len(card.Faces) == 0 {
			continue
		}
		for _, p := range diesDestroyProbes(card.Faces[0]) {
			c, ok := castCause(reg, name, p, v.target())
			if !ok {
				continue
			}
			if v.side == 0 {
				c.battlefield = []string{v.name}
			} else {
				c.opponentBattlefield = []string{v.name}
			}
			if v.counter {
				c.counters = map[string]map[string]int{v.name: {"P1P1": 1}}
			}
			c.prelude = append([]oraclegen.Step(nil), prelude...)
			c.steps = append(append([]oraclegen.Step(nil), pre...), c.steps...)
			if aura {
				c.selfInHand = true
				c.hand = append(c.hand, name)
			}
			causes = append(causes, c)
		}
	}
	return causes, ""
}
