package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// indirectAttackSVar serves an attack trigger whose CheckSVar$ is an indirect
// composite (SVar$A/Plus.B[ /Plus.C...]) whose referenced SVars each count
// `Count$Valid Creature.<type>+attacking` (Fearless Swashbuckler's "if a
// Pirate and a Vehicle attacked this combat"). One attacker of each named
// type is added to the cause's attack step; a Vehicle is not a creature until
// it is crewed, so its crew activation runs as a prelude step first. The
// source is dropped from the attackers: it is the crew fodder (the only other
// creature at crew time) and is tapped before attackers are declared. The
// engine's own SVar evaluator and trigger matcher remain the authority on the
// gate; a shape this builder cannot build returns false and the row keeps its
// named skip.
func indirectAttackSVar(reg *cards.Registry, f *cards.Face, t *cards.Trigger, c *triggerCause) bool {
	types := indirectAttackingTypes(f, t)
	if len(types) == 0 {
		return false
	}
	at := -1
	for i, st := range c.steps {
		if st.Op == "attack" {
			at = i
		}
	}
	if at < 0 {
		return false
	}
	var atk []string
	var prelude []oraclegen.Step
	var xab []string
	activationCost := ""
	for _, ty := range types {
		probe := indirectAttackerProbe(reg, ty)
		if probe == "" {
			return false
		}
		card, ok := reg.Lookup(probe)
		if !ok || len(card.Faces) == 0 {
			return false
		}
		if hasType(card.Faces[0], "Vehicle") {
			steps, cab, taps, cost, ok := crewPrelude(card.Faces[0], probe)
			if !ok {
				return false
			}
			c.battlefield = appendFixtureUnique(c.battlefield, probe)
			for _, tap := range taps {
				c.battlefield = appendFixtureUnique(c.battlefield, tap)
			}
			prelude = append(prelude, steps...)
			xab = append(xab, cab...)
			activationCost = cost
		} else {
			// A setup-placed attacker, not a cast: a creature cast this turn
			// has summoning sickness and cannot attack. The crew's tap
			// election takes the source (the lowest-id other creature), so the
			// probe stays untapped.
			c.battlefield = appendFixtureUnique(c.battlefield, probe)
		}
		atk = append(atk, "p0:"+probe)
	}
	// preludeXAbility is parallel to prelude from index 0: pad for any prelude
	// steps already on the cause before this builder's block.
	for len(c.preludeXAbility) < len(c.prelude) {
		c.preludeXAbility = append(c.preludeXAbility, "")
	}
	c.prelude = append(c.prelude, prelude...)
	c.preludeXAbility = append(c.preludeXAbility, xab...)
	if activationCost != "" {
		c.preludeActivationCost = activationCost
	}
	c.steps[at].Attackers = atk
	return true
}

// indirectAttackingTypes is the creature types an indirect CheckSVar$ gate
// asks to be attacking, in the order the composite names them. nil when the
// gate is not a Plus-chain of `Count$Valid Creature.<type>+attacking` reads.
func indirectAttackingTypes(f *cards.Face, t *cards.Trigger) []string {
	check := t.ParamStr(cards.PKCheckSVar)
	if check == "" {
		return nil
	}
	body := strings.TrimSpace(f.SVars[check])
	if body == "" {
		body = check
	}
	refs := indirectSVarRefs(body)
	if len(refs) == 0 {
		return nil
	}
	var types []string
	for _, ref := range refs {
		refBody, ok := f.SVars[ref]
		if !ok {
			return nil
		}
		ty, ok := attackingTypeFromCount(refBody)
		if !ok {
			return nil
		}
		types = append(types, ty)
	}
	return types
}

// indirectSVarRefs parses an `SVar$A/Plus.B/Plus.C` body into its referenced
// SVar names [A B C]. nil for any other body, including a bare `SVar$A`.
func indirectSVarRefs(body string) []string {
	const prefix = "svar$"
	if len(body) < len(prefix) || !strings.EqualFold(body[:len(prefix)], prefix) {
		return nil
	}
	segs := strings.Split(body[len(prefix):], "/")
	if len(segs) < 2 {
		return nil
	}
	refs := []string{strings.TrimSpace(segs[0])}
	for _, seg := range segs[1:] {
		op, arg, found := strings.Cut(seg, ".")
		if !found || !strings.EqualFold(strings.TrimSpace(op), "Plus") {
			return nil
		}
		refs = append(refs, strings.TrimSpace(arg))
	}
	return refs
}

// attackingTypeFromCount reads the creature type of a
// `Count$Valid Creature.<type>+attacking[/LimitMax.N]` body, lowercased.
func attackingTypeFromCount(body string) (string, bool) {
	b := strings.ToLower(strings.TrimSpace(body))
	rest, ok := strings.CutPrefix(b, "count$valid ")
	if !ok {
		return "", false
	}
	spec, _, _ := strings.Cut(rest, "/")
	base, qual, found := strings.Cut(spec, "+")
	if !found || !strings.Contains(qual, "attacking") {
		return "", false
	}
	parts := strings.Split(base, ".")
	if len(parts) < 2 || parts[0] != "creature" {
		return "", false
	}
	return parts[len(parts)-1], true
}

// indirectAttackerProbes are the attacker probes the indirect SVar builder
// carries, keyed by the lowercased creature type its count names. Each is a
// cheap card of that type whose own cast/crew the builder drives.
var indirectAttackerProbes = map[string]string{
	"pirate":  "Kitesail Corsair",
	"vehicle": "Veloheart Bike",
}

// indirectAttackerProbe is the probe of the named type, "" when the table has
// none or the corpus lacks it.
func indirectAttackerProbe(reg *cards.Registry, ty string) string {
	probe, ok := indirectAttackerProbes[ty]
	if !ok {
		return ""
	}
	if _, ok := reg.Lookup(probe); !ok {
		return ""
	}
	return probe
}
