package cards

import (
	"sort"
	"strings"
)

// Primitives lists every engine symbol this face needs, prefixed by kind. The
// result is sorted so it is stable across runs — coverage reports and the IR
// cache both depend on that. Because Link runs expandKeywords first
// (keywords.go), an expanded keyword's own triggers/replacements/abilities
// are already on the Face by the time this walks it: a Batterskull lists
// not just kw:Living Weapon and kw:Equip but also the api:Token, api:Attach
// and trig:ChangesZone its expansion needs, exactly as if those lines had
// been printed in the script by hand.
//
// The walk follows SubAbility$ chains AND every SVar body this face declares
// (EachSVarAbility), because an effect primitive resolves some behaviour from
// a PARAMETER that names an SVar rather than from its Sub chain — `Choices$`
// (effCharm/effVote), `RepeatSubAbility$` (effRepeat), Branch's
// True/FalseSubAbility$ (resumeResolution), a delayed trigger's `Execute$`.
// Path of the Ghosthunter's api:Planeswalk/api:ChaosEnsues and Torment of
// Hailfire's api:GenericChoice are reachable only that way; a Sub-only walk
// would report the card as fully supported while those APIs are unregistered.
// ResolveSVar returns nil for a body that does not parse as an ability (a
// `Count$…` expression behind ConditionCheckSVar$/SVarCompare$), so value
// bodies are not pulled in.
func (f *Face) Primitives() []string {
	set := map[string]struct{}{}
	var walk func(sa *SA, depth int)
	walk = func(sa *SA, depth int) {
		if sa == nil || depth > maxSVarDepth {
			return
		}
		set["api:"+sa.API] = struct{}{}
		if sa.API == "MustBlock" && !MustBlockNamedTargetShape(sa) {
			// API registration alone cannot certify selector grammars that the
			// resolver cannot bind to a single attacker and a mandatory target.
			set["api:MustBlock.OtherShape"] = struct{}{}
		}
		walk(sa.Sub, depth+1)
	}
	for _, a := range f.Abilities {
		walk(a, 0)
	}
	for _, t := range f.Triggers {
		set["trig:"+t.Mode] = struct{}{}
		walk(t.Effect, 0)
	}
	for _, s := range f.Statics {
		set["stat:"+s.Mode] = struct{}{}
	}
	for _, r := range f.Repls {
		set["repl:"+r.Event] = struct{}{}
		walk(r.With, 0)
	}
	for _, k := range f.Keywords {
		head := KeywordHead(k)
		set["kw:"+head] = struct{}{}
		if head == "Craft" && !CraftUniformShape(keywordParam(k)) {
			set["api:Craft.OtherShape"] = struct{}{}
		}
	}
	// Blanket SVar walk: the shared reachability point so this coverage walk
	// and rules' param census (cardCensusLabels) cannot disagree about which
	// SVar bodies are reachable.
	f.EachSVarAbility(func(sa *SA) { walk(sa, 0) })
	f.EachRawEffectChild(func(c EffectChild) {
		if c.Trigger != nil {
			set["trig:"+c.Trigger.Mode] = struct{}{}
		} else if c.Static != nil {
			set["stat:"+c.Static.Mode] = struct{}{}
		} else if c.Repl != nil {
			set["repl:"+c.Repl.Event] = struct{}{}
		}
	})
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// EachSVarAbility calls visit for every SVar body this face declares that
// compiles to an ability. A body that is not an ability (a `Count$…`
// expression behind ConditionCheckSVar$/SVarCompare$) makes ResolveSVar
// return nil and is skipped, so value bodies are not visited. Each name is
// resolved once; the returned *SA's own SubAbility$ chain is already resolved
// by ResolveSVar, and its depth cap bounds a cyclic reference. This is the
// one reachability rule for SVar-named ability chains: Face.Primitives (the
// coverage walk) and rules' cardCensusLabels (the param census) both go
// through it.
func (f *Face) EachSVarAbility(visit func(sa *SA)) {
	if visit == nil {
		return
	}
	for name := range f.SVars {
		if sa := ResolveSVar(f.SVars, name); sa != nil {
			visit(sa)
		}
	}
}

// EffectChild is one raw trigger, static, or replacement body named by an
// Effect ability. Exactly one pointer is non-nil.
type EffectChild struct {
	Trigger *Trigger
	Static  *Static
	Repl    *Repl
}

// EachRawEffectChild visits the typed raw bodies named by every Effect in the
// face. The owning Effect field is the type authority; malformed, missing, or
// wrong-shaped SVar bodies are ignored.
func (f *Face) EachRawEffectChild(visit func(EffectChild)) {
	if visit == nil {
		return
	}
	seen := map[*SA]bool{}
	var walk func(*SA)
	walk = func(sa *SA) {
		if sa == nil || seen[sa] {
			return
		}
		seen[sa] = true
		if sa.API == "Effect" {
			for _, name := range rawEffectNames(sa.ParamStr(PKTriggers)) {
				if t, ok := ParseTriggerLine(f.SVars[name]); ok {
					visit(EffectChild{Trigger: &t})
				}
			}
			for _, name := range rawEffectNames(sa.ParamStr(PKStaticAbilities)) {
				for _, s := range parseRawStatics(f.SVars[name]) {
					visit(EffectChild{Static: &s})
				}
			}
			for _, name := range rawEffectNames(sa.Params["ReplacementEffects"]) {
				if r, ok := ParseReplacementLine(f.SVars[name]); ok {
					visit(EffectChild{Repl: &r})
				}
			}
		}
		walk(sa.Sub)
	}
	for _, a := range f.Abilities {
		walk(a)
	}
	for _, t := range f.Triggers {
		walk(t.Effect)
	}
	for _, r := range f.Repls {
		walk(r.With)
	}
	f.EachSVarAbility(walk)
}

// rawEffectNames matches Effect's runtime name-list grammar: commas and
// whitespace separate SVar names in all three typed fields.
func rawEffectNames(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
}

func parseRawStatics(body string) []Static {
	statics, ok := ParseStaticLines(body)
	if !ok {
		return nil
	}
	return statics
}

// Primitives is the union across every face.
func (c *Card) Primitives() []string {
	set := map[string]struct{}{}
	for _, f := range c.Faces {
		for _, p := range f.Primitives() {
			set[p] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
