// Choosing a trigger's probe card from its card filter. A trigger such as
// "whenever you cast a multicoloured spell" (ValidCard$ Card.MultiColor) or
// "whenever another Goblin you control dies" (Creature.Goblin+YouCtrl) fires
// only for a card the filter accepts, and a fixed probe list (Shock, Grizzly
// Bears) is rejected by the filter, so the cause never fires and the
// requirement was skipped "trigger did not fire".
//
// The filter is NOT parsed here. The probe's real card is placed in a scratch
// game and gorge's own matcher (effects.MatchesObjectCtx, the one the trigger
// itself uses) accepts or rejects it, so the engine stays the only authority
// on what a filter means. A filter the matcher does not implement is
// undecided, and the old probe order stands.
package templates

import (
	"sort"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/events"
	"github.com/adams-shaun/gorge/state"
)

// filterProbe evaluates one trigger filter against probe cards.
type filterProbe struct {
	g       *state.Game
	spec    string
	zone    state.Zone
	decided bool
	p1p1    bool // zone-change recipes may supply a counter on the victim
	// controller is the seat that controls the probe the matcher is asked
	// about (0 unless an OppCtrl filter needs the opponent's permanent). The
	// trigger's source is always p0's.
	controller int
}

// newFilterProbe prepares the evaluation of spec for a probe sitting in zone
// (the stack for a cast, the battlefield for a dying creature). A blank or
// undecidable spec leaves decided false and every probe accepted.
func newFilterProbe(spec string, zone state.Zone) *filterProbe {
	p := &filterProbe{spec: spec, zone: zone}
	if strings.TrimSpace(spec) == "" || len(effects.UnknownPredicates(spec)) > 0 {
		return p
	}
	p.g = state.NewGame([]string{"p0", "p1"})
	p.decided = true
	return p
}

// accepts reports whether the matcher takes card as the trigger's subject.
// The probe is the controller's (p0 unless set), and the trigger's source is a
// different object of p0, so YouCtrl accepts a p0 probe and Other does too.
func (p *filterProbe) accepts(card *cards.Card) bool {
	if !p.decided {
		return true
	}
	// A scratch game of two objects, rebuilt per probe so scanning the whole
	// corpus does not grow it.
	p.g.Objs, p.g.NextID = p.g.Objs[:0], 1
	source := p.g.AddObject(card, 0)
	source.Zone = state.ZBattlefield
	sourceID := source.ID
	subject := p.g.AddObject(card, state.PlayerID(p.controller))
	subject.Zone = p.zone
	if p.p1p1 {
		events.Apply(p.g, events.Event{Kind: events.CounterChange, Obj: subject.ID, Counter: "P1P1", Amount: 1})
	}
	return effects.MatchesObjectCtx(p.g, p.spec, p.g.Obj(subject.ID), effects.SpecContext{You: 0, Source: sourceID})
}

// victimProbes lists, in sorted name order, creature cards a destroy probe can
// kill that the filter accepts: no abilities of their own to disturb the
// trigger, a fixed toughness so setup does not kill them, and no way to resist
// destruction. Capped, since each costs a full scenario run.
func (p *filterProbe) victimProbes(reg *cards.Registry, skip string, limit int) []string {
	names := make([]string, 0, len(reg.Cards))
	for _, c := range reg.Cards {
		if len(c.Faces) != 0 {
			names = append(names, c.Faces[0].Name)
		}
	}
	sort.Strings(names)
	var out []string
	for _, n := range names {
		if n == skip || !oraclegen.XMageKnown(n) {
			continue
		}
		card, ok := reg.Lookup(n)
		if !ok || len(card.Faces) != 1 {
			continue
		}
		f := card.Faces[0]
		if !f.IsCreature() || f.IsLand() || f.Name != n || len(f.Abilities) != 0 || len(f.Triggers) != 0 || len(f.Statics) != 0 || len(f.Repls) != 0 || f.CharacteristicDefining() || f.Toughness() < 1 || resistsDestroy(f) {
			continue
		}
		if !p.accepts(card) {
			continue
		}
		out = append(out, n)
		if len(out) == limit {
			break
		}
	}
	return out
}

// resistsDestroy reports a printed keyword that stops a destroy probe from
// killing the creature or from targeting it.
func resistsDestroy(f *cards.Face) bool {
	for _, k := range []string{"Indestructible", "Hexproof", "Shroud", "Protection", "Ward", "Persist", "Undying"} {
		if _, ok := f.KeywordParam(k); ok {
			return true
		}
	}
	return false
}

// diesVictimSkips are the dying-victim qualifiers no destroy probe can
// supply, each with the reason it is skipped by name. The victim is placed by
// setup and destroyed in main phase, so it is never attacking, blocking, face
// down or a token, was never dealt damage, and starts with toughness 1 or
// more; a Clue is a token the setup cannot place.
var diesVictimSkips = []struct{ qualifier, reason string }{
	{"attacking", "dies victim must be attacking"},
	{"blocking", "dies victim must be blocking"},
	{"facedown", "dies victim must be facedown"},
	{"token", "dies victim must be token"},
	{"damagedby", "dies victim must be damaged by the card"},
	{"toughnesslt1", "dies victim must have toughness less than 1"},
	{"clue", "dies victim must be a Clue token"},
}

// diesVictimSkip names the dying-victim qualifier no destroy probe can supply.
func diesVictimSkip(t *cards.Trigger) string {
	filter := strings.ToLower(levelb.ZoneChangeFilter(t))
	for _, q := range diesVictimSkips {
		if strings.Contains(strings.ReplaceAll(filter, "!"+q.qualifier, ""), q.qualifier) {
			return q.reason
		}
	}
	return ""
}
