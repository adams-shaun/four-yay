package rules

// TestCostStaticCensus measures the WHOLE cost-modifier class across the
// corpus instead of one card at a time. Every Mode$ ReduceCost / RaiseCost /
// SetCost static (printed, keyword-expanded, or carried in an SVar body and
// delivered by an Effect/Animate/AddStaticAbility grant) and every activated
// ability's own ReduceCost$ is classified against the engine's real gate
// chain (costStaticApplies and the helpers it calls). Wherever it can, the
// census CALLS the engine predicate on a fixture board rather than
// re-implementing it, so the census cannot drift from the engine:
//
//   - Condition$         -> e.costConditionHolds on a board where seat 0 is
//                           active, has metalcraft and delirium
//   - CheckSVar$         -> effects.CheckSVarHolds' evaluated verdict
//   - Amount$ / Relative -> effects.EvalCountOK / e.relativeAmountResolves
//   - Activator$/Caster$ -> effects.MatchesPlayerSpecCtx for both seats
//   - ValidSpell$        -> e.validSpellMatches over every cast scope/mode
//   - ValidCard$/IsPresent$/ValidTarget$ -> effects.UnknownPredicates per
//                           alternative plus the base word (matchesBase)
//   - EffectZone$/AffectedZone$ -> effectZoneOK/affectedZoneOK over all zones
//   - offer-time targeting -> markCostValidTarget
//   - unread parameters  -> the param census' derived read set (scanPackages)
//
// Classes:
//
//	BROKEN      the engine can NEVER apply the static (fail-closed shape)
//	WRONG       the engine applies it but ignores/misreads a parameter
//	OFFER-BLIND applies only at the CR 601.2f payment reprice, never at the
//	            offer gate (a cast affordable only with the discount is not
//	            offered)
//
// NOTE marks a log-only finding (a state-dependent spec the fixture cannot
// settle, a genuinely duplicated Secondary$ static).
//
// It is a RATCHET with Ruling R-20's two-direction contract: the measured
// BROKEN cards must equal knownBrokenCostStatics and the WRONG/OFFER-BLIND
// cards knownWrongCostStatics (cost_static_census_known_test.go), each card
// mapped to its sorted reason classes. A newly broken card fails naming its
// finding, raw line and engine file:line; a stale entry fails too. NOTE
// findings stay log-only. GORGE_COST_CENSUS_TSV=1 additionally writes the
// full finding table to /mnt/sata/gorge-training/tmp/cost-census.tsv
// (outside the repo); an ordinary run writes nothing.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

const (
	ccBroken = "BROKEN"
	ccWrong  = "WRONG"
	ccOffer  = "OFFER-BLIND"
	ccNote   = "NOTE"
)

type ccFinding struct{ class, reason, loc string }

type ccRow struct {
	card     string
	playable bool
	mode     string
	delivery string
	raw      string
	findings []ccFinding
}

// ccLoc resolves an anchor substring to file:line in the engine source, so the
// reported location follows the code rather than a hard-coded line number.
type ccLocator struct {
	cache map[string][]string
}

func (l *ccLocator) loc(rel, anchor string) string {
	if l.cache == nil {
		l.cache = map[string][]string{}
	}
	lines, ok := l.cache[rel]
	if !ok {
		b, err := os.ReadFile(filepath.Join("..", rel))
		if err == nil {
			lines = strings.Split(string(b), "\n")
		}
		l.cache[rel] = lines
	}
	for i, ln := range lines {
		if strings.Contains(ln, anchor) {
			return fmt.Sprintf("%s:%d", rel, i+1)
		}
	}
	return rel + ":?(" + anchor + ")"
}

func ccIsCostMode(m string) bool {
	return m == "ReduceCost" || m == "RaiseCost" || m == "SetCost"
}

func TestCostStaticCensus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	supported := effects.Supported()
	L := &ccLocator{}

	// Unread-parameter read sets, derived from the code (paramcensus scan).
	scanned := scanPackages(t)
	reads := scanned.derived()

	// ---- fixture board -------------------------------------------------
	must := func(name string) *cards.Card {
		c, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("corpus fixture %q missing", name)
		}
		return c
	}
	deck0 := []*cards.Card{
		must("Grizzly Bears"), must("Grizzly Bears"),
		must("Ornithopter"), must("Ornithopter"), must("Ornithopter"), must("Ornithopter"),
		must("Lightning Bolt"), must("Lightning Bolt"),
		must("Divination"), must("Divination"),
	}
	deck1 := []*cards.Card{must("Grizzly Bears")}
	e := New(Config{Seed: 7, Names: []string{"a", "b"},
		Decks: [][]*cards.Card{append(deck0, mountainDeck(t, 40-len(deck0))...),
			append(deck1, mountainDeck(t, 40-len(deck1))...)}})
	e.Advance()
	toMain1(t, e)
	src := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	myBears := moveByName(t, e, 0, "Grizzly Bears", state.ZBattlefield)
	for i := 0; i < 3; i++ {
		moveByName(t, e, 0, "Ornithopter", state.ZBattlefield)
	}
	moveByName(t, e, 0, "Ornithopter", state.ZGraveyard)
	moveByName(t, e, 0, "Lightning Bolt", state.ZGraveyard)
	moveByName(t, e, 0, "Divination", state.ZGraveyard)
	moveByName(t, e, 0, "Mountain", state.ZGraveyard)
	instantID := moveByName(t, e, 0, "Lightning Bolt", state.ZHand)
	sorceryID := moveByName(t, e, 0, "Divination", state.ZHand)
	oppBears := moveByName(t, e, 1, "Grizzly Bears", state.ZBattlefield)
	if e.G.Active != 0 || !e.metalcraftHolds(0) || e.graveyardCardTypeCount(0) < 4 {
		t.Fatalf("fixture: active=%d metalcraft=%v gyTypes=%d", e.G.Active, e.metalcraftHolds(0), e.graveyardCardTypeCount(0))
	}
	targets := []state.Target{{Obj: oppBears}, {Obj: myBears}, {IsPlayer: true, Player: 1}, {IsPlayer: true, Player: 0}}

	// ---- corpus-derived vocabularies -----------------------------------
	typeWords := map[string]bool{}
	for _, w := range effects.CreatureTypeWordList() {
		typeWords[w] = true
	}
	kwTags := map[string]bool{}
	walkSA := func(f *cards.Face, visit func(sa *cards.SA)) {
		seen := map[*cards.SA]bool{}
		var walk func(sa *cards.SA)
		walk = func(sa *cards.SA) {
			for ; sa != nil && !seen[sa]; sa = sa.Sub {
				seen[sa] = true
				visit(sa)
			}
		}
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			walk(tr.Effect)
		}
		for _, r := range f.Repls {
			walk(r.With)
		}
		f.EachSVarAbility(walk)
	}
	for _, c := range reg.Cards {
		for _, f := range c.Faces {
			for _, ty := range f.Types {
				typeWords[ty] = true
			}
			walkSA(f, func(sa *cards.SA) {
				for kw := range strings.SplitSeq(sa.Params["Keyword"], ",") {
					if kw = strings.TrimSpace(kw); kw != "" {
						kwTags[kw] = true
					}
				}
			})
		}
	}
	specialBases := map[string]bool{"Any": true, "Card": true, "Permanent": true, "Affinity": true,
		"PermanentCard": true, "Spell": true, "SpellAbility": true}
	baseKnown := func(base string) bool {
		base = strings.TrimSpace(base)
		if nb, ok := strings.CutPrefix(base, "non"); ok && nb != "" {
			base = nb
		}
		return specialBases[base] || typeWords[base]
	}
	playerBase := func(base string) bool {
		switch strings.TrimSpace(base) {
		case "You", "Opponent", "Player", "Other", "Any":
			return true
		}
		return false
	}

	// probe guards every engine call: a panic is itself a finding.
	probe := func(fn func()) (panicked string) {
		defer func() {
			if r := recover(); r != nil {
				panicked = fmt.Sprint(r)
			}
		}()
		fn()
		return ""
	}

	// objAltDead reports why one object-spec alternative can never match
	// ("" when it can). spellScoped flags the Permanent-base zone trap.
	objAltDead := func(alt string, spellOnly bool) (why string, provenance bool) {
		alt = strings.TrimSpace(alt)
		base, _, _ := strings.Cut(alt, ".")
		if !baseKnown(base) {
			return "unknown base word " + strconv.Quote(base), false
		}
		if spellOnly && (base == "Permanent") {
			return "base Permanent matches only battlefield objects, never a spell", false
		}
		var unk []string
		for _, u := range effects.UnknownPredicates(alt) {
			if strings.Contains(u, "wasCast") {
				provenance = true
				continue
			}
			unk = append(unk, u)
		}
		if len(unk) > 0 {
			return "unknown predicate(s) " + strings.Join(unk, ","), provenance
		}
		return "", provenance
	}

	// Activated.* probe SAs: mana, non-mana, loyalty and every keyword tag.
	probeSAs := []*cards.SA{
		{Kind: "AB", API: "Mana", Params: map[string]string{}},
		{Kind: "AB", API: "Pump", Params: map[string]string{}},
		{Kind: "AB", API: "Pump", Params: map[string]string{"Planeswalker": "True"}},
	}
	for kw := range kwTags {
		probeSAs = append(probeSAs, &cards.SA{Kind: "AB", API: "Pump", Params: map[string]string{"Keyword": kw}})
	}
	// The param-backed SA flags (Exhaust$/PowerUp$/Boast$/Monstrosity$ True
	// on the A: line itself): a real carrier's ability has no Keyword$ tag, so
	// the probe offers the flag the way the script spells it.
	for _, flag := range saParamFlagProperties {
		probeSAs = append(probeSAs, &cards.SA{Kind: "AB", API: "Pump", Params: map[string]string{flag: "True"}})
	}
	sort.Slice(probeSAs, func(i, j int) bool { return probeSAs[i].Params["Keyword"] < probeSAs[j].Params["Keyword"] })
	var scopes []costScope
	for _, m := range []string{"", "flashback", "kicked", "kicked1", "surged", "miracle", "blitzed", "foretold", "dashed", "evoked", "bargained", "buyback"} {
		scopes = append(scopes, spellScope(m))
	}
	scopes = append(scopes, foretellScope())
	for _, ab := range probeSAs {
		scopes = append(scopes, abilityScope(ab))
	}
	validSpellAltLive := func(sv staticView, alt string) bool {
		for _, sc := range scopes {
			for _, id := range []state.ObjID{instantID, sorceryID, src, oppBears} {
				for _, p := range []state.PlayerID{0, 1} {
					for _, tg := range [][]state.Target{nil, targets} {
						if e.validSpellMatches(sv, sc, p, id, alt, tg) {
							return true
						}
					}
				}
			}
		}
		// The IsTargeting form is target-conditional: recognised grammar is
		// live even when this fixture's targets do not satisfy it.
		if c, ok := strings.CutPrefix(alt, "Spell."); ok && strings.HasPrefix(c, "IsTargeting") {
			return len(effects.UnknownPredicates(alt)) == 0
		}
		return false
	}

	// secondarySkipped asks the real gate chain whether a Secondary$ True
	// marker alone denies a cost static: the same trivially-live ReduceCost
	// with and without the marker, on the fixture's instant.
	secondarySkipped := func() bool {
		plain := map[string]string{"Mode": "ReduceCost", "ValidCard": "Card", "Type": "Spell", "Amount": "1"}
		marked := map[string]string{"Mode": "ReduceCost", "ValidCard": "Card", "Type": "Spell", "Amount": "1", "Secondary": "True"}
		base := e.costStaticApplies(staticView{Source: src, Controller: 0, Params: plain}, "ReduceCost", 0, instantID, spellScope(""), nil, false)
		withMark := e.costStaticApplies(staticView{Source: src, Controller: 0, Params: marked}, "ReduceCost", 0, instantID, spellScope(""), nil, false)
		if !base {
			t.Fatalf("fixture: a plain ReduceCost does not apply to the fixture instant; the Secondary$ probe is meaningless")
		}
		return !withMark
	}()

	// svarChainMentions reports whether an Amount$ body (following SVar
	// references transitively) mentions needle.
	var svarChainMentions func(svars map[string]string, body, needle string, depth int) bool
	svarChainMentions = func(svars map[string]string, body, needle string, depth int) bool {
		if depth > 6 {
			return false
		}
		if strings.Contains(body, needle) {
			return true
		}
		for tok := range strings.FieldsFuncSeq(body, func(r rune) bool {
			return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
		}) {
			if b, ok := svars[tok]; ok && b != body {
				if svarChainMentions(svars, b, needle, depth+1) {
					return true
				}
			}
		}
		return false
	}

	evalAmount := func(sv staticView, x int32, tg []state.Target) (n int32, ok bool, panicked string) {
		raw := strings.TrimSpace(sv.Params["Amount"])
		if v, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return int32(v), true, ""
		}
		body := raw
		if b, found := sv.SVars[raw]; found {
			body = b
		}
		panicked = probe(func() {
			ctx := &effects.Ctx{Source: sv.Source, Controller: sv.Controller, SVars: sv.SVars, X: x,
				ChosenNumber: 3, ChosenNumberBound: sv.chosenNumberBound, Targets: tg}
			n, ok = effects.EvalCountOK(e, ctx, body)
		})
		return
	}

	faceHasX := func(f *cards.Face) bool {
		if strings.Contains(f.ManaCost, "X") {
			return true
		}
		has := false
		walkSA(f, func(sa *cards.SA) {
			for tok := range strings.FieldsSeq(sa.Params["Cost"]) {
				if tok == "X" || strings.Contains(tok, "<X") || strings.Contains(tok, "/X") {
					has = true
				}
			}
		})
		return has
	}

	// ---- the per-static classifier -------------------------------------
	classify := func(c *cards.Card, f *cards.Face, st cards.Static, delivery string) []ccFinding {
		var out []ccFinding
		add := func(class, reason, loc string) { out = append(out, ccFinding{class, reason, loc}) }
		mode := st.Mode
		p := st.Params
		sv := staticView{Source: src, Controller: 0, Params: p, PS: st.ParamSetOf(), SVars: f.SVars}
		if delivery == "effect" {
			sv.chosenNumberBound = true
			sv.ChosenNumber = 3
		}
		ty := strings.TrimSpace(p["Type"])
		spellOnly := ty == "Spell"

		// Collection: EffectZone$ (printed route only; the Effect route has no zone gate).
		if delivery == "printed" || delivery == "keyword" {
			if v, ok := p["EffectZone"]; ok {
				live := false
				for _, z := range []state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard, state.ZHand,
					state.ZLibrary, state.ZExile, state.ZCommand} {
					if effectZoneOK(v, z) {
						live = true
					}
				}
				if !live {
					add(ccBroken, "EffectZone$ "+strings.TrimSpace(v)+" unrecognised (never collected)",
						L.loc("rules/statics.go", "func effectZoneOK("))
				}
			}
		}
		// ClassBand$
		if raw := strings.TrimSpace(p["ClassBand"]); raw != "" {
			if n, err := strconv.Atoi(raw); err != nil || n < 1 {
				add(ccBroken, "ClassBand$ "+raw+" malformed", L.loc("rules/class_level.go", "if err != nil || n < 1 {"))
			}
		}
		// Type$
		if ty != "" && ty != "Spell" && ty != "Ability" && ty != "Foretell" {
			add(ccBroken, "Type$ "+ty+" is no cost scope kind", L.loc("rules/statics.go", "ok && ty != \"\" && ty != scope.kind {"))
		}
		// Activator$/Caster$
		for _, key := range []string{"Activator", "Caster"} {
			spec, ok := p[key]
			if !ok {
				continue
			}
			live := false
			if pn := probe(func() {
				for _, actor := range []state.PlayerID{0, 1} {
					if effects.MatchesPlayerSpecCtx(e.G, spec, actor, 0, e.playerSpecCtx(src)) {
						live = true
					}
				}
			}); pn != "" {
				add(ccBroken, key+"$ probe panicked: "+pn, L.loc("rules/statics.go", "if !e.costActorMatches(sv, p) {"))
			} else if !live {
				if strings.Contains(spec, "IsRemembered") || strings.Contains(spec, "EnchantedBy") || strings.Contains(spec, "Chosen") {
					add(ccNote, key+"$ "+spec+" state-dependent; unmatched on fixture (inconclusive)", L.loc("rules/statics.go", "if !e.costActorMatches(sv, p) {"))
				} else {
					add(ccBroken, key+"$ "+spec+" matches no player", L.loc("rules/statics.go", "if !e.costActorMatches(sv, p) {"))
				}
			}
			if key == "Caster" && p["Activator"] != "" {
				add(ccWrong, "Caster$ ignored when Activator$ present", L.loc("rules/statics.go", "func (e *Engine) costActorMatches("))
			}
		}
		// ValidCard$
		if spec, ok := p["ValidCard"]; ok {
			var dead []string
			n, prov := 0, false
			for alt := range effects.FilterAlternatives(spec) {
				if strings.TrimSpace(alt) == "" {
					continue
				}
				n++
				why, pv := objAltDead(alt, spellOnly)
				prov = prov || pv
				if why != "" {
					dead = append(dead, strings.TrimSpace(alt)+": "+why)
				}
			}
			locVC := L.loc("rules/statics.go", "if !ok2 || !e.matchesSpec(spec, id, e.staticSpecCtx(sv)) {")
			if strings.Contains(strings.Join(dead, ";"), "base Permanent") {
				locVC = L.loc("effects/filter.go", "func matchesBase(")
			}
			switch {
			case n > 0 && len(dead) == n:
				add(ccBroken, "ValidCard$ dead: "+strings.Join(dead, "; "), locVC)
			case len(dead) > 0:
				add(ccWrong, "ValidCard$ partially dead (narrower): "+strings.Join(dead, "; "), locVC)
			}
			if prov && (strings.Contains(spec, "wasCastFromYourHand") || strings.Contains(spec, "wasCastByYou")) {
				add(ccOffer, "ValidCard$ hand-provenance token: denied pre-push, repriced after push",
					L.loc("rules/cast_provenance.go", "func (e *Engine) castProvenanceAdmitsPending("))
			}
		}
		// ValidSpell$
		if vs, ok := p["ValidSpell"]; ok && strings.TrimSpace(vs) != "" {
			var dead []string
			n := 0
			for alt := range strings.SplitSeq(vs, ",") {
				if alt = strings.TrimSpace(alt); alt == "" {
					continue
				}
				n++
				live := false
				if pn := probe(func() { live = validSpellAltLive(sv, alt) }); pn != "" {
					dead = append(dead, alt+" (probe panicked: "+pn+")")
					continue
				}
				if !live {
					dead = append(dead, alt)
				}
			}
			loc := L.loc("rules/statics.go", "func (e *Engine) spellConstraintMatches(")
			if len(dead) > 0 && (strings.HasPrefix(dead[0], "Activated.")) {
				loc = L.loc("rules/statics.go", "func (e *Engine) abilityConstraintMatches(")
			} else if len(dead) > 0 && strings.HasPrefix(dead[0], "Static.") {
				loc = L.loc("rules/statics.go", "case \"Static\":")
			}
			switch {
			case n > 0 && len(dead) == n:
				add(ccBroken, "ValidSpell$ unsupported: "+strings.Join(dead, ","), loc)
			case len(dead) > 0:
				add(ccWrong, "ValidSpell$ partially unsupported: "+strings.Join(dead, ","), loc)
			}
		}
		// AffectedZone$
		if az, ok := p["AffectedZone"]; ok {
			spellReachable := ty == "Spell"
			if ty == "" {
				spellReachable = true
				if vs := strings.TrimSpace(p["ValidSpell"]); vs != "" {
					spellReachable = false
					for alt := range strings.SplitSeq(vs, ",") {
						if strings.HasPrefix(strings.TrimSpace(alt), "Spell") {
							spellReachable = true
						}
					}
				}
			}
			if spellReachable {
				add(ccWrong, "AffectedZone$ "+strings.TrimSpace(az)+" ignored for spells (read only for Type$ Ability; Forge gates the cast-from zone)",
					L.loc("rules/statics.go", "if az, ok := sv.Param(cards.PKAffectedZone); ok && scope.kind == \"Ability\" {"))
			}
			if ty != "Spell" {
				live := false
				for _, z := range []state.Zone{state.ZBattlefield, state.ZStack, state.ZGraveyard, state.ZHand,
					state.ZLibrary, state.ZExile, state.ZCommand} {
					if affectedZoneOK(az, z) {
						live = true
					}
				}
				if !live {
					add(ccBroken, "AffectedZone$ "+az+" unrecognised", L.loc("rules/statics.go", "func affectedZoneOK("))
				}
			}
		}
		// IsPresent$
		if spec, ok := p["IsPresent"]; ok {
			var dead []string
			n := 0
			for alt := range effects.FilterAlternatives(spec) {
				if strings.TrimSpace(alt) == "" {
					continue
				}
				n++
				if why, _ := objAltDead(alt, false); why != "" {
					dead = append(dead, strings.TrimSpace(alt)+": "+why)
				}
			}
			locIP := L.loc("rules/statics.go", "if spec, ok := sv.Param(cards.PKIsPresent); ok && !e.presentGate(sv, spec) {")
			if n > 0 && len(dead) == n {
				add(ccBroken, "IsPresent$ dead: "+strings.Join(dead, "; "), locIP)
			} else if len(dead) > 0 {
				add(ccWrong, "IsPresent$ partially dead: "+strings.Join(dead, "; "), locIP)
			}
			// The cost gate is the shared presentGate: PresentZone$ through
			// presentZoneFromParam, PresentCompare$ through presentCompareFor
			// + splitCompare. Ask those engine helpers whether this static's
			// spelling is one they read.
			if pz := p["PresentZone"]; strings.TrimSpace(pz) != "" {
				if _, ok := presentZoneFromParam(pz); !ok {
					add(ccBroken, "PresentZone$ "+strings.TrimSpace(pz)+" unrecognised", L.loc("rules/statics.go", "func presentZoneFromParam("))
				}
			}
			if pc := strings.TrimSpace(p["PresentCompare"]); pc != "" {
				if _, _, ok := splitCompare(e.presentCompareFor(pc, sv.Source, sv.Controller)); !ok {
					add(ccBroken, "PresentCompare$ "+pc+" unevaluable", L.loc("rules/statics.go", "func (e *Engine) presentGate("))
				}
			}
		}
		// Condition$
		if cond, ok := p["Condition"]; ok {
			holds := false
			if pn := probe(func() { holds = e.costConditionHolds(sv, 0) || e.costConditionHolds(sv, 1) }); pn != "" || !holds {
				add(ccBroken, "Condition$ "+strings.TrimSpace(cond)+" unsupported", L.loc("rules/statics.go", "func (e *Engine) costConditionHolds("))
			}
		}
		// CheckSVar$
		if raw, ok := p["CheckSVar"]; ok {
			evaluated := false
			pn := probe(func() {
				ctx := &effects.Ctx{Source: src, Controller: 0, SVars: f.SVars}
				_, evaluated = effects.CheckSVarHolds(e, ctx, raw, p["SVarCompare"])
			})
			if pn != "" || !evaluated {
				body := raw
				if b, ok := f.SVars[raw]; ok {
					body = b
				}
				add(ccBroken, "CheckSVar$ "+raw+" unevaluable ("+body+" "+p["SVarCompare"]+")",
					L.loc("rules/statics.go", "holds, evaluated := effects.CheckSVarHolds(e, ctx, raw, sv.Params[\"SVarCompare\"])"))
			}
		}
		// ValidTarget$
		if spec, ok := p["ValidTarget"]; ok {
			var dead []string
			n := 0
			for alt := range effects.FilterAlternatives(spec) {
				if alt = strings.TrimSpace(alt); alt == "" {
					continue
				}
				n++
				base, _, _ := strings.Cut(alt, ".")
				if playerBase(base) && base != "Any" {
					live := false
					probe(func() {
						for _, pl := range []state.PlayerID{0, 1} {
							if effects.MatchesPlayerSpec(e.G, alt, pl, 0) {
								live = true
							}
						}
					})
					if !live && !strings.Contains(alt, "EnchantedBy") {
						dead = append(dead, alt+": player spec matches nobody")
					}
					continue
				}
				if why, _ := objAltDead(alt, false); why != "" {
					dead = append(dead, alt+": "+why)
				}
			}
			locVT := L.loc("rules/statics.go", "if spec, ok := sv.Params[\"ValidTarget\"]; ok && !e.costTargetsMatch(")
			if n > 0 && len(dead) == n {
				add(ccBroken, "ValidTarget$ dead: "+strings.Join(dead, "; "), locVT)
			} else if len(dead) > 0 {
				add(ccWrong, "ValidTarget$ partially dead: "+strings.Join(dead, "; "), locVT)
			}
			if _, ok := p["UnlessValidTarget"]; ok {
				add(ccWrong, "UnlessValidTarget$ unread: the ValidTarget$ test is applied un-inverted", locVT)
			}
		}
		// SetCost
		if mode == "SetCost" && p["RaiseTo"] != "True" {
			add(ccBroken, "SetCost without RaiseTo$ True", L.loc("rules/statics.go", "if mode == \"SetCost\" && sv.Params[\"RaiseTo\"] != \"True\" {"))
		}
		// Secondary$: probe the engine rather than assume. Forge reads
		// Secondary$ for card text only; if costStaticApplies ever skips it
		// again the probe below reports the skip exactly as before.
		if p["Secondary"] == "True" {
			// Is there a primary on the same face with the same gating?
			gate := func(q map[string]string) string {
				var ks []string
				for k, v := range q {
					switch k {
					case "Description", "Secondary", "KeywordLine", "Keyword":
						continue
					}
					ks = append(ks, k+"="+v)
				}
				sort.Strings(ks)
				return strings.Join(ks, "|")
			}
			primary, samegate := false, false
			for _, o := range f.Statics {
				if o.Mode == mode && o.Params["Secondary"] != "True" {
					primary = true
					if gate(o.Params) == gate(p) {
						samegate = true
					}
				}
			}
			locSec := L.loc("rules/statics.go", "// Secondary$ True is NOT a gate")
			switch {
			case secondarySkipped && !primary:
				add(ccBroken, "Secondary$ True skipped (Forge reads Secondary$ for card text only); no primary "+mode+" on the face", locSec)
			case secondarySkipped && !samegate:
				add(ccBroken, "Secondary$ True skipped (Forge reads Secondary$ for card text only); its gates differ from the primary", locSec)
			case secondarySkipped:
				add(ccNote, "Secondary$ True skipped; an identically-gated primary exists (a genuine duplicate)", locSec)
			case samegate:
				add(ccNote, "Secondary$ True applied beside an identically-gated primary (Forge applies both too)", locSec)
			}
		}
		// Relative$
		if p["Relative"] == "True" {
			locRel := L.loc("rules/statics.go", "if !(mode == \"ReduceCost\" && e.relativeAmountResolves(sv, targets)) {")
			if mode != "ReduceCost" {
				add(ccBroken, "Relative$ True on "+mode+" (only ReduceCost has an exception)", locRel)
			} else {
				var rNil, rT bool
				pn := probe(func() {
					rNil = e.relativeAmountResolves(sv, nil)
					rT = e.relativeAmountResolves(sv, targets)
				})
				switch {
				case pn != "":
					add(ccBroken, "Relative$ amount probe panicked: "+pn, locRel)
				case !rNil && !rT && !faceHasX(f):
					add(ccBroken, "Relative$ ReduceCost amount "+p["Amount"]+" unresolvable and no {X} to bind", locRel)
				case !rNil && !rT:
					// resolves only under the announced-X recomputation (Dargo shape)
				}
			}
		}
		// Amount$ / Cost$
		amtRaw, hasAmt := p["Amount"]
		amtRaw = strings.TrimSpace(amtRaw)
		costRaw, hasCost := p["Cost"]
		amountMatters := true
		if mode == "RaiseCost" && hasCost {
			_, _, _, plain := raiseFromCost(costRaw)
			_, blight := raiseExtraFromCost(costRaw)
			switch {
			case !hasAmt && (plain || blight):
				amountMatters = false
			case !hasAmt:
				add(ccBroken, "RaiseCost Cost$ "+costRaw+" unmodelled (no plain mana/life, not Blight): raises nothing",
					L.loc("rules/statics.go", "mods.raises = append(mods.raises, e.modAmountX(sv, 0, targets))"))
				amountMatters = false
			default:
				add(ccWrong, "RaiseCost Cost$ "+costRaw+" with Amount$ "+amtRaw+": Cost$ ignored, Amount$ priced as generic",
					L.loc("rules/statics.go", "mods.raises = append(mods.raises, e.modAmountX(sv, 0, targets))"))
			}
		}
		if amountMatters {
			locAmt := L.loc("rules/statics.go", "func (e *Engine) modAmountX(")
			if !hasAmt || amtRaw == "" {
				if mode == "RaiseCost" {
					add(ccBroken, "RaiseCost with neither Cost$ nor Amount$ (Forge default 1; engine evaluates \"\" to 0)", locAmt)
				} else {
					add(ccBroken, mode+" without Amount$ (engine evaluates \"\" to 0)", locAmt)
				}
			} else {
				_, ok0, pn0 := evalAmount(sv, 0, nil)
				_, okX, pnX := evalAmount(sv, 2, targets)
				switch {
				case pn0 != "" || pnX != "":
					add(ccBroken, "Amount$ "+amtRaw+" evaluation panicked: "+pn0+pnX, locAmt)
				case !ok0 && !okX:
					body := amtRaw
					if b, ok := f.SVars[amtRaw]; ok {
						body = b
					}
					add(ccBroken, "Amount$ "+amtRaw+" unevaluable by EvalCountOK ("+body+"): modAmountX degrades to 0", locAmt)
				}
				{
					if _, isLit := strconv.Atoi(amtRaw); isLit != nil {
						n0, _, _ := evalAmount(sv, 0, nil)
						nT, _, _ := evalAmount(sv, 0, targets)
						readsT := n0 != nT || svarChainMentions(f.SVars, amtRaw, "Targeted", 0)
						// Only a REDUCTION the offer gate cannot see withholds a
						// castable option; a Relative$ raise is already BROKEN.
						if readsT && mode == "ReduceCost" {
							views := costStaticViews{reduce: []staticView{sv}}
							markCostValidTarget(&views)
							if !views.validTarget {
								add(ccOffer, "Amount$ reads the chosen targets but markCostValidTarget does not flag it: priced with nil targets at the offer gate",
									L.loc("rules/statics.go", "func markCostValidTarget("))
							}
						}
					}
				}
			}
		}
		// Color$ tokens the reducer silently drops.
		if col, ok := p["Color"]; ok && mode == "ReduceCost" {
			var bad []string
			for tok := range strings.FieldsSeq(col) {
				if isDigitRun(tok) || len(tok) == 1 && strings.ContainsRune("WUBRGC", rune(tok[0])) {
					continue
				}
				bad = append(bad, tok)
			}
			if len(bad) > 0 {
				add(ccWrong, "Color$ token(s) "+strings.Join(bad, ",")+" dropped", L.loc("rules/statics.go", "if len(tok) == 1 && strings.ContainsRune(\"WUBRGC\", rune(tok[0])) {"))
			}
		}
		// Unread parameter keys (param census read set).
		readSet := reads.stat[mode]
		var unread []string
		for k := range p {
			if structuralKeys["stat"][k] || ignoredParam("stat:"+mode, k) {
				continue
			}
			switch k {
			case "ClassBand":
				continue // read by classBandGateHolds at costStaticApplies' head
			case "PresentCompare", "PresentZone", "UnlessValidTarget":
				// reported with their semantic consequence above
				if !readSet[k] {
					continue
				}
			}
			if !readSet[k] {
				unread = append(unread, k)
			}
		}
		sort.Strings(unread)
		for _, k := range unread {
			add(ccWrong, "param unread: "+k+"$ "+strings.TrimSpace(p[k]), "param census read set (stat:"+mode+")")
		}
		return out
	}

	// ---- walk the corpus -------------------------------------------------
	rawLine := func(c *cards.Card, st cards.Static) string {
		if kl := st.Params["KeywordLine"]; kl != "" {
			return "K:" + kl
		}
		b, err := os.ReadFile(c.Path)
		if err == nil {
			for ln := range strings.SplitSeq(string(b), "\n") {
				ln = strings.TrimRight(ln, "\r")
				body, ok := strings.CutPrefix(ln, "S:")
				if !ok || !strings.Contains(body, "Mode$ "+st.Mode) {
					continue
				}
				parsed, ok := cards.ParseStaticLines(body)
				if !ok {
					continue
				}
				for _, ps := range parsed {
					match := ps.Mode == st.Mode
					for k, v := range ps.Params {
						if k == "Mode" {
							continue
						}
						if st.Params[k] != v {
							match = false
						}
					}
					if match {
						return ln
					}
				}
			}
		}
		var ks []string
		for k, v := range st.Params {
			ks = append(ks, k+"$ "+v)
		}
		sort.Strings(ks)
		return "[synth] " + strings.Join(ks, " | ")
	}

	var rows []ccRow
	saRows := 0
	for _, c := range reg.Cards {
		playable := len(reg.Unsupported(c, supported)) == 0
		name := c.Faces[0].Name
		for _, f := range c.Faces {
			// Printed / keyword-expanded statics.
			for _, st := range f.Statics {
				if !ccIsCostMode(st.Mode) {
					continue
				}
				delivery := "printed"
				if st.Params["KeywordLine"] != "" {
					delivery = "keyword"
				}
				rows = append(rows, ccRow{card: name, playable: playable, mode: st.Mode, delivery: delivery,
					raw: rawLine(c, st), findings: classify(c, f, st, delivery)})
			}
			// SVar-carried cost statics and their carrier.
			svarStatics := map[string][]cards.Static{}
			for sname, body := range f.SVars {
				if !strings.Contains(body, "Mode$") {
					continue
				}
				if sts, ok := cards.ParseStaticLines(body); ok {
					for _, s := range sts {
						if !ccIsCostMode(s.Mode) {
							continue
						}
						// A body the cards pipeline already expanded into the
						// face's own statics (K:Class level grants carry
						// ClassBand$) is censused there, not twice.
						expanded := false
						for _, fs := range f.Statics {
							if fs.Mode != s.Mode {
								continue
							}
							same := true
							for k, v := range s.Params {
								if fs.Params[k] != v {
									same = false
								}
							}
							if same {
								expanded = true
							}
						}
						if !expanded {
							svarStatics[sname] = append(svarStatics[sname], s)
						}
					}
				}
			}
			if len(svarStatics) == 0 {
				continue
			}
			carriers := map[string]map[string]bool{}
			note := func(name, carrier string) {
				if carriers[name] == nil {
					carriers[name] = map[string]bool{}
				}
				carriers[name][carrier] = true
			}
			scanVals := func(params map[string]string, prefix string) {
				for k, v := range params {
					if k == "Mode" || k == "Description" {
						continue
					}
					for tok := range strings.FieldsFuncSeq(v, func(r rune) bool { return r == ',' || r == ' ' || r == '&' || r == '\t' }) {
						if _, ok := svarStatics[tok]; ok {
							note(tok, prefix+"."+k)
						}
					}
				}
			}
			walkSA(f, func(sa *cards.SA) { scanVals(sa.Params, "api:"+sa.API) })
			for _, st := range f.Statics {
				scanVals(st.Params, "stat:"+st.Mode)
			}
			for sname := range f.SVars {
				// nested SVar statics granting other SVar statics
				if sts, ok := cards.ParseStaticLines(f.SVars[sname]); ok {
					for _, s := range sts {
						scanVals(s.Params, "svarstat:"+s.Mode)
					}
				}
			}
			names := make([]string, 0, len(svarStatics))
			for n := range svarStatics {
				names = append(names, n)
			}
			sort.Strings(names)
			for _, sname := range names {
				var cs []string
				for k := range carriers[sname] {
					cs = append(cs, k)
				}
				sort.Strings(cs)
				for _, st := range svarStatics[sname] {
					raw := "SVar:" + sname + ":" + f.SVars[sname]
					var fs []ccFinding
					delivery := "svar(" + strings.Join(cs, ",") + ")"
					isEffect := carriers[sname]["api:Effect.StaticAbilities"]
					switch {
					case isEffect:
						delivery = "effect"
						if !effects.CostStaticParamsReadable(st.Params) {
							var bad []string
							for k := range st.Params {
								if !effects.CostStaticParamsReadable(map[string]string{k: ""}) {
									bad = append(bad, k)
								}
							}
							sort.Strings(bad)
							fs = append(fs, ccFinding{ccBroken, "Effect-delivered: param(s) " + strings.Join(bad, ",") + " outside CostStaticParamsReadable (loud Note, not registered)",
								L.loc("effects/misc.go", "(mode != \"ManaConvert\" && !CostStaticParamsReadable(params))")})
						}
						fs = append(fs, classify(c, f, st, "effect")...)
					case len(cs) == 0:
						fs = append(fs, ccFinding{ccBroken, "SVar cost static with no recognised carrier", "n/a"})
					default:
						kind := ""
						for _, k := range cs {
							switch {
							case strings.HasSuffix(k, ".AddStaticAbility") || strings.HasSuffix(k, ".AddStaticAbilities"):
								kind = "AddStaticAbility$ grant: layers.go queues only Mode$ Continuous inner statics; a granted cost static is never collected"
								fs = append(fs, ccFinding{ccBroken, "delivery " + k + ": " + kind,
									L.loc("rules/layers.go", "if inner.Mode == \"Continuous\" &&")})
							case strings.HasPrefix(k, "api:Animate") && strings.HasSuffix(strings.ToLower(k), ".staticabilities"):
								fs = append(fs, ccFinding{ccBroken, "delivery " + k + ": registerAnimateStaticAbilities accepts only CantSacrifice/CantBlockUnless/CantAttackUnless (loud Note)",
									L.loc("effects/leavebattlefield.go", "if mode != \"CantSacrifice\" && mode != \"CantBlockUnless\" && mode != \"CantAttackUnless\" {")})
							default:
								fs = append(fs, ccFinding{ccBroken, "delivery " + k + ": no cost-static registration on this carrier", "n/a"})
							}
						}
						// also record what the body itself would hit if it were delivered
						for _, inner := range classify(c, f, st, "svar") {
							inner.reason = "(if delivered) " + inner.reason
							fs = append(fs, inner)
						}
					}
					rows = append(rows, ccRow{card: name, playable: playable, mode: st.Mode, delivery: delivery, raw: raw, findings: fs})
				}
			}
		}
		// Activated abilities' own ReduceCost$.
		for _, f := range c.Faces {
			walkSA(f, func(sa *cards.SA) {
				v, ok := sa.Params["ReduceCost"]
				if !ok {
					return
				}
				saRows++
				v = strings.TrimSpace(v)
				var fs []ccFinding
				locSA := L.loc("rules/legal.go", "if n, ok := effects.EvalCountOK(e, ctx, body); ok && n > 0 {")
				if _, err := strconv.Atoi(v); err != nil {
					body := v
					if b, ok := f.SVars[v]; ok {
						body = b
					}
					var ok0 bool
					pn := probe(func() {
						_, ok0 = effects.EvalCountOK(e, &effects.Ctx{Source: src, Controller: 0, SVars: f.SVars, Targets: targets}, body)
					})
					if pn != "" || !ok0 {
						fs = append(fs, ccFinding{ccBroken, "ReduceCost$ " + v + " unevaluable (" + body + "): ownReduceCost reads 0", locSA})
					}
				}
				if ra, ok := sa.Params["ReduceAmount"]; ok {
					fs = append(fs, ccFinding{ccBroken, "ReduceAmount$ " + ra + " unread: ReduceCost$ " + v + " is a mana cost repeated ReduceAmount times in Forge",
						L.loc("rules/legal.go", "func (e *Engine) ownReduceCost(")})
				}
				rows = append(rows, ccRow{card: name, playable: playable, mode: "SA.ReduceCost$", delivery: "api:" + sa.API,
					raw: sa.Line, findings: fs})
			})
		}
	}

	// ---- report ------------------------------------------------------------
	type agg struct {
		statics int
		cards   map[string]bool
		loc     string
		class   string
		ex      []string
	}
	norm := func(r string) string {
		// Collapse per-card detail into a reason class.
		for _, pfx := range []string{
			"param unread: ", "Condition$ ", "Type$ ", "EffectZone$ ", "ValidSpell$ unsupported: ",
			"ValidSpell$ partially unsupported: ", "delivery ",
		} {
			if strings.HasPrefix(r, pfx) || strings.HasPrefix(r, "(if delivered) "+pfx) {
				// keep the key/value head only
				head := r
				if i := strings.Index(head, ": "); i >= 0 && strings.HasPrefix(pfx, "delivery") {
					head = head[:i]
				}
				if strings.HasPrefix(pfx, "param unread") {
					if i := strings.Index(head, "$"); i >= 0 {
						head = head[:i+1]
					}
				}
				return head
			}
		}
		switch {
		case strings.Contains(r, "Amount$") && strings.Contains(r, "unevaluable"):
			return strings.SplitN(r, " unevaluable", 2)[0][:strings.Index(r, "Amount$")] + "Amount$ unevaluable by EvalCountOK"
		case strings.HasPrefix(r, "ReduceCost$") && strings.Contains(r, "unevaluable"):
			return "SA ReduceCost$ unevaluable"
		case strings.HasPrefix(r, "CheckSVar$"):
			return "CheckSVar$ unevaluable"
		case strings.HasPrefix(r, "ValidCard$ dead"):
			if strings.Contains(r, "base Permanent") {
				return "ValidCard$ dead: Permanent base on a spell"
			}
			return "ValidCard$ dead"
		case strings.HasPrefix(r, "ValidCard$ partially"):
			return "ValidCard$ partially dead"
		case strings.HasPrefix(r, "ValidTarget$ dead"):
			return "ValidTarget$ dead"
		case strings.HasPrefix(r, "IsPresent$ dead"):
			return "IsPresent$ dead"
		case strings.HasPrefix(r, "AffectedZone$") && strings.Contains(r, "ignored for spells"):
			return "AffectedZone$ ignored for spells"
		case strings.HasPrefix(r, "RaiseCost Cost$") && strings.Contains(r, "unmodelled"):
			return "RaiseCost Cost$ unmodelled"
		case strings.HasPrefix(r, "RaiseCost Cost$"):
			return "RaiseCost Cost$+Amount$: Cost$ ignored"
		case strings.HasPrefix(r, "Effect-delivered: param"):
			return "Effect-delivered: param outside whitelist"
		case strings.HasPrefix(r, "Activator$"):
			return strings.SplitN(r, " matches", 2)[0]
		case strings.HasPrefix(r, "ReduceAmount$"):
			return "SA ReduceAmount$ unread (ReduceCost$ is a mana cost)"
		case strings.HasPrefix(r, "Relative$ ReduceCost amount"):
			return "Relative$ ReduceCost amount unresolvable, no {X}"
		case strings.HasPrefix(r, "Secondary$ True skipped") && strings.Contains(r, "no primary"):
			return "Secondary$ True skipped (no primary)"
		case strings.HasPrefix(r, "Secondary$ True skipped") && strings.Contains(r, "gates differ"):
			return "Secondary$ True skipped (gates differ from primary)"
		case strings.HasPrefix(r, "Amount$ reads the chosen targets"):
			return "Amount$ reads targets, not flagged by markCostValidTarget"
		}
		if i := strings.Index(r, " ("); i > 0 && strings.HasPrefix(r, "(if delivered)") {
			return r[:i]
		}
		return r
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.card != b.card {
			return a.card < b.card
		}
		if a.mode != b.mode {
			return a.mode < b.mode
		}
		if a.delivery != b.delivery {
			return a.delivery < b.delivery
		}
		return a.raw < b.raw
	})
	byReason := map[string]*agg{}
	classCards := map[string]map[string]bool{}
	brokenPlayable := map[string]bool{}
	totalStatics := 0
	okStatics := 0
	for _, r := range rows {
		totalStatics++
		if len(r.findings) == 0 {
			okStatics++
		}
		seenR := map[string]bool{}
		for _, f := range r.findings {
			key := f.class + " | " + norm(f.reason)
			a := byReason[key]
			if a == nil {
				a = &agg{cards: map[string]bool{}, loc: f.loc, class: f.class}
				byReason[key] = a
			}
			if !seenR[key] {
				a.statics++
				seenR[key] = true
			}
			if !a.cards[r.card] && len(a.ex) < 6 {
				a.ex = append(a.ex, r.card)
			}
			a.cards[r.card] = true
			if classCards[f.class] == nil {
				classCards[f.class] = map[string]bool{}
			}
			classCards[f.class][r.card] = true
			if f.class == ccBroken && r.playable {
				brokenPlayable[r.card] = true
			}
		}
	}
	keys := make([]string, 0, len(byReason))
	for k := range byReason {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := byReason[keys[i]], byReason[keys[j]]
		if a.class != b.class {
			return a.class < b.class
		}
		if len(a.cards) != len(b.cards) {
			return len(a.cards) > len(b.cards)
		}
		return keys[i] < keys[j]
	})
	var sb strings.Builder
	fmt.Fprintf(&sb, "cost-static census: %d cost statics/SA reducers (%d SA ReduceCost$), %d with no finding\n",
		totalStatics, saRows, okStatics)
	for _, cl := range []string{ccBroken, ccWrong, ccOffer} {
		fmt.Fprintf(&sb, "  %-11s distinct cards: %d\n", cl, len(classCards[cl]))
	}
	fmt.Fprintf(&sb, "  BROKEN cards that make report still counts playable: %d\n", len(brokenPlayable))
	fmt.Fprintf(&sb, "\nper reason (statics / distinct cards / engine location / examples):\n")
	for _, k := range keys {
		a := byReason[k]
		fmt.Fprintf(&sb, "  %4d %4d  %s  @ %s\n        e.g. %s\n", a.statics, len(a.cards), k, a.loc, strings.Join(a.ex, "; "))
	}
	t.Log("\n" + sb.String())

	// Validation cases (log only).
	for _, want := range []string{"Battlefield Thaumaturge", "Dargo, the Shipwrecker"} {
		for _, r := range rows {
			if r.card != want {
				continue
			}
			if len(r.findings) == 0 {
				t.Logf("VALIDATE %s [%s %s]: supported (no finding) -- %s", want, r.mode, r.delivery, r.raw)
			}
			for _, f := range r.findings {
				t.Logf("VALIDATE %s [%s %s]: %s %s @ %s", want, r.mode, r.delivery, f.class, f.reason, f.loc)
			}
		}
	}

	// ---- the ratchet (Ruling R-20's two-direction contract) ----------------
	// BROKEN findings are held against knownBrokenCostStatics; WRONG and
	// OFFER-BLIND findings against knownWrongCostStatics. NOTE findings are
	// log-only. The value is the card's sorted reason classes.
	measured := map[string]map[string]map[string]bool{ccBroken: {}, ccWrong: {}}
	detail := map[string]map[string][]string{ccBroken: {}, ccWrong: {}}
	for _, r := range rows {
		for _, f := range r.findings {
			table := f.class
			label := norm(f.reason)
			switch f.class {
			case ccNote:
				continue
			case ccOffer:
				table = ccWrong
				label = ccOffer + ": " + label
			}
			if measured[table][r.card] == nil {
				measured[table][r.card] = map[string]bool{}
			}
			measured[table][r.card][label] = true
			detail[table][r.card] = append(detail[table][r.card],
				fmt.Sprintf("%s [%s %s] @ %s -- %s", f.reason, r.mode, r.delivery, f.loc, r.raw))
		}
	}
	joined := func(set map[string]bool) string {
		ls := make([]string, 0, len(set))
		for l := range set {
			ls = append(ls, l)
		}
		sort.Strings(ls)
		return strings.Join(ls, "; ")
	}
	for _, tc := range []struct {
		class, name string
		known       map[string]string
	}{
		{ccBroken, "knownBrokenCostStatics", knownBrokenCostStatics},
		{ccWrong, "knownWrongCostStatics", knownWrongCostStatics},
	} {
		got := map[string]string{}
		for card, set := range measured[tc.class] {
			got[card] = joined(set)
		}
		names := make([]string, 0, len(got)+len(tc.known))
		for n := range got {
			names = append(names, n)
		}
		for n := range tc.known {
			if _, ok := got[n]; !ok {
				names = append(names, n)
			}
		}
		sort.Strings(names)
		failed := false
		for _, n := range names {
			g, measuredOK := got[n]
			k, knownOK := tc.known[n]
			switch {
			case measuredOK && !knownOK:
				failed = true
				t.Errorf("%s: %q newly %s: %s\n    %s", tc.name, n, tc.class, g, strings.Join(detail[tc.class][n], "\n    "))
			case !measuredOK && knownOK:
				failed = true
				t.Errorf("%s: %q is stale -- the build no longer measures it %s (table says %q); delete the entry", tc.name, n, tc.class, k)
			case g != k:
				failed = true
				t.Errorf("%s: %q reason classes changed:\n    table:    %s\n    measured: %s\n    %s", tc.name, n, k, g, strings.Join(detail[tc.class][n], "\n    "))
			}
		}
		if failed {
			var lit strings.Builder
			fmt.Fprintf(&lit, "measured %s table (%d cards):\nvar %s = map[string]string{\n", tc.class, len(got), tc.name)
			for _, n := range names {
				if g, ok := got[n]; ok {
					fmt.Fprintf(&lit, "\t%q: %q,\n", n, g)
				}
			}
			lit.WriteString("}\n")
			t.Log(lit.String())
		}
	}

	// TSV, opt-in and outside the repo: GORGE_COST_CENSUS_TSV=1 writes
	// /mnt/sata/gorge-training/tmp/cost-census.tsv.
	if os.Getenv("GORGE_COST_CENSUS_TSV") == "" {
		return
	}
	var tsv strings.Builder
	tsv.WriteString("card\tplayable\tmode\tdelivery\tclass\treason\tlocation\traw\n")
	clean := func(s string) string { return strings.NewReplacer("\t", " ", "\n", " ").Replace(s) }
	for _, r := range rows {
		for _, f := range r.findings {
			fmt.Fprintf(&tsv, "%s\t%v\t%s\t%s\t%s\t%s\t%s\t%s\n", clean(r.card), r.playable, r.mode, clean(r.delivery),
				f.class, clean(f.reason), f.loc, clean(r.raw))
		}
	}
	const out = "/mnt/sata/gorge-training/tmp/cost-census.tsv"
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Logf("mkdir %s: %v", filepath.Dir(out), err)
		return
	}
	if err := os.WriteFile(out, []byte(tsv.String()), 0o644); err != nil {
		t.Logf("write %s: %v", out, err)
		return
	}
	t.Logf("wrote %s", out)
}
