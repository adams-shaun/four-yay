// Scenario mistakes the continuous-static template used to make: a fixture
// prelude that casts a card the probe already is, and an effect that lands on
// the permanent an Aura or Equipment is attached to rather than on a probe
// (ticket levelb-static-not-observable-shapes). Every helper here only adds a
// candidate or widens what counts as observed after the existing paths failed,
// so a row served before keeps its scenario bytes.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
	"github.com/adams-shaun/gorge/rules"
)

// staticProbeStandIn is the vanilla card a fixture casts instead of a card a
// probe already is. It is in no probe row, so the cast cannot collide.
const staticProbeStandIn = "Runeclaw Bear"

// staticIsProbeCard reports whether card is staticProbe or a staticProbeTable
// row's card. It is the collision test for a fixture prelude's hand or cast
// step, which is always the probe on the battlefield (staticProbe) or a probe
// the Affected$ filter names (staticProbeTable); a probe plan's own probes are
// drawn from the same two sources (staticPlanFor).
func staticIsProbeCard(card string) bool {
	if card == staticProbe {
		return true
	}
	for _, row := range staticProbeTable {
		if row.card == card {
			return true
		}
	}
	return false
}

// staticAvoidProbeCollision returns fixtures followed by a stand-in variant of
// each one whose hand or cast step names a probe card: the entered-this-turn
// prelude casts Grizzly Bears from hand while the probe Grizzly Bears is on the
// battlefield, and no cast of the hand copy can be told from the probe, so the
// whole fixture fails. The variant casts staticProbeStandIn instead. The
// originals stay first, so a fixture that already served keeps its bytes.
func staticAvoidProbeCollision(reg *cards.Registry, fixtures []staticFixture) []staticFixture {
	out := fixtures
	for _, fx := range fixtures {
		if v, ok := staticStandInFixture(reg, fx); ok {
			out = append(out, v)
		}
	}
	return out
}

func staticStandInFixture(reg *cards.Registry, fx staticFixture) (staticFixture, bool) {
	collides := false
	for _, h := range fx.hand {
		collides = collides || staticIsProbeCard(h)
	}
	if !collides {
		return fx, false
	}
	standIn, ok := castProbe(reg, staticProbeStandIn)
	if !ok {
		return fx, false
	}
	fx.hand = append([]string(nil), fx.hand...)
	for i, h := range fx.hand {
		if staticIsProbeCard(h) {
			fx.hand[i] = staticProbeStandIn
		}
	}
	fx.steps = append([]oraclegen.Step(nil), fx.steps...)
	for i, st := range fx.steps {
		if st.Op == "cast" && staticIsProbeCard(strings.TrimPrefix(st.Card, "p0:")) {
			fx.steps[i].Card = standIn.Card
			fx.steps[i].Mana = standIn.Mana
		}
	}
	return fx, true
}

// staticWithAttachHost adds to specs the printed spec of the permanent the
// card is attached to, when that permanent is not already a probe: an Aura or
// Equipment's effect lands on its host, which a fixture such as p1's
// Ornithopter or Forest can be, and staticObserved compares only probes.
func staticWithAttachHost(reg *cards.Registry, s rules.OracleSnapshot, cardName string, specs map[string]staticProbeSpec) map[string]staticProbeSpec {
	host := ""
	for _, p := range s.Permanents {
		if p.Controller == 0 && p.Name == cardName {
			host = p.AttachedTo
		}
	}
	if host == "" {
		return specs
	}
	for _, p := range s.Permanents {
		if p.Ref != host {
			continue
		}
		if _, isProbe := specs[p.Name]; isProbe {
			return specs
		}
		extra := staticProbeSpecs(reg, []string{p.Name})
		if len(extra) == 0 {
			return specs
		}
		merged := make(map[string]staticProbeSpec, len(specs)+1)
		for k, v := range specs {
			merged[k] = v
		}
		for k, v := range extra {
			merged[k] = v
		}
		return merged
	}
	return specs
}

// staticAffectsAttachment reports whether a static's Affected$ filter names the
// permanent an Equipment is attached to (EquippedBy / AttachedBy).
func staticAffectsAttachment(affected string) bool {
	words := affectedWords(affected)
	return hasWord(words, "EquippedBy") || hasWord(words, "AttachedBy")
}

// staticBackFaceAttach attaches a back-face Equipment to p0's probe, so the
// static it carries on its equipped creature is live at the final checkpoint.
// A face-after-0 Equipment is placed by setup, which attaches nothing.
func staticBackFaceAttach(base *oraclegen.Item, f *cards.Face, name, affected string) {
	if !oraclegen.HasType(f, "Equipment") || !staticAffectsAttachment(affected) {
		return
	}
	base.Steps = append(base.Steps, oraclegen.Step{
		Op: "attach", Seat: 0, Card: "p0:" + name, AttachedTo: "p0:" + staticProbe,
	})
}
