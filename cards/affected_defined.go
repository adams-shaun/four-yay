package cards

import "strings"

// affectedDefinedFilter maps an `AffectedDefined$` value onto the filter
// property the legacy `Affected$` form carried for the same object.
var affectedDefinedFilter = map[string]string{
	"Self":            "Self",
	"Enchanted":       "EnchantedBy",
	"Equipped":        "EquippedBy",
	"AttachedBy Self": "AttachedBy",
}

// NormalizeAffectedDefined rewrites a static's `AffectedDefined$ <who>` (plus
// an optional `Affected$ <Type>[.<props>]` filter) into the single legacy
// `Affected$ <Type>.<who>[+<props>]` filter every static reader in this
// module already understands, and drops the AffectedDefined key.
//
// Upstream Forge moved ~2,600 static lines to the AffectedDefined$ form
// between FORGE_REF 95f04e8 and fb4d809 (`Affected$ Creature.EquippedBy`
// became `AffectedDefined$ Equipped | Affected$ Creature`). The mapping here
// reproduces the old line exactly for every pair measured across the two
// corpora. A value it does not know, or an Affected$ with comma
// alternatives, is left untouched, so the param census still reports it
// rather than the static quietly changing scope.
func NormalizeAffectedDefined(p map[string]string) {
	defined, ok := p["AffectedDefined"]
	if !ok {
		return
	}
	prop, known := affectedDefinedFilter[strings.TrimSpace(defined)]
	if !known {
		return
	}
	affected := strings.TrimSpace(p["Affected"])
	if strings.Contains(affected, ",") {
		return
	}
	if affected == "" {
		affected = "Card"
	}
	typ, props, _ := strings.Cut(affected, ".")
	out := typ + "." + prop
	if props != "" {
		out += "+" + props
	}
	p["Affected"] = out
	delete(p, "AffectedDefined")
}

// parseStaticParams is parseParams for a static body: the one place every
// static reader in this package gets its params, so the AffectedDefined$
// rewrite cannot be missed by one of them.
func parseStaticParams(val string) map[string]string {
	p := parseParams(val)
	NormalizeAffectedDefined(p)
	return p
}
