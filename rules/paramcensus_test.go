package rules

// The parameter census (task inbox-engine-ratchet-parameter-census): the
// primitive coverage ratchet in rules/acceptance_test.go only asks the
// registry whether each primitive is REGISTERED. A registered primitive with
// an unconsumed parameter passes -- Reanimate's `GainControl$` counted as
// supported while api:ChangeZone never read the key. This file closes that
// blind spot with a second ratchet over the same repo decks:
//
//   - reads are DERIVED from the code, not hand-listed. A test-time static
//     scan (go/parser over the effects/ and rules/ non-test sources)
//     collects every `.Params["Key"]` access and attributes it to the
//     primitive whose implementation reads it. Attribution roots are derived
//     where the code states them (effects.Register call sites,
//     triggerMatches' Mode$ dispatch switch, activeStatics' literal mode
//     arguments); the few roots the code does not state in a
//     machine-readable position (adjustedCost's apply closure,
//     staticEffects' Continuous filter, the trigger-queue drain,
//     mustAttackRequired's direct scan) are declared in handRoots below --
//     those are ATTRIBUTION (which function to read), never read lists.
//   - the scan is data-flow shaped, not shape-matched: an access counts
//     wherever the Params map is reached -- `x.Params["k"]`, a LOCAL ALIAS
//     (`p := x.Params; p["k"]`), a HELPER-PASSED map (a function whose
//     parameter is a map[string]string, attributed through its call sites'
//     arguments), or a range. An aliased or helper-passed read the scanner
//     cannot attribute fails the rot guard, so a consumer cannot hide by
//     renaming the map.
//   - an unregistered read FAILS the ratchet (the rot guard): any
//     `.Params[...]` access the scanner cannot resolve -- a new dynamic key,
//     a new base variable, a read in a function no root reaches -- breaks
//     TestParamCensusScanIsComplete until it is classified, so the read sets
//     cannot silently rot.
//   - the rules-side generic machinery (cast/activation/legality/targeting)
//     applies to whatever primitive runs, so its SA reads join every api's
//     read set -- but the API-SPECIALISED rules paths (the mana-ability
//     offer/payment/resolution chain, the Charm mode ask/resume, the
//     unless-pay resume for Counter/CopySpellAbility) run only for their own
//     APIs, and apiSpecificRulesSA attributes their reads accordingly: the
//     mana path's `Amount$` read no longer masks api:Sacrifice's unread
//     `Amount$`.
//   - rules.ParseCost reports every token it does not model in Cost.Unknown
//     (rules/mana.go) instead of degrading it silently; the census turns
//     those into `cost:<Token>` labels alongside `param:<Primitive>.<Key>`
//     ones.
//   - knownUnsupportedParams is the seeded baseline over the current repo
//     decks, checked in both directions exactly like knownUnsupported: a
//     card the engine newly cannot support parameter-wise is a regression; a
//     table entry the build now reads is stale and must be deleted. It only
//     ever shrinks, and only when a real read (or a real parse) is added.
//
// Scope: the census walks what cards.Face.Primitives walks (the
// Abilities/Triggers/Statics/Repls plus the SubAbility chains link.go
// resolved) AND every SVar body in each face's own SVar table, and censuses
// only REGISTERED primitives -- an unimplemented primitive is the first
// ratchet's business, and when one registers it automatically comes under
// this census. Keyword heads (kw:...) carry no parameter maps; their
// expansions are censused through the expanded abilities' APIs. The SVar
// walk is load-bearing, not decoration: ResolveSVar's production callers
// execute bodies the Link pass never attached -- Charm's `Choices$` entries
// (effects/misc.go effCharm), Repeat/RepeatEach's `RepeatSubAbility$`
// (effects/choose_control.go), Branch's True/FalseSubAbility$
// (rules/resolution.go) and the delayed trigger's Execute$
// (rules/trigger_match.go) -- so a body an execution-bearing parameter
// names must be censused like a linked sub-ability. The walk is the
// conservative superset: EVERY face SVar is parsed, not only the ones a
// parameter reference can be traced through, so a new execution-bearing
// parameter cannot silently open a blind spot. Ability-less bodies
// (`SVar:X:Count$...` expressions behind ConditionCheckSVar$/SVarCompare$)
// fail parseSA and drop out; cycle protection is ResolveSVar's depth cap
// plus one visit per SVar name.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/internal/testutil"
)

// ---------------------------------------------------------------------------
// 1. The static scan
// ---------------------------------------------------------------------------

// bucket classes a `.Params[...]` access by which card-side structure it
// reads: an SA (the api: primitives' parameter maps, plus the generic
// cast/activation machinery in rules), a trigger, a static, or a
// replacement.
type bucket int

const (
	bSA bucket = iota
	bTrig
	bStat
	bRepl
)

func (b bucket) String() string {
	switch b {
	case bSA:
		return "sa"
	case bTrig:
		return "trig"
	case bStat:
		return "stat"
	default:
		return "repl"
	}
}

// baseBuckets maps the base expression text of a `.Params` selector to its
// bucket for the rules package. In effects every Params map belongs to a
// *cards.SA (the only Params-bearing type any effects function sees), so
// everything there is bSA. Each rules entry below is hand-verified against
// the base's declared type; a name not listed here is UNCLASSIFIED and fails
// the census (the rot guard) rather than being guessed at:
//
//	t       cards.Trigger (trigger-match/queue function parameters)
//	sib     cards.Trigger (a paired sibling trigger: secondaryYields' scan)
//	s,st,sv cards.Static / staticView (static and restriction machinery)
//	r, repl cards.Repl; m.repl the replMatch pair (replacement machinery)
//	sa, ab  *cards.SA parameters; sub the SubAbility$ chain successor;
//	cp, copy, targetSA, SA, Ability, With, head, ma *cards.SA locals;
//	pt.SA   the pendingTrigger's effect SA.
var baseBuckets = map[string]bucket{
	"t": bTrig,
	// sib is the paired sibling trigger secondaryYields (checkFaceTriggers'
	// Secondary$ walk) scans the same face for: a cards.Trigger like t.
	"sib": bTrig,
	// rt is resolveTop's ability branch's findTriggerForAbility result (the
	// trigger the resolving ability was fired from): a cards.Trigger like t,
	// read for OptionalDecider$ (the resolution-time optional gate) and
	// ResolvedLimit$ (the per-turn resolution cap's increment eligibility).
	"rt": bTrig,
	"s":  bStat, "st": bStat, "sv": bStat,
	// pst.Static is manaConversionParts' PileStaticAt element (state.PileStatic
	// -- the merged-under-card static walk): its Static is a cards.Static whose
	// Params (EffectZone$) is the same static parameter map every bStat entry
	// covers.
	"pst.Static": bStat,
	"r":          bRepl, "repl": bRepl, "m.repl": bRepl, "c.repl": bRepl,
	"sa": bSA, "ab": bSA, "sub": bSA, "cp": bSA, "copy": bSA,
	// a is faceWantsConvoked's compiled-ability walk (the face's Abilities
	// slice): each element is a *cards.SA whose Defined$ parameter the
	// Convoked provenance gate reads -- the same cards.SA parameter map
	// every bSA entry covers.
	"a": bSA, "targetSA": bSA, "SA": bSA, "Ability": bSA, "With": bSA,
	"head": bSA, "ma": bSA, "mana": bSA, "original": bSA, "pt.SA": bSA,
	// alt.ma is payment-plan's one concrete mana-ability alternative.  It is
	// a *cards.SA just like ma; the qualified name makes that type explicit
	// to the census rather than allowing this planner read to evade it.
	"alt.ma": bSA,
	// source.original is the attack window's choice-shaped mana source's
	// compiled pile ability (attackManaSource.original, a *cards.SA like the
	// bare "original" entry): the targeted-equip window probe (cast.go
	// castWindowUnits) re-prices its Produced$ against the chosen
	// colour, reading the same SA parameter map every bSA entry covers.
	"source.original": bSA,
	// spell.Ability is the stack object's resolved *cards.SA, checked before
	// falling back to its printed face in TargetableObjects.
	"spell.Ability": bSA,
	// rsub is runPreventionShieldRider's rewritten copy of the
	// PreventionSubAbility$ rider (a shallow copy of a fresh ResolveSVar
	// parse, whose NumDmg$/Defined$ the shield application binds): a
	// *cards.SA value like the sub it copies.
	"rsub": bSA,
	// m.ability is manaUnlessActivation's resolved *cards.SA — the ability
	// whose activation cost/UnlessCost$ the off-stack mana-activation
	// window reads (resolveManaEffect / askManaUnless / the settle path).
	"m.ability": bSA,
	// ma.ability is manaColorActivation's resolved *cards.SA — the paid
	// mana ability whose Cost$ (its T part) answerNestedManaColor re-binds
	// as manaFromTap when a routed SubAbility$ colour answer re-enters.
	"ma.ability": bSA,
	// selector bases: r.With and m.repl.With are cards.Repl's resolved
	// With *cards.SA (the ReplaceWith$ body: a real SA parameter map, read
	// as generic machinery), rp.sa the resume plan's SA, o.Ability the
	// stack object's resolved SA, and d.ResumeSA the pending decision's
	// resume SA (validateSearch's ShareLandType$ read — the same
	// cards.SA the "search" resume arm re-enters). "body" is the same
	// resolved ReplaceWith$ body under its local name in the CreateToken
	// replacement dispatcher (continueCreateTokenReplacements /
	// applyTokenReplacementToPlan read its Type$/Amount$/TokenScript$).
	"r.With": bSA, "m.repl.With": bSA, "with": bSA, "rp.sa": bSA, "o.Ability": bSA, "d.ResumeSA": bSA, "body": bSA,
	// repl.With is the attached-replacement dispatcher's local *cards.Repl
	// (the `r` it captured from replacementFace's scan), whose resolved With
	// *cards.SA it reads for ChooseName's ValidCards$ pool -- the same
	// cards.SA parameter map r.With covers.
	"repl.With": bSA,
	// offeredSA is resolveTop's ability-branch marker derivation: the SA
	// whose ValidTgts$ the placement ask actually covered -- o.Ability for a
	// non-modal trigger, the first target-bearing chosen mode's sub for a
	// modal one. The same cards.SA parameter map, so the same bucket as
	// o.Ability.
	"offeredSA": bSA,
	// root is the modal spell's root SpellAbility -- cast.go's
	// f.SpellAbility() local and askCharmModeTargets' `root *cards.SA`
	// parameter -- whose Choices$ the distinct-mode Charm target ask reads.
	// A *cards.SA parameter map exactly like every other bSA entry.
	"root": bSA,
	// hsa is rules/split.go's fusedHalfTargets' per-half root spell ability
	// (the loop local for halves[i].SpellAbility(), read for the half's
	// ValidTgts$ spec and targetZones): a *cards.SA parameter map exactly
	// like the sa it mirrors.
	"hsa": bSA,
	// so.Ability is handleModes' placement branch's stack object (the local
	// name for the same stack object o.Ability reads): the trigger Charm's
	// resolved SA, whose full Choices$ list classifies the cross-mode
	// TargetUnique family (effects.CharmCrossModeShape).
	"so.Ability": bSA,
	// index bases: candidates/rc.cands/matches are all []replMatch (the
	// phase-replacement pipeline, its parked-choice resume, and the
	// damage/counter/effect-created replacement match lists), so element
	// .repl is the same cards.Repl pair the named bases read.
	"candidates[i].repl": bRepl, "rc.cands[i].repl": bRepl, "matches[i].repl": bRepl,
}

// callSite records one package-local call with its string-literal arguments
// (for `.Params[paramIdent]` resolution) -- args[i] is "" when argument i is
// not a string literal.
type callSite struct {
	callee string
	args   []string
	// exprs renders each argument expression's text ("c.SVars",
	// "sa.Params", a plain identifier) for map[string]string-parameter
	// attribution: args[i] is the rendered text, "" when not renderable.
	exprs []string
}

// fnInfo is one function's scanned shape. name is package-qualified
// ("rules:Engine.triggerMatches", "effects:effDealDamage"); methods keep
// their receiver type ("Engine.triggerMatches").
type fnInfo struct {
	pkg       string
	name      string
	reads     map[bucket]map[string]bool
	keyRds    map[bucket]map[string]bool
	calls     map[string]bool
	callSites []callSite
	sig       []string
	strParams map[string]int
	// mapParams records parameters whose declared type is map[string]string
	// (the Params maps' type): an index read on one is a potential Params
	// read attributed through the call sites' arguments, never silently
	// dropped.
	mapParams map[string]int
	// mapIdxRds records literal keys indexed on a mapParams parameter
	// ("" marks a dynamic key, which cannot be attributed and fails).
	mapIdxRds map[string]map[string]bool
	// stringMapResults holds this function's result POSITIONS typed
	// map[string]string, so a local assigned from a call can be tracked.
	stringMapResults []int
	// aliases maps a local name to the base text of the Params map it was
	// assigned (`params := sa.Params` -> "sa", chains resolved).
	aliases map[string]string
}

type scan struct {
	fns     map[string]*fnInfo
	apiImpl map[string]string // api -> effects function name (from Register)
	// statRoots[mode] = functions whose code calls activeStatics("mode") or
	// assignmentStatics("mode") -- the two literal-mode stat collectors.
	statRoots map[string]map[string]bool
	// modeFns[trigMode] = the dispatch function triggerMatches calls for it;
	// dispatchFns is the set of all dispatch callees (excluded from the
	// SHARED trigger read set so each mode only inherits its own branch).
	modeFns      map[string]string
	dispatchFns  map[string]bool
	unclassified []string
	// guardErrs carries rotGuard's findings; failGuard turns them into test
	// failures, and the probes inspect the list directly.
	guardErrs []string
	// mapAttributed records which map[string]string parameters had at least
	// one attributed call site ("pkg:fn:param"), set by propagateKeyReads.
	mapAttributed map[string]bool
	// mapArgFailures collects map[string]string-parameter call sites whose
	// argument could not be attributed to a Params map, resolved during
	// propagateKeyReads and surfaced by rotGuard.
	mapArgFailures []string
	// ambiguousStat records activeStatics calls whose mode is not a literal,
	// for rotGuard to reconcile against handRoots.stat.
	ambiguousStat map[string][]string
	// complete marks a scan over the REAL packages (scanPackages): the
	// apiSpecificRulesSA staleness check only makes sense there -- a probe's
	// synthetic scan deliberately contains none of the real functions.
	complete bool
}

// newScan builds an empty scan; scanPackages and the synthetic-source probes
// both start here.
func newScan() *scan {
	return &scan{
		fns:         map[string]*fnInfo{},
		apiImpl:     map[string]string{},
		statRoots:   map[string]map[string]bool{},
		modeFns:     map[string]string{},
		dispatchFns: map[string]bool{},
	}
}

// scanPackages parses the non-test sources of the rules package (dir ".",
// where a rules test binary runs) and the effects package ("../effects").
func scanPackages(t *testing.T) *scan {
	t.Helper()
	s := newScan()
	for _, spec := range []struct{ dir, pkg string }{
		{dir: ".", pkg: "rules"},
		{dir: "../effects", pkg: "effects"},
	} {
		files, err := filepath.Glob(filepath.Join(spec.dir, "*.go"))
		if err != nil || len(files) == 0 {
			t.Fatalf("paramcensus: glob %s: %v", spec.dir, err)
		}
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("paramcensus: read %s: %v", f, err)
			}
			s.scanSource(t, f, spec.pkg, string(src))
		}
	}
	s.propagateKeyReads()
	s.complete = true
	return s
}

// scanSource scans one compilation unit from its bytes -- the real packages'
// files and the probes' synthetic sources all come through here. Pass one
// declares every function's signature shape (string params, map[string]string
// params, map[string]string result positions) so pass two can classify
// aliases, helper-passed maps and call-returned maps without declaration-order
// dependence; pass two collects reads, calls and attribution roots.
func (s *scan) scanSource(t *testing.T, path, pkg, src string) {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, path, src, 0)
	if err != nil {
		t.Fatalf("paramcensus: parse %s: %v", path, err)
	}
	for _, decl := range af.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fi := s.fnOf(pkg, s.funcName(fd))
		position := 0
		for _, p := range fd.Type.Params.List {
			for _, pn := range p.Names {
				fi.sig = append(fi.sig, pn.Name)
				if pt, ok := p.Type.(*ast.Ident); ok && pt.Name == "string" {
					fi.strParams[pn.Name] = position
				}
				if isStringMap(p.Type) {
					fi.mapParams[pn.Name] = position
				}
				position++
			}
			if len(p.Names) == 0 {
				position++
			}
		}
		if fd.Type.Results != nil {
			rpos := 0
			for _, r := range fd.Type.Results.List {
				if isStringMap(r.Type) {
					for range max(1, len(r.Names)) {
						fi.stringMapResults = append(fi.stringMapResults, rpos)
						rpos++
					}
				}
				rpos += max(1, len(r.Names))
			}
		}
	}
	for _, decl := range af.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		s.scanFuncBody(t, fset, fd, pkg)
	}
}

// funcName renders a declaration's name the way fnInfo keys it: methods keep
// their receiver type ("Engine.triggerMatches").
func (s *scan) funcName(fd *ast.FuncDecl) string {
	name := fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		if star, ok := fd.Recv.List[0].Type.(*ast.StarExpr); ok {
			if id, ok := star.X.(*ast.Ident); ok {
				name = id.Name + "." + name
			}
		}
	}
	return name
}

// isStringMap reports whether the type expression is exactly
// map[string]string -- the type of every cards Params map.
func isStringMap(e ast.Expr) bool {
	mt, ok := e.(*ast.MapType)
	if !ok {
		return false
	}
	k, kok := mt.Key.(*ast.Ident)
	v, vok := mt.Value.(*ast.Ident)
	return kok && vok && k.Name == "string" && v.Name == "string"
}

func (s *scan) failf(t *testing.T, pos token.Position, format string, args ...any) {
	s.unclassified = append(s.unclassified, fmt.Sprintf("%s: %s", pos, fmt.Sprintf(format, args...)))
}

func (s *scan) fnOf(pkg, name string) *fnInfo {
	key := pkg + ":" + name
	if fi, ok := s.fns[key]; ok {
		return fi
	}
	fi := &fnInfo{
		pkg: pkg, name: name,
		reads:     map[bucket]map[string]bool{},
		keyRds:    map[bucket]map[string]bool{},
		calls:     map[string]bool{},
		strParams: map[string]int{},
		mapParams: map[string]int{},
		aliases:   map[string]string{},
	}
	s.fns[key] = fi
	return fi
}

func (s *scan) addRead(fi *fnInfo, b bucket, key string) {
	if fi.reads[b] == nil {
		fi.reads[b] = map[string]bool{}
	}
	fi.reads[b][key] = true
}

func (s *scan) addKeyRead(fi *fnInfo, b bucket, param string) {
	if fi.keyRds[b] == nil {
		fi.keyRds[b] = map[string]bool{}
	}
	fi.keyRds[b][param] = true
}

// exprText renders the base expression of a .Params selector for bucket
// lookup: plain identifiers and selector/index chains of any depth
// (m.repl, pt.SA, m.repl.With, rc.cands[i].repl). An index expression
// renders with a fixed "[i]" placeholder: the bucket depends on the ELEMENT
// type (which slice it indexes), never on which element, so one table entry
// covers every index. A chain whose head is not renderable (a call result,
// a composite) renders "" and fails the rot guard.
func exprText(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		if x := exprText(v.X); x != "" {
			return x + "." + v.Sel.Name
		}
	case *ast.IndexExpr:
		if x := exprText(v.X); x != "" {
			return x + "[i]"
		}
	}
	return ""
}

// scanFile parses one source file: signatures, direct reads, calls, call-site
// literals, Register/activeStatics/dispatch-switch attribution, and the
// range-over-Params whitelist pattern (see scanRangeWhitelist).
// scanFuncBody is pass two for one function: writes, the Params-alias
// pre-pass, then reads/calls/roots. Aliases and helper-passed maps are
// classified here so a Params read cannot hide behind a renamed map.
func (s *scan) scanFuncBody(t *testing.T, fset *token.FileSet, fd *ast.FuncDecl, pkg string) {
	name := s.funcName(fd)
	fi := s.fnOf(pkg, name)
	// Assignment LHS index expressions are writes (`copy.Params["Produced"] = c`),
	// never reads.
	writes := map[ast.Node]bool{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range as.Lhs {
			if _, ok := lhs.(*ast.IndexExpr); ok {
				writes[lhs] = true
			}
		}
		return true
	})
	// Alias pre-pass: every local assigned a `.Params` selector (directly or
	// through another alias) becomes an alias; locals assigned another
	// selector's map, a map[string]string composite/make, or a call whose
	// result at that position is a map[string]string are tracked so an index
	// READ on them is classified rather than silently dropped.
	otherAliases := map[string]bool{}
	localStringMaps := map[string]bool{}
	s.collectAliases(fd.Body, fi, otherAliases, localStringMaps)
	s.scanDispatchSwitch(t, fset, fd, pkg)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.IndexExpr:
			if writes[v] {
				return true
			}
			switch xb := v.X.(type) {
			case *ast.SelectorExpr:
				if xb.Sel.Name != "Params" {
					return true // .SVars and every other map field: not a param read
				}
				s.recordParamsRead(t, fset, fi, name, v, exprText(xb.X), pkg)
			case *ast.Ident:
				if base, aliased := fi.aliases[xb.Name]; aliased {
					s.recordParamsRead(t, fset, fi, name, v, base, pkg)
					return true
				}
				if _, isMapParam := fi.mapParams[xb.Name]; isMapParam {
					s.recordMapParamRead(t, fset, fi, name, v, xb.Name)
					return true
				}
				if localStringMaps[xb.Name] {
					s.failf(t, fset.Position(v.Pos()),
						"%s: index read on local map[string]string %q that is not a tracked Params alias -- classify it", name, xb.Name)
					return true
				}
				if otherAliases[xb.Name] {
					return true // an alias of a non-Params map field: not a param read
				}
				// Anything else indexed under a plain identifier is a slice,
				// array or non-string map -- never a Params map (whose only
				// shapes are the selector, alias, helper-param and local
				// forms classified above).
				return true
			default:
				// A call result (or composite) indexed directly is a slice or a
				// non-Params map -- never a Params read (a call returning an SA
				// reaches the map only through a `.Params` selector, which the
				// case above classifies). One exception fails loudly: a call
				// whose position-0 result is itself a map[string]string must
				// not be indexed without classification.
				if ce, ok := v.X.(*ast.CallExpr); ok {
					if id, ok := ce.Fun.(*ast.Ident); ok {
						if target := s.fns[fi.pkg+":"+id.Name]; target != nil && slices.Contains(target.stringMapResults, 0) {
							s.failf(t, fset.Position(v.Pos()),
								"%s: indexes the map[string]string result of %s directly -- assign it to a tracked local or classify it", name, id.Name)
						}
					}
				}
				return true
			}
			return true
		case *ast.CallExpr:
			s.scanCall(t, fset, fi, name, v, pkg)
			return true
		case *ast.RangeStmt:
			s.scanRangeWhitelist(t, fset, fi, name, v, pkg, writes)
			return true
		}
		return true
	})
}

// recordParamsRead records one `.Params[...]` (or alias) access on the given
// base text -- the shared read logic for selector and aliased forms.
func (s *scan) recordParamsRead(t *testing.T, fset *token.FileSet, fi *fnInfo, name string, v *ast.IndexExpr, base, pkg string) {
	b, ok := s.bucketOf(t, fset, v.Pos(), base, pkg)
	if !ok {
		return
	}
	switch idx := v.Index.(type) {
	case *ast.BasicLit:
		if idx.Kind != token.STRING {
			s.failf(t, fset.Position(v.Pos()), "%s: non-string literal Params key %s", name, idx.Value)
			return
		}
		key, err := strconv.Unquote(idx.Value)
		if err != nil {
			s.failf(t, fset.Position(v.Pos()), "%s: unparseable Params key %s", name, idx.Value)
			return
		}
		s.addRead(fi, b, key)
	case *ast.Ident:
		if _, isParam := fi.strParams[idx.Name]; isParam {
			s.addKeyRead(fi, b, idx.Name)
			return
		}
		s.failf(t, fset.Position(v.Pos()),
			"%s: dynamic Params key %q that is not a function parameter -- resolve it via a parameter or classify it", name, idx.Name)
	default:
		s.failf(t, fset.Position(v.Pos()), "%s: unclassifiable Params key expression %T", name, v.Index)
	}
}

// recordMapParamRead records an index on a map[string]string parameter (the
// helper-passed-map form). Literal keys are attributed through the call
// sites' arguments in propagateKeyReads; a dynamic key cannot be attributed
// and fails. Whitelisted non-card maps (parseStaticLine's SVar table, ...) skip
// attribution entirely.
func (s *scan) recordMapParamRead(t *testing.T, fset *token.FileSet, fi *fnInfo, name string, v *ast.IndexExpr, param string) {
	if _, skipped := stringMapParams[fi.pkg+":"+fi.name+":"+param]; skipped {
		return
	}
	var key string
	switch idx := v.Index.(type) {
	case *ast.BasicLit:
		if idx.Kind == token.STRING {
			if k, err := strconv.Unquote(idx.Value); err == nil {
				key = k
			}
		}
		if key == "" {
			s.failf(t, fset.Position(v.Pos()), "%s: non-string literal key on map[string]string parameter %q", name, param)
			return
		}
	case *ast.Ident:
		if _, isStr := fi.strParams[idx.Name]; isStr {
			// A dynamic key taken from a string parameter: the read is real
			// but its key is only known at the call sites, where BOTH the
			// key argument and the map argument must be attributed.
			key = ""
		} else {
			s.failf(t, fset.Position(v.Pos()),
				"%s: dynamic key %q on map[string]string parameter %q is not a function parameter -- classify it", name, idx.Name, param)
			return
		}
	default:
		s.failf(t, fset.Position(v.Pos()), "%s: unclassifiable key expression on map[string]string parameter %q", name, param)
		return
	}
	if fi.mapIdxRds == nil {
		fi.mapIdxRds = map[string]map[string]bool{}
	}
	if fi.mapIdxRds[param] == nil {
		fi.mapIdxRds[param] = map[string]bool{}
	}
	fi.mapIdxRds[param][key] = true
}

// collectAliases finds the Params-alias and local-string-map locals of one
// function body: single assignments (`x := y.Params`, `x := y.SVars`,
// `x := otherAlias`, `x := make(map[string]string...)`,
// `x := map[string]string{...}`, `x := f(...)` with a map[string]string
// result at that position) and var declarations of the same shapes.
func (s *scan) collectAliases(body *ast.BlockStmt, fi *fnInfo, otherAliases, localStringMaps map[string]bool) {
	track := func(lhs *ast.Ident, rhs ast.Expr) {
		rhs = ast.Unparen(rhs)
		switch r := rhs.(type) {
		case *ast.SelectorExpr:
			if r.Sel.Name == "Params" {
				fi.aliases[lhs.Name] = exprText(r.X)
			} else {
				otherAliases[lhs.Name] = true
			}
		case *ast.Ident:
			if base, ok := fi.aliases[r.Name]; ok {
				fi.aliases[lhs.Name] = base
			} else if otherAliases[r.Name] {
				otherAliases[lhs.Name] = true
			}
		case *ast.CompositeLit:
			if isStringMap(r.Type) {
				localStringMaps[lhs.Name] = true
			}
		case *ast.CallExpr:
			if callee, ok := r.Fun.(*ast.Ident); ok {
				if target := s.fns[fi.pkg+":"+callee.Name]; target != nil {
					for _, pos := range target.stringMapResults {
						if pos == 0 {
							localStringMaps[lhs.Name] = true
						}
					}
				}
			}
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.AssignStmt:
			if len(v.Rhs) == 1 && len(v.Lhs) == 1 {
				if id, ok := v.Lhs[0].(*ast.Ident); ok && (v.Tok == token.DEFINE || v.Tok == token.ASSIGN) {
					track(id, v.Rhs[0])
				}
			} else if len(v.Rhs) == 1 && len(v.Lhs) > 1 && v.Tok == token.DEFINE {
				// Multi-assign from one call: a map[string]string result at
				// position i makes LHS i a tracked local string map.
				if ce, ok := ast.Unparen(v.Rhs[0]).(*ast.CallExpr); ok {
					if callee, ok := ce.Fun.(*ast.Ident); ok {
						if target := s.fns[fi.pkg+":"+callee.Name]; target != nil {
							for _, pos := range target.stringMapResults {
								if pos < len(v.Lhs) {
									if id, ok := v.Lhs[pos].(*ast.Ident); ok {
										localStringMaps[id.Name] = true
									}
								}
							}
						}
					}
				}
			}
		case *ast.GenDecl:
			for _, spec := range v.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Values) != len(vs.Names) {
					continue
				}
				for i, val := range vs.Values {
					if id := vs.Names[i]; id != nil {
						track(id, val)
					}
				}
			}
		}
		return true
	})
}

// scanDispatchSwitch derives the trigger Mode$ dispatch from the
// registerTrigMatcher calls the per-mode trigmatch_*.go files make: mode
// literal -> the matcher registered for it. It used to read Engine.
// triggerMatches' switch; that switch became a table when the modes were split
// into their own files, so the same fact is now read from the registration.
// Hand-listing the mode functions here would be exactly the hand list this
// census must not keep.
//
// Two registration shapes carry a callee:
//
//	registerTrigMatcher((*Engine).zoneChangeMatches, "ChangesZone", ...)
//	registerTrigMatcher(func(e *Engine, ...) bool { return e.attacksMatches(...) }, "Attacks")
//
// A func literal that calls nothing (Mode$ Always returns true inline) reads
// only the shared set, exactly as its switch arm did.
func (s *scan) scanDispatchSwitch(t *testing.T, fset *token.FileSet, fd *ast.FuncDecl, pkg string) {
	if pkg != "rules" {
		return
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		ce, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := ce.Fun.(*ast.Ident)
		if !ok || id.Name != "registerTrigMatcher" || len(ce.Args) < 2 {
			return true
		}
		var modes []string
		for _, a := range ce.Args[1:] {
			if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil {
					modes = append(modes, v)
				}
			}
		}
		if len(modes) == 0 {
			s.failf(t, fset.Position(ce.Pos()), "registerTrigMatcher call names no mode literal")
			return false
		}
		callee := trigMatcherCallee(ce.Args[0])
		if callee == "" {
			if len(modes) == 1 && modes[0] == "Always" {
				// Always' matcher returns true inline; its reads are the
				// shared set, as its switch arm's were.
				return false
			}
			s.failf(t, fset.Position(ce.Pos()), "registerTrigMatcher for %v names no local function", modes)
			return false
		}
		for _, m := range modes {
			if m != "Always" {
				s.modeFns[m] = callee
			}
			s.dispatchFns[callee] = true
		}
		return false
	})
}

// trigMatcherCallee names the matcher a registerTrigMatcher first argument
// installs: the method of a method expression, or the first Engine method a
// func literal calls.
func trigMatcherCallee(arg ast.Expr) string {
	// (*Engine).zoneChangeMatches
	if sel, ok := arg.(*ast.SelectorExpr); ok {
		if _, isParen := sel.X.(*ast.ParenExpr); isParen {
			return "Engine." + sel.Sel.Name
		}
	}
	fl, ok := arg.(*ast.FuncLit)
	if !ok {
		return ""
	}
	var callee string
	ast.Inspect(fl.Body, func(m ast.Node) bool {
		if callee != "" {
			return false
		}
		ce, ok := m.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := ce.Fun.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "e" {
				callee = "Engine." + sel.Sel.Name
				return false
			}
		}
		if id, ok := ce.Fun.(*ast.Ident); ok {
			callee = id.Name
			return false
		}
		return true
	})
	return callee
}

// scanCall records package-local calls (with literal args for key
// propagation) and the attribution roots the code states: effects.Register
// and rules' activeStatics/assignmentStatics. FuncLit bodies are attributed
// to the enclosing function by the caller's Inspect, so closures like
// adjustedCost's apply participate here.
func (s *scan) scanCall(t *testing.T, fset *token.FileSet, fi *fnInfo, fname string, ce *ast.CallExpr, pkg string) {
	var callee string
	switch fun := ce.Fun.(type) {
	case *ast.Ident:
		callee = fun.Name
	case *ast.SelectorExpr:
		if id, ok := fun.X.(*ast.Ident); ok {
			if id.Name == "e" && pkg == "rules" {
				callee = "Engine." + fun.Sel.Name
			} else if strings.HasPrefix(fname, id.Name+".") {
				// a method calling another method on the same receiver
				callee = id.Name + "." + fun.Sel.Name
			} else {
				return // cross-package (effects.X) or unrelated selector: not local
			}
		} else {
			return
		}
	default:
		return
	}
	if (callee == "Engine.activeStatics" || callee == "Engine.assignmentStatics") && pkg == "rules" {
		if len(ce.Args) > 0 {
			if lit, ok := ce.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if mode, err := strconv.Unquote(lit.Value); err == nil {
					if s.statRoots[mode] == nil {
						s.statRoots[mode] = map[string]bool{}
					}
					s.statRoots[mode][fname] = true
					// assignmentStatics is a collector LIKE activeStatics, so
					// its own stat-param reads (the EffectZone$ gate) are
					// attributed through this call edge into the caller's mode
					// bucket (activeStatics is a handRoots.stat entry instead).
					if callee == "Engine.assignmentStatics" {
						fi.calls[callee] = true
					}
					return
				}
			}
		}
		if s.ambiguousStat == nil {
			s.ambiguousStat = map[string][]string{}
		}
		s.ambiguousStat[fname] = append(s.ambiguousStat[fname], fset.Position(ce.Pos()).String())
		return
	}
	fi.calls[callee] = true
	if callee == "Register" && pkg == "effects" && len(ce.Args) == 2 {
		if lit, ok := ce.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if api, err := strconv.Unquote(lit.Value); err == nil {
				if id, ok := ce.Args[1].(*ast.Ident); ok {
					s.apiImpl[api] = id.Name
				}
			}
		}
	}
	if len(ce.Args) == 0 {
		return
	}
	args := make([]string, len(ce.Args))
	exprs := make([]string, len(ce.Args))
	for i, a := range ce.Args {
		exprs[i] = argText(a)
		if lit, ok := a.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if v, err := strconv.Unquote(lit.Value); err == nil {
				args[i] = v
			}
		}
	}
	fi.callSites = append(fi.callSites, callSite{callee: callee, args: args, exprs: exprs})
}

// argText renders an argument expression for map[string]string-parameter
// attribution: plain identifiers, one-level selector chains and parenthesised
// forms of those; anything else renders "" (unattributable, and the rot
// guard says so when it is a map argument).
func argText(e ast.Expr) string {
	switch v := ast.Unparen(e).(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			return x.Name + "." + v.Sel.Name
		}
	}
	return ""
}

// scanRangeWhitelist handles `for k := range X.Params` shapes (X a selector
// base or a tracked alias): a range over a Params map whose body switches on
// the range variable is a READ of the keys named in the case clauses (the
// whitelist). DigUntil's two withheld-parameter prefix scans are classified
// separately: recognition followed only by a Note is not a semantic read.
// Any other range shape is a rot-guard failure.
func (s *scan) scanRangeWhitelist(t *testing.T, fset *token.FileSet, fi *fnInfo, fname string, rs *ast.RangeStmt, pkg string, writes map[ast.Node]bool) {
	var base string
	switch x := rs.X.(type) {
	case *ast.SelectorExpr:
		if x.Sel.Name != "Params" {
			return
		}
		base = exprText(x.X)
	case *ast.Ident:
		if b, ok := fi.aliases[x.Name]; ok {
			base = b
		} else {
			return // range over some other map: not a Params read
		}
	default:
		return
	}
	b, ok := s.bucketOf(t, fset, rs.X.Pos(), base, pkg)
	if !ok {
		return
	}
	keyIdent, ok := rs.Key.(*ast.Ident)
	if !ok {
		s.failf(t, fset.Position(rs.X.Pos()), "%s: range over Params without an index variable", fname)
		return
	}
	var keys []string
	switched := false
	ast.Inspect(rs.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		tag, ok := sw.Tag.(*ast.Ident)
		if !ok || tag.Name != keyIdent.Name {
			return true
		}
		switched = true
		for _, c := range sw.Body.List {
			cc, ok := c.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cc.List {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if v, err := strconv.Unquote(lit.Value); err == nil {
						keys = append(keys, v)
					}
				}
			}
		}
		return false
	})
	if !switched {
		// saMentionsGoaded recognizes IsGoaded in any inline filter value so
		// effects can install the matching filter resolver. Recognition only;
		// it does not consume any SA parameter.
		if pkg == "effects" && fname == "saMentionsGoaded" {
			return
		}
		// A copy loop (`for k, v := range src.Params { dst.Params[k] = v }`)
		// is not a read: every use of the key sits in a write-position index.
		if rangeKeyIsWriteOnly(rs, keyIdent.Name, writes) {
			return
		}
		// A FILTERED copy (`for k, v := range src.Params { if k != "X" { dst.Params[k] = v } }`)
		// is likewise not a read: it deliberately omits one already-resolved
		// key (DamageSource, resolved once by the caller) and copies every
		// other key into a new SA. Matched by shape, not by function name, so
		// extracting or renaming the helper cannot silently turn the copy into
		// an unclassified read.
		if filteredParamsCopy(rs, keyIdent.Name, writes) {
			return
		}
		s.failf(t, fset.Position(rs.X.Pos()),
			"%s: range over Params is neither the case-whitelist nor a copy shape -- classify it", fname)
		return
	}
	for _, k := range keys {
		s.addRead(fi, b, k)
	}
}

// filteredParamsCopy accepts a Params copy loop that deliberately omits one
// or more already-resolved keys and copies every other key/value into a new
// SA map. Two body shapes are accepted (the code has carried both across
// refactors):
//
//	for k, v := range src.Params { if k != "X" { dst.Params[k] = v } }
//	for k, v := range src.Params { if k == "X" { continue }; dst.Params[k] = v }
//
// Recognition is by SHAPE, not by function name: the key must appear only in
// (a) comparison guards against a constant string, and (b) write-position
// index expressions `dst.Params[k]` that are assignments. Every such
// assignment must copy the range value straight through. The copy consumes no
// parameter, so it contributes no read; any other use of the key falls through
// to the rot guard.
func filteredParamsCopy(rs *ast.RangeStmt, key string, writes map[ast.Node]bool) bool {
	if key != "k" {
		return false
	}
	wrote := false
	for _, stmt := range rs.Body.List {
		switch v := stmt.(type) {
		case *ast.IfStmt:
			// A guard that compares the key against a constant string and
			// either copies inside or `continue`s. Anything else rejects.
			if v.Else != nil || !keyConstCompare(v.Cond, key) {
				return false
			}
			if len(v.Body.List) != 1 {
				return false
			}
			if br, ok := v.Body.List[0].(*ast.BranchStmt); ok {
				if br.Tok != token.CONTINUE {
					return false
				}
				continue // `if k == "X" { continue }` filter
			}
			if !keyCopyAssign(v.Body.List[0], key, writes) {
				return false
			}
			wrote = true
		case *ast.AssignStmt:
			if !keyCopyAssign(v, key, writes) {
				return false
			}
			wrote = true
		default:
			return false
		}
	}
	return wrote
}

// keyCopyAssign reports whether stmt is `dst.Params[k] = v` (the key in
// write-position index, the range value copied straight through).
func keyCopyAssign(stmt ast.Stmt, key string, writes map[ast.Node]bool) bool {
	as, ok := stmt.(*ast.AssignStmt)
	if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return false
	}
	ix, ok := as.Lhs[0].(*ast.IndexExpr)
	if !ok || !writes[ix] {
		return false
	}
	id, ok := ix.Index.(*ast.Ident)
	if !ok || id.Name != key {
		return false
	}
	val, ok := as.Rhs[0].(*ast.Ident)
	return ok && val.Name == "v"
}

// keyConstCompare reports whether cond is a `key == "X"` / `key != "X"`
// comparison of the range key against a constant string, with no other use of
// the key.
func keyConstCompare(cond ast.Expr, key string) bool {
	be, ok := cond.(*ast.BinaryExpr)
	if !ok || (be.Op != token.EQL && be.Op != token.NEQ) {
		return false
	}
	id, ok := be.X.(*ast.Ident)
	if !ok || id.Name != key {
		return false
	}
	lit, ok := be.Y.(*ast.BasicLit)
	return ok && lit.Kind == token.STRING
}

// rangeKeyIsWriteOnly reports whether the range body is a pure Params copy
// loop: every statement is `dst.Params[key] = v` (a collected write-position
// index) and the key never appears on any right-hand side.
func rangeKeyIsWriteOnly(rs *ast.RangeStmt, key string, writes map[ast.Node]bool) bool {
	usesKey := func(e ast.Expr) bool {
		found := false
		ast.Inspect(e, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == key {
				found = true
			}
			return !found
		})
		return found
	}
	for _, stmt := range rs.Body.List {
		as, ok := stmt.(*ast.AssignStmt)
		if !ok || as.Tok != token.ASSIGN || len(as.Lhs) != 1 {
			return false
		}
		ix, ok := as.Lhs[0].(*ast.IndexExpr)
		if !ok || !writes[ix] {
			return false
		}
		id, ok := ix.Index.(*ast.Ident)
		if !ok || id.Name != key {
			return false
		}
		for _, rhs := range as.Rhs {
			if usesKey(rhs) {
				return false
			}
		}
	}
	return true
}

func cloneKeySet(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	for k := range src {
		out[k] = true
	}
	return out
}

// bucketOf classes a Params base. effects: everything is an SA map. rules:
// the verified baseBuckets table; anything else is a rot-guard failure.
func (s *scan) bucketOf(t *testing.T, fset *token.FileSet, pos token.Pos, base, pkg string) (bucket, bool) {
	if pkg == "effects" {
		return bSA, true
	}
	if b, ok := baseBuckets[base]; ok {
		return b, true
	}
	s.failf(t, fset.Position(pos),
		"unclassified Params base %q -- add it to baseBuckets with a type justification", base)
	return 0, false
}

// stringMapParams whitelists the map[string]string-typed parameters whose
// maps are NOT card Params maps, so their index reads are not attributed:
// each entry is "pkg:func:param" with its justification. Anything not listed
// here AND not called with a `.Params`/alias argument fails the rot guard.
var stringMapParams = map[string]string{
	// ETB choice option builders receive the source ability's selector map;
	// the map is forwarded to type-choice enumeration, not consumed as card
	// Params by the census.
	"rules:Engine.typeChoiceOptions:params": "ETB type-choice selector map, not a card Params map",
	// effects/misc.go parseStaticLine: svars is the face's SVars table (a
	// cards.SA's SVar: bodies), read by NAME to fetch a static line -- not a
	// card Params map.
	"effects:parseStaticLine:svars":          "SVars table lookup by static-line name, not a card Params map",
	"effects:CloneStaticGrantReadable:svars": "SVars table lookup by named Clone static, not a card Params map",
	// Goad-static helpers inspect map arguments copied from parsed SVar
	// statics, not card SA Params; their callers classify the actual source.
	"effects:goadStaticGrantReadable:params": "parsed Goad static-line Params map, not a card SA Params map",
	// effects/misc.go compoundRememberedSpec: params is the map parseStaticLine
	// built from one SVar static line -- its ValidCard$/ValidTarget$ keys are
	// consumed here, but the map originates in an SVar body, not a card's
	// Params map.
	"effects:compoundRememberedSpec:params": "keys of a parseStaticLine-built static line (an SVar body), not a card Params map",
	// rules/cast.go bodyReadsAllTargeted: svars is the source face's (or
	// merged pile's) SVar table, walked by SVar NAME to decide whether a
	// cost head reaches the AllTargeted$ count ref (the alltargeted1 scope
	// gate) -- an SVar-body lookup, not a card Params map.
	"rules:bodyReadsAllTargeted:svars": "SVars table lookup by SVar name for the AllTargeted$ cost-head scan, not a card Params map",
	// rules/cast.go bodyReadsRef: the shared transitive SVar walk behind
	// bodyReadsAllTargeted and bodyReadsRootTarget (the root-target arm of
	// the equip-reduce window probe). svars is the source face's (or merged
	// pile's) SVar table, walked by SVar NAME -- an SVar-body lookup, not a
	// card Params map. Whitelisting the callee also silences the caller
	// attribution of every site that forwards its own svars into it.
	"rules:bodyReadsRef:svars": "SVars table lookup by SVar name for the transitive AllTargeted$/Targeted$ ref scan, not a card Params map",
	// rules/mayplay.go mayPlayGateRejected: params IS a card Params map, but
	// every key the function indexes is indexed ONLY to fail the MayPlay
	// static closed (mayPlayUnreadGates + MayPlayPlayer$) -- a fail-closed
	// gate is a RECOGNITION, not a consumption: the static is withheld whole
	// and the key is never honoured. CheckSVar$/SVarCompare$ are NOT in that
	// set any more: they are genuinely consumed, via effects.CheckSVarHolds
	// (Engine.mayPlayConditionGateHolds on the may-play family and
	// continuousGateHolds on the generic Continuous bucket), so they
	// correctly read. Whitelisting this function keeps its residual
	// recognitions out of the read sets; without it the keys would propagate
	// into mayPlayStatic's reads (propagateKeyReads attributes a callee's
	// indexed keys to the caller that passes the map) and mask the
	// genuinely unread gate keys of every MayPlay static. A NEW fail-closed
	// gate reader must be whitelisted here too, or its recognition reads mask
	// real gaps.
	"rules:mayPlayGateRejected:params": "fail-closed MayPlay gate recognition (mayPlayUnreadGates + MayPlayPlayer$) -- a rejection, never a consumption",
	// rules/mayplay.go mayPlayGateRejectedOther: the same fail-closed MayPlay
	// gate recognition as mayPlayGateRejected above -- the helper only
	// carries the family of keys the mutate-token carve-out left behind
	// (ValidSA$ is indexed by the CALLER now, and only to admit the exact
	// Spell.Mutate token mayPlayKinds classifies), so this whitelist keeps
	// its residual recognitions out of the read sets like the parent entry
	// does.
	"rules:mayPlayGateRejectedOther:params": "fail-closed MayPlay gate recognition split out of mayPlayGateRejected (mayPlayUnreadGates + MayPlayPlayer$) -- a rejection, never a consumption",
	// rules/mayplay.go mayPlayConditionGateHolds: params is a card Params map,
	// but the helper only forwards it to Engine.checkSVarHolds, whose
	// CheckSVar$/SVarCompare$ reads are already attributed through the generic
	// Continuous bucket (continuousGateHolds -> checkSVarHolds) -- the
	// whitelist keeps this forwarding call from being mistaken for an
	// unclassified third argument. The MayPlay family's read set gains the
	// keys via that same generic union, so this masks nothing.
	"rules:Engine.mayPlayConditionGateHolds:params": "card Params map forwarded to checkSVarHolds; CheckSVar$/SVarCompare$ are read on the generic Continuous bucket",
	// rules/class_level.go Engine.classBandGateHolds: params IS a card Params
	// map, but the only key it indexes is ClassBand$ -- a compile-time band the
	// cards keyword expansion injects onto a kw:Class granted body, never a key
	// a raw corpus face carries. Attributing the read would add no gap (the
	// census measures raw corpus faces), and the call sites forward a Params
	// map that the surrounding gate functions already own, so the whitelist
	// keeps this lookup from being mistaken for an unclassified third map.
	"rules:Engine.classBandGateHolds:params": "reads only the compile-time ClassBand$ key the kw:Class expansion injects, never a raw corpus Params key",
	// effects/misc.go MayPlayStaticParams: params is a map parseStaticLine
	// built from one SVar static line (or the S: line's own Params map passed
	// by rules/layers.go's mayPlayGrant) -- the MayPlay-family keys it
	// whitelists are static-line keys, not a card Params map.
	"effects:MayPlayStaticParams:params": "keys of a static may-play line (S: or parseStaticLine-built), not a card Params map",
	// effects/misc.go mayPlayGrantFromLine: params is the map parseStaticLine
	// built from one SVar static line -- the same SVar-body shape
	// compoundRememberedSpec reads.
	"effects:mayPlayGrantFromLine:params": "keys of a parseStaticLine-built static line (an SVar body), not a card Params map",
	// effects/misc.go mayPlayFreeGrantFromLine: the same parseStaticLine-built
	// SVar static line, the FREE-cast MayPlay grant arm's whitelist (the
	// MayPlayWithoutManaCost$ True shape).
	"effects:mayPlayFreeGrantFromLine:params": "keys of a parseStaticLine-built static line (an SVar body), not a card Params map",
	// effects/misc.go MayPlayFreeStaticParams: the same static-line map -- the
	// free-cast shape's own whitelist, shared grammar with MayPlayStaticParams.
	"effects:MayPlayFreeStaticParams:params": "keys of a static may-play line (S: or parseStaticLine-built), not a card Params map",
	// effects/misc.go mayPlayParamsScan: the ONE key-scan every MayPlay
	// whitelist delegates to (the key-range loop plus the rider reads). Its
	// reads are also made -- and genuinely attributed -- by rules/mayplay.go's
	// mayPlayStatic on the static family, so skipping the effects-side
	// attribution masks nothing.
	"effects:mayPlayParamsScan:params": "shared key-scan of a static may-play line (S: or parseStaticLine-built), not a card Params map",
	// effects/misc.go mayPlayEffectParams: the Effect-delivery sibling of
	// MayPlayStaticParams -- the same static-line map, widened by the
	// ValidAfterStack$ qualifier the effect registration path carries.
	"effects:mayPlayEffectParams:params": "keys of a static may-play line (S: or parseStaticLine-built), not a card Params map",
	// effects/misc.go mayPlayEffectFreeParams: the free-cast Effect-delivery
	// sibling, the same static-line map.
	"effects:mayPlayEffectFreeParams:params": "keys of a static may-play line (S: or parseStaticLine-built), not a card Params map",
	// effects/misc.go cascadeKeywordGrantFromLine: params is the same
	// parseStaticLine-built SVar static line (the AddKeyword$ Cascade grant
	// arm's whitelist); its dynamic gate-key loop (Condition/CheckSVar/...) is
	// a fail-closed recognition, never a consumption.
	"effects:cascadeKeywordGrantFromLine:params": "keys of a parseStaticLine-built static line (an SVar body), not a card Params map",
	// effects/misc.go setMaxHandSizeGrantFromLine: params is the same
	// parseStaticLine-built SVar static line (the SetMaxHandSize$ grant arm's
	// whitelist); its dynamic gate-key loop (Condition/CheckSVar/...) is a
	// fail-closed recognition, never a consumption.
	"effects:setMaxHandSizeGrantFromLine:params": "keys of a parseStaticLine-built static line (an SVar body), not a card Params map",
	// effects/staticeffect.go parseStaticEffectGrant: params is the map
	// parseStaticLine built from one SVar static line -- the StaticEffect$
	// rider's Continuous body (AddType$/AddColor$/AddKeyword$/...), whose keys
	// are static-line keys, not a card Params map.
	"effects:parseStaticEffectGrant:params": "keys of a parseStaticLine-built SVar static line (the StaticEffect$ rider body), not a card Params map",
	// effects/misc.go parseReplacementLine: svars is the face's SVars table
	// (an Effect's ReplacementEffects$ body lives behind an SVar name),
	// mirroring parseStaticLine's svars -- not a card Params map.
	"effects:parseReplacementLine:svars": "SVars table lookup by replacement-line name, not a card Params map",
	// effects/misc.go replacementLineWith: params is the map
	// parseReplacementLine built from one SVar replacement line -- its
	// ReplaceWith$ key is consumed here, but the map originates in an SVar
	// body, not a card's Params map.
	"effects:replacementLineWith:params": "keys of a parseReplacementLine-built static line (an SVar body), not a card Params map",
	// effects/misc.go replacementLineCantHappen: params is the map
	// parseReplacementLine built from one SVar replacement body -- the
	// Layer$ CantHappen recognition of the bodyless form (Mistrise Village's
	// AntiMagic) reads the same SVar body shape, not a card Params map.
	"effects:replacementLineCantHappen:params": "keys of a parseReplacementLine-built replacement line (an SVar body), not a card Params map",
	// effects/misc.go replacementLinePrevents: params is the map
	// parseReplacementLine built from one SVar replacement body -- the
	// bodyless Prevent$ True DamageDone recognition (Selfless Squire's
	// RPrevent, task dponce1) reads the same SVar body shape, not a card
	// Params map.
	"effects:replacementLinePrevents:params": "keys of a parseReplacementLine-built replacement line (an SVar body), not a card Params map",
	// effects/misc.go redirectExileBody / selfExileIdiom (cardfuzz batch5
	// line 7): body and sub are replacementBodyParams-built maps of an
	// Effect's ReplaceWith$ SVar body and its SubAbility$ line, svars the
	// face's SVar table the SubAbility$ name is looked up in -- recognitions
	// of the Effect-created "exile it instead" redirect, not card Params maps.
	"effects:redirectExileBody:body":  "keys of a replacementBodyParams-built ReplaceWith$ SVar body, not a card Params map",
	"effects:redirectExileBody:svars": "SVars table lookup by SubAbility$ name, not a card Params map",
	"effects:selfExileIdiom:sub":      "keys of a replacementBodyParams-built SubAbility$ SVar body, not a card Params map"}

// propagateKeyReads resolves two indirect read shapes:
//
//   - `.Params[paramIdent]` reads: every caller of a function that reads one
//     of its own string parameters as a key gains the literal keys passed at
//     those positions (hasStat / statList / actorMatches /
//     effects/count's Num-body).
//   - map[string]string-parameter reads (the helper-passed-map form): a
//     literal key indexed on such a parameter is attributed to the caller
//     with the BUCKET of the argument expression -- which must be a `.Params`
//     selector or a tracked Params alias; anything else is a
//     mapArgFailure the rot guard surfaces. parseStaticLine and
//     compoundRememberedSpec are whitelisted (stringMapParams): their maps
//     are SVar tables and parsed static lines, not card Params.
func (s *scan) propagateKeyReads() {
	s.mapAttributed = map[string]bool{}
	// Key indirection is transitive (effScry passes "ScryNum" into
	// effLookAndArrange's numKey, which forwards it into Num's key), so the
	// walk runs to a fixpoint: each round attributes literals through the
	// key-parameters recorded in earlier rounds. keyRds grows monotonically,
	// so the rounds converge (32 is far above the longest call chain in
	// effects/ or rules/).
	for round := 0; round < 32; round++ {
		if !s.propagateKeyReadsOnce() {
			break
		}
	}
}

// propagateKeyReadsOnce runs one full pass over every call site, attributing
// literal keys and recording key-indirection; it reports whether the pass
// added anything (so the fixpoint loop above knows when to stop).
func (s *scan) propagateKeyReadsOnce() bool {
	added := false
	count := func(fi *fnInfo, b bucket, set map[string]bool, k string) bool {
		if set == nil {
			return true
		}
		return !set[k]
	}
	for _, fi := range s.fns {
		for _, cs := range fi.callSites {
			target := s.fns[fi.pkg+":"+cs.callee]
			if target == nil {
				continue
			}
			for b, params := range target.keyRds {
				for param := range params {
					idx, ok := target.strParams[param]
					if !ok || idx >= len(cs.args) {
						continue
					}
					if cs.args[idx] != "" {
						if count(fi, b, fi.reads[b], cs.args[idx]) {
							added = true
						}
						s.addRead(fi, b, cs.args[idx])
						continue
					}
					// Key indirection: the caller forwards one of its OWN
					// string parameters as the callee's key parameter (the
					// effLookAndArrange(h,c,sa,numKey,..) -> Num(,,key,..)
					// chain behind Scry's ScryNum$ and Surveil's Amount$).
					// Record the read against the caller's parameter so a
					// still-outer caller passing a literal attributes through
					// it; a non-identifier argument cannot be followed and
					// stays unattributed, exactly as before.
					if pname := cs.exprs[idx]; pname != "" {
						if _, isParam := fi.strParams[pname]; isParam {
							if count(fi, b, fi.keyRds[b], pname) {
								added = true
							}
							s.addKeyRead(fi, b, pname)
							continue
						}
					}
				}
			}
			for param, keys := range target.mapIdxRds {
				idx, ok := target.mapParams[param]
				if !ok || idx >= len(cs.exprs) {
					continue
				}
				s.mapAttributed[target.pkg+":"+target.name+":"+param] = true
				arg := cs.exprs[idx]
				var base string
				switch {
				case strings.HasSuffix(arg, ".Params"):
					base = strings.TrimSuffix(arg, ".Params")
				default:
					if b, isAlias := fi.aliases[arg]; isAlias {
						base = b
						break
					}
					s.mapArgFailures = append(s.mapArgFailures, fmt.Sprintf(
						"%s calls %s passing %q for map[string]string parameter %q -- not a Params map; classify it in stringMapParams or fix the argument",
						fi.name, target.name, arg, param))
					continue
				}
				b, ok := s.bucketOf(nil, token.NewFileSet(), token.NoPos, base, fi.pkg)
				if !ok {
					continue // already recorded as unclassified by bucketOf
				}
				for key := range keys {
					if key == "" {
						s.mapArgFailures = append(s.mapArgFailures, fmt.Sprintf(
							"%s: map[string]string parameter %q of %s is indexed with a dynamic key -- classify it",
							target.name, param, fi.name))
						continue
					}
					if fi.reads[b] == nil || !fi.reads[b][key] {
						added = true
					}
					s.addRead(fi, b, key)
				}
			}
		}
	}
	return added
}

// closureReads unions fn's own reads with everything reachable through its
// package-local calls. exclude skips dispatch callees (the shared trigger set
// must not inherit every mode's reads). visited memoises; out accumulates.
func (s *scan) closureReads(fi *fnInfo, exclude map[string]bool, visited map[string]bool, out map[bucket]map[string]bool) {
	if visited[fi.name] {
		return
	}
	visited[fi.name] = true
	// NOTE: fi.keyRds deliberately does NOT join here -- a `.Params[param]`
	// read only counts once propagateKeyReads resolved it to literal keys at
	// call sites (which land in fi.reads of the callers). Unresolved key
	// parameters are not reads.
	for b, keys := range fi.reads {
		if out[b] == nil {
			out[b] = map[string]bool{}
		}
		for k := range keys {
			out[b][k] = true
		}
	}
	for callee := range fi.calls {
		if exclude[callee] {
			continue
		}
		if target := s.fns[fi.pkg+":"+callee]; target != nil {
			s.closureReads(target, exclude, visited, out)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Attribution roots the code does not state in a machine-readable position
// ---------------------------------------------------------------------------

// apiSpecificRulesSA names the rules functions whose SA (bSA) reads execute
// only inside the listed APIs' code paths -- NOT for every cast/activation/
// targeting of any primitive. Their direct SA reads are REMOVED from the
// generic rules union and attributed to exactly the named APIs, so the mana
// path's `Amount$`/`Produced$` reads no longer mask e.g. api:Sacrifice's
// genuinely unread `Amount$`, and the unless-pay resume's `UnlessCost$` read
// no longer masks api:Sacrifice's unread `UnlessCost$`. Each entry was
// verified by reading its callers: every call site sits on a path only that
// API reaches (the mana-ability offer/payment/resolution chain, the Charm
// mode ask/resume pair, the modal-trigger placement ask, the unless-pay
// resume arms effCounter/effCopySpellAbility suspend with). The rot guard
// fails on a stale entry (renamed function, or one that no longer reads SA
// params); a NEW api-specialised path must be added here or its reads
// over-suppress every other API's real gaps.
var apiSpecificRulesSA = map[string][]string{
	// The mana-ability chain: the ability-choose wheel's Produced$ label read
	// (manaAbilityLabel, called from activateManaFor which itself no longer
	// touches SA params), the flattened combo wheel's per-colour expansion
	// read (manaAbilityComboColours -- both call sites, activateManaFor and
	// answerManaActivation, guard on ma.API == "Mana" before calling, so the
	// Produced$ read never executes for another api), manaAbilityPayablePool's
	// offer/payment path (the pool-parameterized core manaAbilityPayable
	// delegates to; the potential-action walk calls it directly with the
	// hypothetical bound), the AvailableMana projection, and
	// activatedMatchesValidSA's Produced$-based mana-ability recognition --
	// all run on mana abilities (api:Mana) only.
	"Engine.manaAbilityPayablePool": {"Mana"},
	"manaProducedLabel":             {"Mana"},
	"manaAmountPips":                {"Mana"},
	"manaAbilityCostPrefix":         {"Mana"},
	// The intrinsic-append dedup read (rules/mana_activation.go): the CR 305.6
	// all-land-types walk reads a mana ability's Produced$ ONLY, so left in
	// the generic union it would mask every other API's unread Produced$
	// (measured: api:Sacrifice/api:DealDamage).
	"manaAbilityProduced":     {"Mana"},
	"manaAbilityComboColours": {"Mana"},
	// The potential pool's mana-ability readers (rules/potential.go): they
	// read a mana ability's Produced$/Amount$ ONLY, and PotentialMana is
	// reachable from the viewer's projection on every priority decision, so
	// without this entry their reads would join the generic union and mask
	// Sacrifice's unread Produced$/DealDamage's unread Produced$.
	"addPotentialMana": {"Mana"},
	"potentialAmount":  {"Mana"},
	// The ManaReflected activation gate: only a reflected-mana ability's
	// offer consults IsPresent$/PresentCompare$ on the SA itself (Tazri's
	// "another activated ability" condition). A plain AB$ Mana ability's
	// IsPresent$ gate is read separately, by manaActivationGateHolds below
	// (the Verge lands, Temple of the False God, Shrine of the Forsaken
	// Gods), so this entry must not swallow it.
	"Engine.manaReflectedPresentHolds": {"ManaReflected"},
	// The plain-Mana activation gate (Shrine of the Forsaken Gods'
	// IsPresent$/PresentCompare$, Urza's Workshop's Activation$ Metalcraft):
	// only a plain AB$ Mana ability's offer runs it.
	"Engine.manaActivationGateHolds": {"Mana"},
	"Engine.emitManaTap":             {"Mana"},
	"Engine.isTriggeredManaAbility":  {"Mana"},
	// askTriggeredManaColor reads the first resolved Mana sub-ability's
	// Amount$/Produced$ to build its allocation; that local is bSA but this
	// path only ever reaches api:Mana.
	"Engine.askTriggeredManaColor": {"Mana"},
	"Engine.askManaColor":          {"Mana"},
	"triggeredManaColourChoice":    {"Mana"},
	// rewriteChosenMana (rules/mana_activation.go) executes only inside
	// resolveTriggeredManaAbilities, so its Produced$ read belongs to
	// api:Mana alone -- left in the generic union it would mask every
	// other API's unread Produced$.
	"Engine.rewriteChosenMana":             {"Mana"},
	"Engine.resolveManaAbilityRefOriginal": {"Mana"},
	"Engine.resolveManaEffect":             {"Mana"},
	"manaColourPrompt":                     {"Mana"},
	"Engine.AvailableMana":                 {"Mana"},
	"addAvailable":                         {"Mana"},
	"availableAmount":                      {"Mana"},
	"activatedMatchesValidSA":              {"Mana"},
	// The attack-prop and unless-cost payment windows' affordability input
	// (rules/mana_available.go windowManaUnits, called by
	// rules/attack_cost.go attackManaSources and
	// rules/unless_payment.go unlessManaBudget): it walks the payer's
	// battlefield and reads each window-usable mana ability's Produced$
	// (and Amount$, via availableAmount above) to count the units the
	// window can tap. The walk only ever inspects api:Mana abilities
	// (availableManaAbilitiesForWindow), so its Reads belong to api:Mana
	// alone -- left in the generic union they would mask every other
	// API's unread Produced$ (measured: api:Sacrifice/api:DealDamage).
	"Engine.windowManaUnits": {"Mana"},
	// The attack-prop payment window's choice-shaped membership
	// (rules/attack_cost.go attackChoiceManaSources): it walks the payer's
	// battlefield and reads each window-usable mana ability's Produced$ (plus
	// Cost$/RestrictValid$) to decide whether an "Any"/"Combo"/"Chosen"
	// source can pay a generic attack tax, and pins the colour it will be
	// tapped for. Like windowManaUnits above it only ever inspects api:Mana
	// abilities (availableManaAbilitiesForWindow), so its reads belong to
	// api:Mana alone -- left in the generic union they mask every other API's
	// unread Produced$ (measured: api:Sacrifice/api:DealDamage).
	"Engine.attackChoiceManaSources": {"Mana"},
	// The cast-payment window's activation-cost layer: castWindowProbeUnits
	// walks untapped api:Mana abilities (with the live-pool payability gate
	// lifted, so a fee an earlier same-window activation funds is priced) and
	// reads each ability's Cost$/RestrictValid$/Produced$, while
	// castWindowAmount reads its Amount$ (and the SVar body behind it). Both
	// are cast-window-only readers of api:Mana abilities, so their reads
	// belong to api:Mana alone -- left in the generic union they mask every
	// other API's unread Amount$/Produced$ (measured:
	// api:ChangeZone/api:Sacrifice/api:DealDamage).
	"Engine.castWindowProbeUnits": {"Mana"},
	"Engine.castWindowAmount":     {"Mana"},
	// The payment-plan offer is another mana-only source walk.  It obtains
	// windowManaUnit alternatives exclusively from AB$ Mana abilities, then
	// reads Produced$/RestrictValid$/Cost$ (and the Amount$ helper) to build a
	// replayable tap witness.  These reads cannot make those parameters appear
	// implemented on unrelated resolving APIs such as Sacrifice or DealDamage.
	"Engine.paymentPlanManaUnits":         {"Mana"},
	"Engine.paymentPlanUnitAlternatives":  {"Mana"},
	"Engine.executePlannedManaActivation": {"Mana"},
	// The Charm mode paths: the CR 601.2b cast-time modes ask (castModeAsk),
	// the per-mode target declaration (modalTargetSA), the resume-side mode
	// decisions/labels, and the modal-trigger placement ask (CharmNum$).
	"Engine.castModeAsk":     {"Charm"},
	"modalTargetSA":          {"Charm"},
	"modeDecision":           {"Charm"},
	"modeDecisionForChoices": {"Charm"},
	"modeLabels":             {"Charm"},
	"modeChoiceNames":        {"Charm"},
	"Engine.askTriggerModes": {"Charm"},
	// The unless-pay resume arm: only effCounter and effCopySpellAbility
	// suspend with an UnlessCost$ ask, so resumeResolution's UnlessCost$
	// read belongs to those two APIs alone. api:Play joins them for the
	// same reason: the "play" resume arm is the only reader of that
	// primitive's own riders (WithoutManaCost$/PlayCost$/ReplaceGraveyard$/
	// ImprintPlayed$/ShowCards$ -- only an answered Play effect re-enters
	// here), and left in the generic union none of them was ever
	// attributable to api:Play, which kept WithoutManaCost listed unread on
	// every repo-deck carrier even though the free-cast read (and its vaan
	// end-to-end pin) predates this entry. The function-level granularity
	// over-attributes resumeResolution's OTHER cases' reads to Play too;
	// measured against the repo-deck Play carriers (Scarlet Witch,
	// Spinerock Knoll, West Coast Expansion, Conduit of Worlds) none of
	// them carries a param only another case reads, and Play's genuinely
	// unread RememberPlayed$ stays unmasked (no case reads it).
	"Engine.resumeResolution": {"Counter", "CopySpellAbility", "Play"},
	// The ward payment path: only the Ward keyword's expanded trigger
	// reaches these (resumeResolution dispatches on rp.sa.API == "Ward"),
	// so their UnlessCost$ reads belong to api:Ward alone -- left in the
	// generic union they would mask every other API's unread UnlessCost$
	// (measured: api:Tap on Blood Crypt/Hallowed Fountain; api:Sacrifice's
	// UnlessCost$ read moved to the registered effSacrifice gate (vexdev),
	// so the resume's generic read no longer masks any api:Sacrifice gap).
	"Engine.beginWardPayment":  {"Ward"},
	"Engine.settleWardPayment": {"Ward"},
	// The opening-hand pregame actions: applyOpeningEffect, its delayed-
	// trigger registration and the answer handler run ONLY on the expanded
	// opening-action SVar of a MayEffectFromOpeningHand keyword (Chancellor
	// of the Tangle, Gemstone Caverns, Impatient Iguana), so their
	// Origin$/Destination$/BecomeStartingPlayer$/Triggers$/SubAbility$
	// reads belong to the APIs such an action carries (ChangeZone for the
	// put-onto-battlefield shape, PutCounter for the counter rider, Effect
	// for the delayed-trigger shape) -- left in the generic union they
	// would mask every other API's unread Destination$ (measured:
	// api:Counter on Force of Will and Remand).
	"Engine.applyOpeningEffect":            {"ChangeZone", "PutCounter", "Effect"},
	"Engine.registerOpeningEffectTriggers": {"ChangeZone", "PutCounter", "Effect"},
	"Engine.handleOpening":                 {"ChangeZone", "PutCounter", "Effect"},
	// The token-creation replacement dispatch: these read the ReplaceWith$
	// body of an R:Event$ CreateToken replacement line ONLY -- the body is
	// by definition a DB$ ReplaceToken SA, so the Type$/Amount$/TokenScript$/
	// ValidChoices$ reads belong to api:ReplaceToken alone -- left in the
	// generic union they would mask every other API's unread Amount$
	// (measured: api:ChangeZone). The dispatcher's SA-param reads live in
	// driveTokenReplacements and poseChosenTokenReplacement since the
	// chosen-copy election (Esix/Moonlit/Mirrormind) moved them out of
	// continueCreateTokenReplacements.
	"Engine.driveTokenReplacements":      {"ReplaceToken"},
	"Engine.poseChosenTokenReplacement":  {"ReplaceToken"},
	"Engine.applyTokenReplacementToPlan": {"ReplaceToken"},
	// The CR 616.1 AddCounter order machinery reads the ReplaceWith$ body of
	// an R:Event$ CounterChange replacement line ONLY -- the body is by
	// definition a DB$ ReplaceCounter SA, so its Amount$ read belongs to
	// api:ReplaceCounter alone (the same shape the token dispatch above
	// scopes); left in the generic union it would mark Amount$ read for
	// every other API (measured: api:ChangeZone). Only counterReplaceOp
	// reads the body parameters now; the two application functions consume
	// its priced result.
	"Engine.counterReplaceOp":  {"ReplaceCounter"},
	"tokenReplacementsCommute": {"ReplaceToken"},
	"tokenReplApplies":         {"ReplaceToken"},
	// The body-defined entry-counter fold (rules/entry_counters.go): these
	// read the ReplaceWith$ body of an R:Event$ Moved replacement line ONLY,
	// and only when its DB$ body is a PutCounter|ETB$ True ability (every
	// reader short-circuits on `r.With.API == "PutCounter"` first), so the
	// ETB$/Defined$/CounterNum$/CounterType$ reads plus entryBodyAbsorbable's
	// withheld-modifier keys belong to api:PutCounter alone -- left in the
	// generic union they would mark CounterType$/Optional$/ETB$ read for
	// every other API (measured: api:Mill's World Shaper Optional$ gap).
	"Engine.entryBodyCandidates":    {"PutCounter"},
	"Engine.entryBodyCounterGrants": {"PutCounter"},
	"entryBodyKindEncodable":        {"PutCounter"},
	"entryBodyAbsorbable":           {"PutCounter"},
}

// apiSpecificRulesStat is the stat-bucket twin of apiSpecificRulesSA: it
// names the rules functions whose static (bStat) reads execute only for a
// synthetic FAMILY of a static mode -- NOT for every static of that mode.
// Their reads are removed from the generic mode union and attributed to the
// synthetic mode named in the value ("Continuous.MayPlay": statics whose
// Mode$ is Continuous AND that carry a MayPlay$ value -- the only mode the
// corpus pairs MayPlay$ with, measured 656/656 MayPlay$ lines are Mode$
// Continuous at the corpus pin). The synthetic mode's read set is the base
// mode's union PLUS the family closures -- a MayPlay static is still a
// Continuous static and every generic Continuous consumer still reads it.
//
// Left in the generic union these reads would mask every other Continuous
// static's genuinely unread gate keys: mayPlayStatic evaluates IsPresent$ /
// MayPlayLimit$ live, but only for MayPlay$ statics -- Angelic Overseer's
// IsPresent$ and Master of Etherium's CharacteristicDefining$ were all
// masked exactly this way (review round findings-sol1, MAJOR). The family
// split restores them. Condition$ and CheckSVar$/SVarCompare$ later joined
// the generic Continuous bucket legitimately: the statics wave's generic
// restriction/cost gates read them on every Continuous static.
// The rot guard fails on a stale entry (renamed function, or one that no
// longer reads static params); a NEW mode-scoped reader must be added here
// or its reads over-suppress every other static's real gaps.
// mayPlayStaticParams attributes these genuine effects/mayPlayParams map reads
// to the synthetic MayPlay static family; the forwarded map parameter itself
// is intentionally stringMapParams-whitelisted.
var mayPlayStaticParamReads = map[string]bool{
	"MayPlayIgnoreColor": true,
	"MayPlayIgnoreType":  true,
}

var apiSpecificRulesStat = map[string]string{
	// The MayPlay grant path: mayPlayGrant (the offer walk's per-card
	// evaluation, over the card's own face statics and activeStatics
	// "Continuous") carries the family's propagated reads (propagateKeyReads
	// attributes mayPlayStatic's map indexes to it), and
	// warpGraveyardAllowed scans Continuous MayPlay statics directly over
	// the face's Statics slice (Timeline Culler's explicit graveyard-Warp
	// permission) -- it was previously a generic Continuous root
	// (handRoots.stat), which masked ValidSA$/EffectZone$ for every plain
	// Continuous static.
	"Engine.mayPlayGrant":  "Continuous.MayPlay",
	"warpGraveyardAllowed": "Continuous.MayPlay",
	// The raise walk (rules/mayplay.go's mayPlayRaiseCost, called from
	// legal.go's may-play spell word and land walks and cast.go's "mayplay"
	// cost case): it carries mayPlayStatic's propagated reads (RaiseCost$
	// among them, the genuine consumption that replaced the old fail-closed
	// recognition), so it is family-attributed exactly like the grant path --
	// left generic it would mask a plain Continuous static's real unread
	// keys.
	"Engine.mayPlayRaiseCost": "Continuous.MayPlay",
	// The alt-cost delivery path (rules/mayplay.go's mayPlayAltCosts, called
	// from alternativeCosts): it reads MayPlay statics' MayPlayAltManaCost$
	// live -- Darksteel Monolith's "pay {0} rather than the mana cost" --
	// so its read is family-attributed like the grant path's, never in the
	// generic Continuous union.
	"Engine.mayPlayAltCosts": "Continuous.MayPlay",
	// The ValidSA$ classifier (rules/mayplay.go's mayPlayKinds, called from
	// legal.go's may-play spell walk): it reads MayPlay statics' ValidSA$
	// live -- Brokkos, Apex of Forever's `ValidSA$ Spell.Mutate` permits
	// ONLY the mutate cast from the graveyard, which the split into the
	// plain and mutate halves encodes -- so its read is family-attributed
	// exactly like the grant path's, never in the generic Continuous union.
	"Engine.mayPlayKinds": "Continuous.MayPlay",
}

// statFamilyInternal names the rules functions whose static reads are family
// reads but which are NOT attribution roots -- their map-indexed keys
// propagate to their CALLERS (propagateKeyReads attributes a callee's
// indexed keys to the caller that passes the map), so every caller must be
// an apiSpecificRulesStat root; a generic caller would carry the family's
// reads in its own read set and re-mask the mode's genuinely unread keys.
// The rot guard enforces exactly that caller discipline.
var statFamilyInternal = []string{
	// mayPlayStatic, the per-static gate reader: mayPlayGrant is its only
	// legitimate caller.
	"Engine.mayPlayStatic",
}

// genericSAExcludes is the third attribution class: rules functions whose SA
// reads genuinely run for every activated ability EXCEPT the named APIs --
// the generic ability-offer loop (rules/legal.go legalActions) skips
// isManaAbilityAPI abilities, so its abilityPresentHolds gate never executes
// for Mana/ManaReflected and must not mark those APIs' IsPresent$/
// PresentCompare$ read (the Verge/Temple-of-the-False-God Mana.IsPresent set
// is a separate, still-open gap). Each entry's keys are removed from the
// generic rules union and attributed to every api EXCEPT the named ones --
// the mirror image of apiSpecificRulesSA. The rot guard fails on a stale
// entry (renamed function, or one that no longer reads SA params).
var genericSAExcludes = map[string][]string{
	"Engine.abilityPresentHolds": {"Mana", "ManaReflected"},
}

// handRoots declares ATTRIBUTION (which function to read for a primitive) for
// the few roots the code does not state via Register / the dispatch switch /
// activeStatics literals. The reads still come from the scanned bodies.
var handRoots = struct {
	stat map[string][]string
	trig []string
	repl []string
}{
	stat: map[string][]string{
		// adjustedCost's local `apply` closure and costModifiers' range over
		// []string{"RaiseCost", "ReduceCost"} both call activeStatics with a
		// variable; the literals sit at their callers. Declared instead of
		// refactored so the scan stays read-only over production code.
		"RaiseCost":    {"Engine.costModifiersWithTargets", "Engine.costModifiersWithTargetsX"},
		"ReduceCost":   {"Engine.costModifiersWithTargets", "Engine.costModifiersWithTargetsX"},
		"OptionalCost": {"Engine.optionalCostViews"},
		// staticEffects filters on st.Mode != "Continuous" before reading.
		// activeStatics (the battlefield-only restriction collector) and
		// collectActionStatics (the AddAbility$ mana-grant membership walk)
		// now read the same statics' EffectZone$ through effectZoneOK -- the
		// Continuous EffectZone$ gate shared with staticEffects and
		// collectCostStatics -- so their reads are attributed here like the
		// other direct-scan roots.
		"Continuous": {"Engine.staticEffects", "Engine.activeStatics", "Engine.collectActionStatics",
			"warpGraveyardAllowed", "Engine.maxSpeedAbilities",
			// The may-play grant walks read the Continuous static's Params
			// through mayPlayGrant over the face's Statics slice -- the same
			// direct-scan shape warpGraveyardAllowed has (mayPlaySpellIds also
			// scans the exiled card's own EffectZone$ Exile self-grant).
			"Engine.mayPlaySpellIds", "Engine.mayPlayLandIds"},
		// maxSpeedAbilities scans Continuous AddAbility$/Condition$MaxSpeed
		// statics directly over the face's Statics slice (CR 702.163c's
		// max-speed grant), with no activeStatics call -- the same
		// direct-scan shape.
		// mustAttackRequired scans MustAttack statics directly, with no
		// activeStatics call; its Params reads are the whitelist switch.
		"MustAttack": {"Engine.mustAttackRequired"},
		// AttackRestrict: attackRestrictStatics is the literal activeStatics
		// root the scan already attributes; maxAttackers and attackRestrictLimit
		// are its callers and carry the mode's ValidDefender$/MaxAttackers$
		// reads, so they are declared here too (a caller of a collector root is
		// not reachable FROM that root).
		"AttackRestrict": {"Engine.attackRestrictStatics", "Engine.maxAttackers", "Engine.attackRestrictLimit"},
		// untapOtherStaticsMatch scans UntapOtherPlayer statics directly over
		// the face's Statics slice (Endbringer's foreign-untap shape), with
		// no activeStatics call -- the same direct-scan shape
		// mustAttackRequired has. staticPresentHolds (its IsPresent$/
		// PresentCompare$ gate) is reached through it.
		"UntapOtherPlayer": {"Engine.untapOtherStaticsMatch"},
	},
	// The trigger-queue drain and the stack-resolution paths read trigger
	// params (OptionalDecider$, TriggerDescription$, Static$, ValidCard$)
	// outside triggerMatches' dispatch: the drain (pushTrigger and the labels
	// it renders), the resolve-time optional ask (resolveTop), the triggered
	// mana-ability gate (isTriggeredManaAbility), the referent bindings
	// (triggerReferents) and the stack-optional query (StackOptional). All are
	// mode-SHARED machinery; each mode still adds its own dispatch branch on
	// top.
	trig: []string{"Engine.pushTrigger", "Engine.triggerLabel", "Engine.abilityLabel",
		"Engine.resolveTop", "Engine.isTriggeredManaAbility", "Engine.triggerReferents",
		"Engine.StackOptional", "Engine.optionalDecider",
		// putTriggersOnStack is the queue drain's root: its
		// groupOrderDuplicates step reads the OrderDuplicates$ trigger
		// parameter (through orderDuplicatesGroup / triggerOrdersDuplicates)
		// to keep duplicate instances of a flagged line adjacent. The drain
		// has no machine-readable mode root, so it is declared here like the
		// other queue-drain reads above.
		"Engine.putTriggersOnStack",
		// checkAttackerUnblockedOnceTriggers is a dedicated hook queued from
		// rules/turn.go's declare-blockers round-complete branch, NOT from
		// checkTriggers (unlike checkAttackerBlockedTriggers / checkBlocksTriggers
		// / checkChapterTriggers, which checkTriggers calls and so reach through
		// Engine.triggerMatches). It reads its own mode's ValidDefenders$ /
		// ValidAttackingPlayer$ directly, so the scan needs the explicit root to
		// attribute those reads.
		"Engine.checkAttackerUnblockedOnceTriggers",
		// AttackerUnblocked has the same round-complete dedicated hook, but
		// queues one instance per matching attacker and reads ValidCard$ /
		// ValidDefender$ against the attacker and its actual defender.
		"Engine.checkAttackerUnblockedTriggers",
		// The static-grant's trigger walk (AddTrigger$): mode-SHARED machinery
		// like the drain above -- a granted trigger whose mode has a
		// trigMatchers entry matches through triggerMatches' own dispatch. The
		// dedicated-hook modes (AttackerBlocked/AttackerBlockedByCreature,
		// AttackerUnblocked/AttackerUnblockedOnce, Blocks, Chapter, Attached,
		// …) have no trigMatchers entry and are dispatched by their own
		// granted walks instead, so a granted instance of one of those matches
		// elsewhere -- not here.
		"Engine.checkGrantedStaticTriggers",
		// The live trigger walk's zone-skip classifier (trigger_zoneskip.go)
		// re-reads zoneGate's TriggerZones$/ActiveZones$ and the Phase$
		// diagnostic's spec as pure syntax, mode-shared, to decide which
		// hidden zones the walk may pass over; the reads that give those
		// params meaning stay in zoneGate/phaseGate under triggerMatches.
		"Engine.faceTriggerZones",
		// The event-matched delayed registrations (Chancellor of the Annex's
		// opening-hand Mode$ SpellCast shape): the registration re-parses the
		// stored trigger body, and the firing walker re-evaluates its
		// ValidCard$/ValidActivatingPlayer$/PlayerTurn$ clauses at fire time,
		// outside triggerMatches' dispatch walk.
		"Engine.registerOpeningEffectTriggers", "Engine.checkEventDelayedTriggers"},
	// applyReplacements is the replacement pipeline's root beside
	// replacementMatches, whose `r.Event != "Moved"` early return scopes every
	// r.Params read in it to repl:Moved. handleReplacement is the
	// parked-repl-choice decision handler (it resumes the parked phase
	// machinery and reads the parked repl's Optional$ directly), reached
	// through the decision resume path rather than the pipeline.
	repl: []string{"Engine.applyReplacements", "Engine.handleReplacement"},
}

// derivedReads is the per-primitive read set the scan attributes.
type derivedReads struct {
	api  map[string]map[string]bool
	trig map[string]map[string]bool
	stat map[string]map[string]bool
	repl map[string]map[string]bool
}

func (s *scan) derived() *derivedReads {
	d := &derivedReads{
		api:  map[string]map[string]bool{},
		trig: map[string]map[string]bool{},
		stat: map[string]map[string]bool{},
		repl: map[string]map[string]bool{},
	}
	// api: the effects implementation's closure, UNION the generic cast/
	// activation/legality machinery: every rules-side SA read applies to
	// whatever primitive is being cast, activated or targeted. The
	// API-SPECIALISED rules paths (apiSpecificRulesSA) are excluded from the
	// union and attributed to their own APIs only -- the mana path's
	// Amount$/Produced$ reads must not mark Amount$ read for api:Sacrifice.
	rulesAllSA := map[string]bool{}
	specialised := map[string]map[string]bool{} // api -> direct SA reads
	excludedSA := map[string]bool{}
	for fn, apis := range apiSpecificRulesSA {
		excludedSA[fn] = true
		fi := s.fns["rules:"+fn]
		if fi == nil {
			continue // rotGuard already failed on the stale entry
		}
		for k := range fi.reads[bSA] {
			for _, api := range apis {
				if specialised[api] == nil {
					specialised[api] = map[string]bool{}
				}
				specialised[api][k] = true
			}
		}
	}
	// genericSAExcludes: keys these functions read are removed from the
	// generic union and attributed per api minus the excluded ones (the
	// mirror image of the specialised split above).
	genericKeys := map[string]map[string]bool{}     // fn -> read keys
	genericExcluded := map[string]map[string]bool{} // api -> excluded keys
	for fn, apis := range genericSAExcludes {
		fi := s.fns["rules:"+fn]
		if fi == nil {
			continue // rotGuard already failed on the stale entry
		}
		keys := map[string]bool{}
		for k := range fi.reads[bSA] {
			keys[k] = true
		}
		if len(keys) == 0 {
			continue
		}
		genericKeys[fn] = keys
		for _, api := range apis {
			if genericExcluded[api] == nil {
				genericExcluded[api] = map[string]bool{}
			}
			for k := range keys {
				genericExcluded[api][k] = true
			}
		}
	}
	for key, fi := range s.fns {
		if !strings.HasPrefix(key, "rules:") || excludedSA[fi.name] || genericKeys[fi.name] != nil {
			continue
		}
		for k := range fi.reads[bSA] {
			rulesAllSA[k] = true
		}
	}
	// effects-generic: effects.Resolve runs the Condition* gate (conditionMet)
	// on EVERY SA chain regardless of api, so its closure joins the union the
	// same way the rules machinery does.
	effectsGeneric := map[bucket]map[string]bool{}
	if fi := s.fns["effects:Resolve"]; fi != nil {
		s.closureReads(fi, nil, map[string]bool{}, effectsGeneric)
	}
	for api, fn := range s.apiImpl {
		out := map[bucket]map[string]bool{}
		if fi := s.fns["effects:"+fn]; fi != nil {
			s.closureReads(fi, nil, map[string]bool{}, out)
		}
		keys := map[string]bool{}
		for k := range out[bSA] {
			keys[k] = true
		}
		for k := range rulesAllSA {
			keys[k] = true
		}
		for k := range effectsGeneric[bSA] {
			keys[k] = true
		}
		for k := range specialised[api] {
			keys[k] = true
		}
		// The genericSAExcludes keys: every generic function's read except the
		// apis the read genuinely never runs for.
		for _, ks := range genericKeys {
			for k := range ks {
				if !genericExcluded[api][k] {
					keys[k] = true
				}
			}
		}
		d.api[api] = keys
	}
	// trig: shared = triggerMatches minus the dispatch callees, plus the
	// drain root; each mode adds its dispatch function's closure.
	shared := map[bucket]map[string]bool{}
	if fi := s.fns["rules:Engine.triggerMatches"]; fi != nil {
		s.closureReads(fi, s.dispatchFns, map[string]bool{}, shared)
	}
	for _, root := range handRoots.trig {
		if fi := s.fns["rules:"+root]; fi != nil {
			s.closureReads(fi, nil, map[string]bool{}, shared)
		}
	}
	d.trig["Always"] = cloneKeySet(shared[bTrig]) // no dispatch branch of its own
	for mode, fn := range s.modeFns {
		keys := map[string]bool{}
		for k := range shared[bTrig] {
			keys[k] = true
		}
		out := map[bucket]map[string]bool{}
		if fi := s.fns["rules:"+fn]; fi != nil {
			s.closureReads(fi, nil, map[string]bool{}, out)
		}
		for k := range out[bTrig] {
			keys[k] = true
		}
		d.trig[mode] = keys
	}
	// stat: per mode, every activeStatics literal call site plus the
	// hand-declared roots. The apiSpecificRulesStat family roots' closures
	// are EXCLUDED from the generic union -- their reads execute only for
	// the family's synthetic mode, built below, and left here they would
	// mask every other static of the mode's genuinely unread keys.
	statFamRoots := map[string]bool{}
	for fn := range apiSpecificRulesStat {
		statFamRoots[fn] = true
	}
	for mode, roots := range s.statRoots {
		keys := map[string]bool{}
		for root := range roots {
			if statFamRoots[root] {
				continue // family root: attributed to the synthetic mode below
			}
			out := map[bucket]map[string]bool{}
			if fi := s.fns["rules:"+root]; fi != nil {
				// (the exclusion set also stops any OTHER root's closure
				// from descending into a family root)
				s.closureReads(fi, statFamRoots, map[string]bool{}, out)
			}
			for k := range out[bStat] {
				keys[k] = true
			}
		}
		d.stat[mode] = keys
	}
	for mode, roots := range handRoots.stat {
		keys := d.stat[mode]
		if keys == nil {
			keys = map[string]bool{}
		}
		for _, root := range roots {
			if statFamRoots[root] {
				continue // family root: attributed to the synthetic mode below
			}
			out := map[bucket]map[string]bool{}
			if fi := s.fns["rules:"+root]; fi != nil {
				s.closureReads(fi, statFamRoots, map[string]bool{}, out)
			}
			for k := range out[bStat] {
				keys[k] = true
			}
		}
		d.stat[mode] = keys
	}
	// synthetic family modes: the base mode's union PLUS each family root's
	// closure -- a family static is still a mode static and every generic
	// consumer of the mode still reads it. (The recognition helper's keys
	// never reach any read set: its map is stringMapParams-whitelisted.)
	// Accumulate across roots BEFORE assigning: several roots can share one
	// family (mayPlayGrant and warpGraveyardAllowed both feed
	// Continuous.MayPlay) and overwriting would make the family's set depend
	// on map iteration order.
	famReads := map[string]map[string]bool{}
	for fn, fam := range apiSpecificRulesStat {
		fi := s.fns["rules:"+fn]
		if fi == nil {
			continue // rotGuard already failed on the stale entry
		}
		out := map[bucket]map[string]bool{}
		s.closureReads(fi, nil, map[string]bool{}, out)
		if famReads[fam] == nil {
			famReads[fam] = map[string]bool{}
		}
		for k := range out[bStat] {
			famReads[fam][k] = true
		}
	}
	for fam, extra := range famReads {
		base := fam
		if i := strings.Index(fam, "."); i >= 0 {
			base = fam[:i]
		}
		keys := map[string]bool{}
		for k := range d.stat[base] {
			keys[k] = true
		}
		for k := range extra {
			keys[k] = true
		}
		if fam == "Continuous.MayPlay" {
			for k := range mayPlayStaticParamReads {
				keys[k] = true
			}
		}
		d.stat[fam] = keys
	}
	// repl: replacementMatches + the pipeline root. Every r.Params read in
	// them is scoped to repl:Moved by replacementMatches' early return.
	keys := map[string]bool{}
	for _, root := range append([]string{"Engine.replacementMatches"}, handRoots.repl...) {
		out := map[bucket]map[string]bool{}
		if fi := s.fns["rules:"+root]; fi != nil {
			s.closureReads(fi, nil, map[string]bool{}, out)
		}
		for k := range out[bRepl] {
			keys[k] = true
		}
	}
	d.repl["Moved"] = keys
	return d
}

// ---------------------------------------------------------------------------
// 3. The rot guard
// ---------------------------------------------------------------------------

// rotGuard collects every way a read could have escaped the census into
// s.guardErrs:
//
//   - any unclassified access collected during the scan;
//   - an effects function reading Params but unreachable from every
//     Register'd implementation (a primitive implemented but never
//     registered, or a helper only dead code reaches);
//   - a rules function reading trigger/static/replacement params outside
//     every attribution root for that bucket;
//   - a map[string]string parameter indexed but never attributed through a
//     `.Params`/alias call-site argument, or an unattributable argument;
//   - a stale apiSpecificRulesSA entry (the function is gone or no longer
//     reads SA params).
//
// A new read therefore cannot join the codebase without either being
// attributed or failing the guard; failGuard turns the findings into test
// failures and the probes inspect the list directly.
func (s *scan) rotGuard(t *testing.T) {
	t.Helper()
	// Every non-literal activeStatics call site must be covered by a
	// handRoots.stat declaration naming its function.
	for fname, sites := range s.ambiguousStat {
		covered := false
		for _, fns := range handRoots.stat {
			for _, f := range fns {
				if f == fname {
					covered = true
				}
			}
		}
		if !covered {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: activeStatics non-literal mode call in %s (%s) has no handRoots.stat attribution",
				fname, strings.Join(sites, ", ")))
		}
	}
	if len(s.unclassified) > 0 {
		sort.Strings(s.unclassified)
		s.guardErrs = append(s.guardErrs, fmt.Sprintf("paramcensus: %d unclassified Params reads (the census cannot rot):\n%s",
			len(s.unclassified), strings.Join(s.unclassified, "\n")))
	}
	// effects reachability.
	visited := map[string]bool{}
	for _, fn := range s.apiImpl {
		if fi := s.fns["effects:"+fn]; fi != nil {
			s.closureReads(fi, nil, visited, map[bucket]map[string]bool{})
		}
	}
	for key, fi := range s.fns {
		if !strings.HasPrefix(key, "effects:") {
			continue
		}
		total := 0
		for b := range fi.reads {
			total += len(fi.reads[b])
		}
		if total > 0 && !visited[fi.name] {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: effects function %s reads Params but is unreachable from every Register'd implementation", fi.name))
		}
	}
	// rules bucket reachability.
	for key, fi := range s.fns {
		if !strings.HasPrefix(key, "rules:") {
			continue
		}
		for _, b := range []bucket{bTrig, bStat, bRepl} {
			if len(fi.reads[b]) == 0 {
				continue
			}
			if !s.reachFromRoots(b, fi.name) {
				s.guardErrs = append(s.guardErrs, fmt.Sprintf(
					"paramcensus: rules function %s reads %s params outside every attribution root for that bucket", fi.name, b))
			}
		}
	}
	// Helper-passed maps: every indexed map[string]string parameter must have
	// been attributed through a call site (whitelisted non-card maps never
	// enter mapIdxRds at all).
	for key, fi := range s.fns {
		for param := range fi.mapIdxRds {
			if !s.mapAttributed[key+":"+param] {
				s.guardErrs = append(s.guardErrs, fmt.Sprintf(
					"paramcensus: %s indexes map[string]string parameter %q but no call site passes a Params map -- the helper is unreachable or mis-fed", fi.name, param))
			}
		}
	}
	sort.Strings(s.mapArgFailures)
	s.guardErrs = append(s.guardErrs, s.mapArgFailures...)
	// apiSpecificRulesStat must name real rules functions that still read
	// static params -- the stat-bucket twin of the apiSpecificRulesSA stale
	// check: a renamed or refactored function must not leave a silent stale
	// entry behind.
	for fn := range apiSpecificRulesStat {
		fi, ok := s.fns["rules:"+fn]
		if !ok {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: apiSpecificRulesStat names rules function %q, which no longer exists -- rename the entry", fn))
			continue
		}
		if len(fi.reads[bStat]) == 0 {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: apiSpecificRulesStat entry %q no longer reads static params -- delete the stale entry", fn))
		}
	}
	// statFamilyInternal caller discipline: every caller of a family-internal
	// reader must itself be an apiSpecificRulesStat root -- propagateKeyReads
	// lands the reader's map-indexed keys on the caller, so a generic caller
	// would carry the family's reads in its own read set and re-mask the
	// mode's genuinely unread keys (the exact defect this scoping fixed).
	for _, callee := range statFamilyInternal {
		for key, fi := range s.fns {
			if !strings.HasPrefix(key, "rules:") || !fi.calls[callee] {
				continue
			}
			if _, isRoot := apiSpecificRulesStat[fi.name]; !isRoot {
				s.guardErrs = append(s.guardErrs, fmt.Sprintf(
					"paramcensus: rules function %s calls the stat-family reader %s but is not an apiSpecificRulesStat root -- add it to apiSpecificRulesStat or move the call inside the family",
					fi.name, callee))
			}
		}
	}
	if !s.complete {
		return
	}
	// apiSpecificRulesSA must name real rules functions that still read SA
	// params -- a renamed or refactored function must not leave a silent
	// stale entry behind.
	for fn := range apiSpecificRulesSA {
		fi, ok := s.fns["rules:"+fn]
		if !ok {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: apiSpecificRulesSA names rules function %q, which no longer exists -- rename the entry", fn))
			continue
		}
		if len(fi.reads[bSA]) == 0 {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: apiSpecificRulesSA entry %q no longer reads SA params -- delete the stale entry", fn))
		}
	}
	// genericSAExcludes is the stale-entry twin (mirrors the apiSpecificRulesSA
	// check above).
	for fn := range genericSAExcludes {
		fi, ok := s.fns["rules:"+fn]
		if !ok {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: genericSAExcludes names rules function %q, which no longer exists -- rename the entry", fn))
			continue
		}
		if len(fi.reads[bSA]) == 0 {
			s.guardErrs = append(s.guardErrs, fmt.Sprintf(
				"paramcensus: genericSAExcludes entry %q no longer reads SA params -- delete the stale entry", fn))
		}
	}
}

// failGuard fails the test if rotGuard collected any findings.
func (s *scan) failGuard(t *testing.T) {
	t.Helper()
	if len(s.guardErrs) > 0 {
		sort.Strings(s.guardErrs)
		t.Fatalf("paramcensus rot guard: %d findings:\n%s", len(s.guardErrs), strings.Join(s.guardErrs, "\n"))
	}
}

// reachFromRoots reports whether fn is reachable from the roots declared for
// bucket b (no exclusions: reachability, not read attribution).
func (s *scan) reachFromRoots(b bucket, fn string) bool {
	var roots []string
	switch b {
	case bTrig:
		roots = append([]string{"Engine.triggerMatches"}, handRoots.trig...)
		for _, f := range s.modeFns {
			roots = append(roots, f)
		}
	case bStat:
		for mode := range s.statRoots {
			for root := range s.statRoots[mode] {
				roots = append(roots, root)
			}
		}
		for _, list := range handRoots.stat {
			roots = append(roots, list...)
		}
		// the apiSpecificRulesStat family roots are stat readers in their
		// own right (warpGraveyardAllowed has no activeStatics call at all);
		// they stay reachable-check roots even though their reads are
		// attributed to the synthetic family modes.
		for fn := range apiSpecificRulesStat {
			roots = append(roots, fn)
		}
	case bRepl:
		roots = append([]string{"Engine.replacementMatches"}, handRoots.repl...)
	default:
		return true
	}
	visited := map[string]bool{}
	var dfs func(fi *fnInfo)
	dfs = func(fi *fnInfo) {
		if visited[fi.name] {
			return
		}
		visited[fi.name] = true
		for c := range fi.calls {
			if target := s.fns[fi.pkg+":"+c]; target != nil {
				dfs(target)
			}
		}
	}
	for _, root := range roots {
		if fi := s.fns["rules:"+root]; fi != nil {
			dfs(fi)
		}
	}
	return visited[fn]
}

// ---------------------------------------------------------------------------
// 4. Structural keys, the ignore list, and the card-side census
// ---------------------------------------------------------------------------

// structuralKeys are the keys the cards IR itself consumes (cards/parse.go,
// cards/link.go, cards/keywords.go) rather than any effects/rules
// implementation: the line heads, the chain pointers, and the keyword-
// expansion tags. They are never `.Params[...]` read in Go because the parser
// turned them into structure (Trigger.Mode, Repl.With, sa.Sub...) before any
// primitive ran.
//
// Keyword and KeywordLine are generated by cards/keywords.go's
// expandKeywords, not read from any Forge script (measured at the corpus
// pin: ZERO raw cardsfolder lines carry `| Keyword$ ` or `KeywordLine$` --
// re-measure that claim before trusting this classification for a new
// corpus): every expanded ability/trigger/replacement is tagged
// Params["Keyword"] (head) and Params["KeywordLine"] (the full line) so
// nothing downstream needs to re-derive the difference. Both tags are
// consumed structurally: the KeywordLine tag IS the idempotence check in
// cards/keywords.go's `has` closure (the T:/R:/A: lookups at the top of
// expandKeywords), and rules/cast.go's entryETBChoice reads the Keyword tag
// ("ETBReplacement") to find an ETB replacement's target options. They
// are therefore never a per-primitive script parameter and are never
// measured.
var structuralKeys = func() map[string]map[string]bool {
	m := map[string]map[string]bool{
		"sa":   {"SubAbility": true},
		"trig": {"Mode": true, "Execute": true},
		"stat": {"Mode": true},
		"repl": {"Event": true, "ReplaceWith": true},
	}
	for _, keys := range m {
		keys["Keyword"] = true
		keys["KeywordLine"] = true
	}
	return m
}()

// ignoredParamKeys are keys the census deliberately does NOT report, each
// with the Forge behaviour that makes it presentation/AI-only. Verified
// against the pinned Forge checkout (~/projects/ref/forge; grep the key to
// see every consumer).
var ignoredParamKeys = map[string]string{
	// Forge citations below are relative to ~/projects/ref/forge. These keys
	// affect Forge AI or rendered text only, never its game rules.
	"AILogic":            "AI heuristic; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"AITgts":             "AI target hint; forge-game/src/main/java/forge/game/card/CardState.java",
	"AILifeThreshold":    "AI life threshold; forge-ai/src/main/java/forge/ai/AiController.java",
	"AINoRecursiveCheck": "AI recursion guard; forge-ai/src/main/java/forge/ai/ability/DelayedTriggerAi.java",
	"AIPhyrexianPayment": "AI payment preference; forge-ai/src/main/java/forge/ai/ComputerUtilMana.java",
	// SpellDescription is deliberately NOT ignored: legalActions renders it.
	"StackDescription":      "stack caption; forge-game/src/main/java/forge/game/card/CardFactory.java",
	"TriggerDescription":    "trigger caption; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"ChangeTypeDesc":        "search UI text; forge-game/src/main/java/forge/game/ability/effects/ChangeZoneEffect.java",
	"ValidDescription":      "valid-card UI text; forge-game/src/main/java/forge/game/card/Card.java",
	"ValidCardsDesc":        "valid-card UI text; forge-game/src/main/java/forge/game/card/Card.java",
	"CostDesc":              "alternate-cost UI text; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"ConditionDescription":  "condition display text; forge-game/src/main/java/forge/game/ability/SpellAbilityEffect.java",
	"PrecostDesc":           "cost-prompt prefix; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"TgtPrompt":             "target-prompt UI text; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"ChoiceTitle":           "choice-dialog title; forge-game/src/main/java/forge/game/card/CardFactoryUtil.java",
	"AdditionalDescription": "extra UI rules-text fragment; forge-game/src/main/java/forge/game/ability/effects/CharmEffect.java",
	// AdditionalDesc is the same class of UI fragment one ability at a time:
	// Forge reads it for the ability's rendered description only (the
	// reminder text after a cost, e.g. Dargo's "This spell costs {2} less to
	// cast for each permanent sacrificed this way"), never for its rules.
	"AdditionalDesc": "ability UI description; forge-game/src/main/java/forge/game/spellability/SpellAbility.java (SaParam.AdditionalDesc)",
	"VoteMessage":    "vote-dialog message; forge-game/src/main/java/forge/game/ability/effects/VoteEffect.java",
	"IsCurse":        "AI curse marker; forge-game/src/main/java/forge/game/spellability/SpellAbility.java",
	"Description":    "UI/dialog description; forge-game/src/main/java/forge/game/card/CardFactory.java",
	"SelectPrompt":   "search prompt message; forge-game/src/main/java/forge/game/ability/effects/ChangeZoneEffect.java",
	"Image":          "effect-token image key; forge-game/src/main/java/forge/game/card/CardFactory.java",
	// Ultimate$ marks a planeswalker's ultimate for the AI's ability ranking
	// and the achievement tracker; it gates nothing rules-side (the CR 606.3
	// loyalty gating is the loyalty COST and the permanent's once-per-turn
	// limit, both already enforced).
	"Ultimate": "AI ranking + achievement marker; forge-ai/src/main/java/forge/ai/ComputerUtilAbility.java, forge-ai/src/main/java/forge/ai/ComputerUtilCard.java, forge-game/src/main/java/forge/game/player/AchievementTracker.java",
	// UnlessAI is Forge's AI-side copy hint on a CopySpellAbility (Chain of
	// Vapor's UnlessAI$ ChainOfVapor): CopySpellAbilityAi.java reads it (the
	// aiLogic local) to rank WHEN the AI would pay the copy's unless cost. It
	// gates no rules-side behaviour — the pay-or-decline ask the shared
	// unless gate poses is the rules — so the census ignores it.
	"UnlessAI": "AI copy-eligibility hint; forge-ai/src/main/java/forge/ai/ability/CopySpellAbilityAi.java",
}

// ignoredStatParams scopes a presentation key to individual stat modes.
// ignoredParamKeys is consulted with the BARE key in every bucket, so a key
// some primitives genuinely read can never go in it. Secondary$ is that
// key: on a static it is Forge's card-text dedup marker --
// CardTraitBase.isSecondary()
// (forge-game/src/main/java/forge/game/CardTraitBase.java:179) reads the
// key, but every forge-game caller is Card.java's getText-family renderer
// (forge-game/src/main/java/forge/game/card/Card.java lines
// 2926/2958/2977/2993/3087/3096/3155/3291/3297/3303) and no
// staticability/cost/spellability execution path gates on it. Gorge however
// DOES read Secondary$ rules-side on two classes: the cost-modifier statics
// (rules/statics.go costModifiers' paired-text skip) and every trigger
// (rules/trigger_match.go secondaryYields -- the merged "one card text is
// not two triggers" behaviour, pinned by rules/param_combat_triggers_test.go
// and rules/magecraft_trigger_test.go). So the bare key stays out of
// ignoredParamKeys -- the trigger and cost-modifier reads must stay
// measurable (TestParamCensusDetectsADeletedConsumer's class) -- and the
// presentation modes are ignored scoped, here.
//
// RaiseCost/ReduceCost statics are deliberately NOT listed: they carry the
// live cost-modifier read, and the Continuous.MayPlay family's whitelist
// fail-closes a Secondary$-carrying grant (rules/layers.go mayPlayGrant) --
// a RECOGNITION the census keeps measurable exactly like MayPlayPlayer$.
// The modes below are the REGISTERED stat modes the corpus carries
// Secondary$ on (measured over the full corpus at the current pin); an
// unregistered mode's params are the primitive ratchet's business.
var statPresentationSecondary = []string{
	"Continuous", "CantBlockBy", "MustAttack", "CantBlock", "MinMaxBlocker",
	"CantBeActivated", "CantSacrifice", "CantBeCast", "CantAttack",
	"CastWithFlash", "CantGainLife", "CantTarget", "Panharmonicon",
}

var ignoredStatParams = func() map[string]string {
	const cite = "static text-dedup marker; forge-game/src/main/java/forge/game/CardTraitBase.java:179 (Card.java getText callers only)"
	m := make(map[string]string, len(statPresentationSecondary))
	for _, mode := range statPresentationSecondary {
		m[mode+".Secondary"] = cite
	}
	return m
}()

// ignoredParam is the census's single classification point for a parameter
// key: the key-global ignoredParamKeys table, then the stat-mode-scoped
// overlay (consulted only for a "stat:" primitive, so trigger, SA and
// replacement reads stay measurable no matter what lands in the overlay).
func ignoredParam(prim, key string) bool {
	if ignoredParamKeys[key] != "" {
		return true
	}
	if mode, ok := strings.CutPrefix(prim, "stat:"); ok {
		return ignoredStatParams[mode+"."+key] != ""
	}
	return false
}

// censusResult is one census run: per-card labels plus aggregate sets.
type censusResult struct {
	labels      map[string][]string // card (first-face name) -> sorted labels
	paramLabels []string            // distinct param: labels across the decks
	costLabels  []string            // distinct cost: labels across the decks
}

// measureParamCensus walks the repo decks' compiled cards and reports, per
// card, every parameter key a REGISTERED primitive's implementation does not
// read and every cost token ParseCost does not model. drop simulates a
// deleted consumer (primitive label, e.g. "api:Sacrifice" -> set of keys to
// treat as unread) so the plumbing is testable without editing production
// code. The zero-drop result is memoised: every ratchet run in one test
// binary shares it.
var (
	censusOnce  sync.Once
	censusBase  censusResult
	censusReads *derivedReads
	// censusGuardErrs carries the rot-guard findings out of the memoised
	// Once (nil when the guard passed). No t.Fatal-family call may run
	// inside censusOnce.Do: a Fatalf never returns, but go1.24+'s
	// `defer o.done.Store(true)` marks the once done on the Goexit anyway,
	// so censusReads would stay nil and every later census test in the
	// binary would nil-deref -- the SIGSEGV in
	// TestParamCensusAttributesSpecialisedRulesPaths that this gate round's
	// rot finding produced (failGuard Fatalfs inside the Once). The corpus
	// Skip is hoisted out the same way (a missing .cards/ CorpusRegistry
	// inside the Once would poison the memo identically), and the Fatalf
	// runs here, after Do returns normally, so every census test fails with
	// the real findings instead of a panic in an unrelated one.
	censusGuardErrs []string
)

func measureParamCensus(t *testing.T, drop map[string]map[string]bool) (censusResult, *derivedReads) {
	t.Helper()
	if drop == nil {
		// Per-test corpus decision FIRST: a missing .cards/ skips THIS test
		// here, before the Once, instead of skipping from inside it.
		testutil.CorpusRegistry(t)
		censusOnce.Do(func() {
			s := scanPackages(t)
			s.rotGuard(t)
			censusGuardErrs = s.guardErrs
			censusReads = s.derived()
			censusBase = walkRepoDeckCensus(t, censusReads, nil)
		})
		if len(censusGuardErrs) > 0 {
			sort.Strings(censusGuardErrs)
			t.Fatalf("paramcensus rot guard: %d findings:\n%s",
				len(censusGuardErrs), strings.Join(censusGuardErrs, "\n"))
		}
		return censusBase, censusReads
	}
	s := scanPackages(t)
	s.rotGuard(t)
	s.failGuard(t)
	d := s.derived()
	return walkRepoDeckCensus(t, d, drop), d
}

// faceUnknownCostLabels returns every unmodelled cost token from every
// face-owned value production passes to ParseCost: the printed ManaCost and
// the four parameterised casting keywords. SA/static Cost$ and UnlessCost$
// values are walked beside their owning primitive below. Keeping the
// face-owned sources together is deliberate: a new printed or keyword cost
// cannot silently evade the ratchet merely because it is not a Params entry.
func faceUnknownCostLabels(f *cards.Face) []string {
	inputs := []string{f.ManaCost}
	// The and/or two-part Kicker (Forge's colon-separated Kicker:<a>:<b>,
	// Wastescape Battlemage's "Kicker {G} and/or {1}{U}"): its parts are
	// parsed SEPARATELY by rules' cast-time option family (twoPartKickerCosts
	// offers one cast per payable part), so the census walks the same two
	// parses -- a clean two-part form labels nothing, and a colon form whose
	// parts do not parse falls back to the raw string so the unknown stays
	// labelled. A colon-free param is the single-cost Kicker and parses raw.
	if _, _, two := twoPartKickerCosts(f); two {
		// both parts parsed clean inside twoPartKickerCosts; nothing to label
	} else if cost, ok := f.KeywordParam("Kicker"); ok {
		inputs = append(inputs, cost)
	}
	for _, keyword := range [...]string{"Surge", "Flashback", "Miracle"} {
		if cost, ok := f.KeywordParam(keyword); ok {
			inputs = append(inputs, cost)
		}
	}
	seen := map[string]bool{}
	for _, input := range inputs {
		for _, token := range ParseCost(input).Unknown {
			seen["cost:"+token] = true
		}
	}
	out := make([]string, 0, len(seen))
	for label := range seen {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// statCensusMode is the census's read-set key for one static: the plain
// Mode$, except for the apiSpecificRulesStat families -- a Mode$ Continuous
// static carrying a MayPlay$ value is read by the MayPlay grant path
// (mayPlayGrant/mayPlayStatic/warpGraveyardAllowed), whose keys the census
// attributes to the synthetic "Continuous.MayPlay" mode instead of the
// generic Continuous union. Measured at the corpus pin, ONLY Mode$
// Continuous lines carry MayPlay$ (656 of 656 MayPlay$ lines), so any other
// mode stays on its plain read set -- a latent hole named here: if a future
// corpus adds MayPlay$ to another mode, extend this function and
// apiSpecificRulesStat together.
func statCensusMode(mode string, params map[string]string) string {
	if mode == "Continuous" && strings.TrimSpace(params["MayPlay"]) != "" {
		return "Continuous.MayPlay"
	}
	return mode
}

// cardCensusLabels is the card-side census walk shared by the repo-deck
// ratchet and focused fixtures. Params are labelled only for REGISTERED
// primitives (an unimplemented primitive's unread params are noise -- the
// first ratchet owns the card); cost tokens are labelled regardless, because
// an unmodelled token in a cost string is a real silent substitution even
// when the primitive around it is already unsupported.
//
// The walk also parses EVERY SVar in each face's own SVar table through
// cards.ResolveSVar and censuses the resulting bodies like linked
// sub-abilities. This is the conservative superset of walking only the
// execution-bearing SVar-reference parameters -- `Choices$` (effCharm),
// `RepeatSubAbility$`/`RepeatEach` (effRepeat/effRepeatEach), Branch's
// True/FalseSubAbility$ (resumeResolution) and the delayed trigger's
// `Execute$` (checkDelayedTriggers) -- so the next execution-bearing
// parameter is covered without touching this walk (see the file-head scope
// note). One visit per name; ResolveSVar's depth cap bounds any body's own
// SubAbility$ chain.
func cardCensusLabels(c *cards.Card, d *derivedReads, drop map[string]map[string]bool) []string {
	labels := map[string]bool{}
	addLabel := func(l string) { labels[l] = true }
	labelFor := func(prim, key string) string { return "param:" + prim + "." + key }
	for _, f := range c.Faces {
		for _, label := range faceUnknownCostLabels(f) {
			addLabel(label)
		}
		var walk func(sa *cards.SA)
		walk = func(sa *cards.SA) {
			if sa == nil {
				return
			}
			prim := "api:" + sa.API
			readSet := d.api[sa.API]
			for k, v := range sa.Params {
				if k == "Cost" || k == "UnlessCost" {
					for _, tok := range ParseCost(v).Unknown {
						addLabel("cost:" + tok)
					}
				}
				if readSet == nil {
					continue // unregistered primitive: ratchet 1 owns it
				}
				if structuralKeys["sa"][k] || ignoredParam(prim, k) {
					continue
				}
				if !readSet[k] || drop != nil && drop[prim][k] {
					addLabel(labelFor(prim, k))
				}
			}
			walk(sa.Sub)
		}
		for _, a := range f.Abilities {
			walk(a)
		}
		for _, tr := range f.Triggers {
			prim := "trig:" + tr.Mode
			readSet := d.trig[tr.Mode]
			if readSet != nil {
				for k := range tr.Params {
					if structuralKeys["trig"][k] || ignoredParam(prim, k) {
						continue
					}
					if !readSet[k] || drop != nil && drop[prim][k] {
						addLabel(labelFor(prim, k))
					}
				}
			}
			walk(tr.Effect)
		}
		for _, st := range f.Statics {
			mode := statCensusMode(st.Mode, st.Params)
			prim := "stat:" + mode
			readSet := d.stat[mode]
			for k, v := range st.Params {
				if k == "Cost" || k == "UnlessCost" {
					for _, tok := range ParseCost(v).Unknown {
						addLabel("cost:" + tok)
					}
				}
				if readSet == nil {
					continue // unregistered static mode: ratchet 1 owns it
				}
				if structuralKeys["stat"][k] || ignoredParam(prim, k) {
					continue
				}
				if !readSet[k] || drop != nil && drop[prim][k] {
					addLabel(labelFor(prim, k))
				}
			}
		}
		for _, r := range f.Repls {
			prim := "repl:" + r.Event
			readSet := d.repl[r.Event]
			if readSet != nil {
				for k := range r.Params {
					if structuralKeys["repl"][k] || ignoredParam(prim, k) {
						continue
					}
					if !readSet[k] || drop != nil && drop[prim][k] {
						addLabel(labelFor(prim, k))
					}
				}
			}
			walk(r.With)
		}
		// SVar bodies the Link pass did not attach (see the comment above):
		// one visit per name through the shared reachability rule, so this
		// census and Face.Primitives cannot disagree about which SVar bodies
		// are reachable. A body that is not an ability (Count$ expressions
		// behind ConditionCheckSVar$/SVarCompare$) fails parseSA and yields
		// nil, so EachSVarAbility never calls walk for it.
		f.EachSVarAbility(func(sa *cards.SA) { walk(sa) })
		// Raw Effect children have no SP$/AB$ head, so Link cannot attach
		// them. Use the cards-owned typed traversal, and apply the same
		// trigger/static/replacement read sets as printed lines.
		f.EachRawEffectChild(func(child cards.EffectChild) {
			if child.Trigger != nil {
				tr := child.Trigger
				prim, readSet := "trig:"+tr.Mode, d.trig[tr.Mode]
				if readSet == nil {
					return
				}
				for k := range tr.Params {
					if structuralKeys["trig"][k] || ignoredParam(prim, k) {
						continue
					}
					if !readSet[k] || drop != nil && drop[prim][k] {
						addLabel(labelFor(prim, k))
					}
				}
				return
			}
			if child.Static != nil {
				st := child.Static
				mode := statCensusMode(st.Mode, st.Params)
				prim, readSet := "stat:"+mode, d.stat[mode]
				for k, v := range st.Params {
					if k == "Cost" || k == "UnlessCost" {
						for _, tok := range ParseCost(v).Unknown {
							addLabel("cost:" + tok)
						}
					}
					if readSet == nil {
						continue
					}
					if structuralKeys["stat"][k] || ignoredParam(prim, k) {
						continue
					}
					if !readSet[k] || drop != nil && drop[prim][k] {
						addLabel(labelFor(prim, k))
					}
				}
				return
			}
			r := child.Repl
			prim, readSet := "repl:"+r.Event, d.repl[r.Event]
			if readSet == nil {
				return
			}
			for k := range r.Params {
				if structuralKeys["repl"][k] || ignoredParam(prim, k) {
					continue
				}
				if !readSet[k] || drop != nil && drop[prim][k] {
					addLabel(labelFor(prim, k))
				}
			}
		})
	}
	out := make([]string, 0, len(labels))
	for label := range labels {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// walkRepoDeckCensus applies the card-side walk to each distinct repo-deck
// card and records its per-card plus aggregate labels.
func walkRepoDeckCensus(t *testing.T, d *derivedReads, drop map[string]map[string]bool) censusResult {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	res := censusResult{labels: map[string][]string{}}
	paramSeen := map[string]bool{}
	costSeen := map[string]bool{}
	cardSeen := map[string]bool{}
	for _, name := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, name) {
			cardName := c.Faces[0].Name
			if cardSeen[cardName] {
				continue
			}
			cardSeen[cardName] = true
			labels := cardCensusLabels(c, d, drop)
			if len(labels) == 0 {
				continue
			}
			res.labels[cardName] = labels
			for _, label := range labels {
				if strings.HasPrefix(label, "param:") {
					paramSeen[label] = true
				} else {
					costSeen[label] = true
				}
			}
		}
	}
	for label := range paramSeen {
		res.paramLabels = append(res.paramLabels, label)
	}
	for label := range costSeen {
		res.costLabels = append(res.costLabels, label)
	}
	sort.Strings(res.paramLabels)
	sort.Strings(res.costLabels)
	return res
}

// ---------------------------------------------------------------------------
// 5. The ratchet and its probes
// ---------------------------------------------------------------------------

// knownUnsupportedParams is the PARAMETER ratchet (task
// inbox-engine-ratchet-parameter-census), measured by
// TestEveryRepoDeckParamsAreRead on its first run against the current repo
// decks and seeded by hand, exactly like knownUnsupported. Label grammar:
//
//	param:<Primitive>.<Key>  a key the card's script carries on a registered
//	                         primitive whose implementation never reads it
//	                         (e.g. param:api:ChangeZone.GainControl);
//	cost:<Token>             a cost token ParseCost silently substitutes
//	                         generic mana for (e.g. cost:PayEnergy).
//
// Presentation/AI keys are never measured (ignoredParamKeys above, each with
// its Forge citation; stat-mode-scoped ones through ignoredStatParams). The table is checked in both directions -- a newly
// unread key is a regression; a stale entry means the key is now read and
// must be deleted -- so it only ever shrinks, and only when a real read or a
// real ParseCost model is added.
var knownUnsupportedParams = map[string][]string{
	// Arcane Denial's param:api:Draw.Upto entry was deleted when Upto$ read
	// a real per-target "draw up to N" ask (task mordorparams1,
	// effects/cardflow.go effDraw's upto branch, rules' draw_upto resume
	// arm) — pinned by TestArcaneDenialSlowtripDrawsUpToTwo.
	"Arcane Denial":       {"param:api:Counter.RememberTargets"},
	"Avengers Quinjet":    {"param:api:ChangeZone.ValidTgtsDesc"},
	"Acclaimed Contender": {"param:api:Dig.RestRandomOrder"},
	// Adeline, Resplendent Cathar's param:api:RepeatEach.ChangeZoneTable entry
	// was deleted when the parameter became read (task agent-20260922T090929Z-
	// 07378594): effRepeatEach opens the zone batch the parameter asks for, so
	// the census now sees it read.
	// Captain Marvel, Apex Avenger's param:api:PutCounter.Placer label was
	// deleted when the bare-Choices$ PutCounter pick read Placer$ (task
	// vow1, effects/counters.go putCounterChoose) -- the static scan now
	// sees the read in effPutCounter's closure; its TriggeredCounterMap$
	// shape stays unread and labelled. The param:api:PutCounter.Optional
	// label was deleted when the Optional$ True election read landed
	// (effects/counters.go effPutCounter's put_optional ask) -- the may-put
	// election is pinned end to end in rules/putcounter_optional_test.go.
	"Captain Marvel, Apex Avenger": {"param:api:PutCounter.TriggeredCounterMap"},
	"Conduit of Worlds":            {"param:api:Play.RememberPlayed"},
	"Conjurer's Mantle":            {"param:api:Dig.RestRandomOrder"},
	"Director Nick Fury":           {"param:api:Dig.RestRandomOrder"},
	// Gift of Immortality's param:api:ChangeZone.ForgetOtherRemembered label
	// (and the whole entry) was deleted when the ForgetOtherRemembered read
	// landed (ticket agent-20260919T181318Z-316d7b2a): effChangeZone and
	// effChangeZoneAll clear the prior remembered set before re-remembering
	// (RememberChanged$), pinned end to end on the real corpus carrier The
	// Mimeoplasm in rules/mimeoplasm_forget_test.go (its MimeoExile /
	// MimeoChooseCopy chain) and at the bookkeeping choke points in
	// effects/forget_remembered_test.go.
	// Haakon, Stromgald Scourge's param:stat:Continuous.MayPlay.ValidAfterStack
	// entry was deleted when mayPlayStatic began consuming ValidAfterStack$ as
	// a derived spell filter (task mayplay-validafterstack).
	"Hercules, Olympian Hero": {"param:trig:DamageDoneOnce.FirstTime"},
	"Heroic Return":           {"param:api:ChangeZone.ValidTgtsDesc"},
	// Heroic Sacrifice's param:api:PutCounter.EachFromSource entry was deleted
	// when the CounterType$ EachFromSource copy-each-kind shape was read
	// (task eachfromsource, effects/counters.go effPutCounter's dispatch) --
	// the shape is pinned end to end on real corpus carriers in
	// rules/eachfromsource_test.go (Resourceful Defense, The Ozolith, Denry
	// Klin, Ambitious Augmenter, Zack Fair). Heroic Sacrifice's own carrier
	// path (its delayed trigger, Mode$ ChangesZone) stays unimplemented and
	// the card's OTHER labels above are untouched.
	"Heroic Sacrifice":          {"param:api:Effect.ValidTgtsDesc", "param:api:PutCounter.ValidTgtsDesc", "param:api:ReplaceEffect.VarType"},
	"Iron Man, Armored Avenger": {"param:api:PutCounter.ValidTgtsDesc"},
	// (Love on the Battlefield's param:trig:AttackersDeclared.NoResolvingCheck
	// row retired when the NoResolvingCheck$ read landed: the resolution-time
	// CR 603.4 recheck skips a trigger carrying the param
	// (rules/trigger_condition.go noResolvingCheck/triggerResolvingCheckHolds,
	// consulted by resolveTop) -- pinned end to end on the real corpus
	// carrier Ugin's Mastery in rules/no_resolving_check_test.go, with a
	// no-param control proving the recheck stays live for everyone else.)
	"Methods of the Mighty":   {"param:api:Destroy.ValidTgtsDesc"},
	"Mogis, God of Slaughter": {"param:stat:Continuous.RemoveType"},
	// Opposition Agent's and Rakdos, the Muscle's MayPlayIgnoreColor$/
	// MayPlayIgnoreType$ keys are consumed by effects/mayPlayParams; their
	// former labels were analyzer attribution gaps, not unsupported params.
	"Patriot, Shield Wielder": {"param:api:Pump.ValidTgtsDesc"},
	// (Photon, Mighty Marvel's param:api:Mana.PersistentMana row retired when
	// the PersistentMana$ read landed — the pm ManaAdd suffix, ManaClear's
	// partial clear and the TurnChange expiry — pinned end to end on the real
	// corpus carrier Rousing Refrain in rules/persistent_mana_test.go.)
	"Purphoros, God of the Forge": {"param:stat:Continuous.RemoveType"},
	"Rescue, Pepper Potts":        {"param:api:ChangeZone.ValidTgtsDesc"},
	// Scarlet Witch, Chaotic Avenger's param:api:Dig.WithMayLook entry was
	// deleted when effDig's shared face-down marker learned to read
	// WithMayLook$ (ticket agent-20260919T181318Z-631ddc68): the exiling
	// effect's controller is recorded as the one player who may look at the
	// face-down exiled card (effects/zone.go applyFaceDownMarker, the
	// "exiled_with_face_down_maylook" MoveZone marker; view/cardViews redacts
	// to that looker), pinned end to end on the real corpus Ixhel carrier in
	// rules/dig_withmaylook_test.go. The Dig library-position and
	// random-order variants are separate, already-listed gaps.
	"Speed, Young Avenger": {"param:api:Effect.ValidTgtsDesc"},
	// (Spinerock Knoll and West Coast Expansion's param:api:Play.Controller
	// / param:api:Play.WithoutManaCost rows retired when the Play
	// Controller$ read landed and the play resume arm's rider reads were
	// attributed to api:Play — ticket agent-20260918T221252Z-d504b33b; the
	// vaan_forget_played_test.go pair is the free-cast end-to-end pin.)
	// Vesuva's IntoPlayTapped$ is read on the ETB replacement path.
	"Sundering Eruption": {"param:stat:Continuous.AddHiddenKeyword"},
	// West Coast Expansion's param:api:Play.Controller /
	// param:api:Play.WithoutManaCost row retired with the same attribution
	// fix (see the Spinerock Knoll note above).
	"World Shaper": {"param:api:Mill.Optional"},
	// Torment of Hailfire's FallbackAbility$/TempRemember$ are unread
	// everywhere: its DB$ GenericChoice now resolves through effCharm's
	// modal ask (effects/misc.go), but these two params ride the ask and
	// neither is read by any code (pinned in rules/generic_choice_test.go).
	"Torment of Hailfire": {"param:api:GenericChoice.FallbackAbility", "param:api:GenericChoice.TempRemember"},
	// The pro-shaper player-submitted Commander import (2026-09-18): the
	// parameter reads its cards expose that this build does not implement.
	// Each label is the unimplemented parameter on a fully-registered
	// primitive (the primitive ratchet above separately carries the four
	// unregistered APIs/keywords the deck needs).
	"Chord of Calling":      {"param:api:ChangeZone.AIXMax"},
	"Earthbender Ascension": {"param:api:PutCounter.RememberAmount"},
	"Glacial Chasm":         {"param:api:Sacrifice.ChangeNum"},
	"Green Sun's Zenith":    {"param:api:ChangeZone.AIXMax"},
	"Natural Order":         {"param:api:ChangeZone.AISearchGoal"},
	// The Science! (pip) Commander precon import (2026-09-26,
	// internal/testutil/decks/science-pip.json). Its 90 distinct cards expose
	// exactly two parameter gaps, both measured by the first ratchet run; the
	// deck added no primitive gap (C.A.M.P.'s kw:Fortify was already
	// implemented). Overencumbered's cost token Y is the {Y} Phyrexian-style
	// generic payment ParseCost does not model; Expert-Level Safe's
	// ChooseNumber Secretly/MatchedAbility/UnmatchedAbility shape is a
	// face-down guessed-number ask this build never poses (the card is
	// otherwise fully registered).
	"Expert-Level Safe": {"param:api:ChooseNumber.MatchedAbility", "param:api:ChooseNumber.Secretly", "param:api:ChooseNumber.UnmatchedAbility"},
	"Overencumbered":    {"cost:Y"},
}

// TestEveryRepoDeckParamsAreRead is the parameter ratchet: every card across
// the repo decks carries only the unread parameters and unmodelled cost
// tokens knownUnsupportedParams lists, and every entry in the table is still
// measured. Same failure style as the primitive ratchet.
func TestEveryRepoDeckParamsAreRead(t *testing.T) {
	t.Parallel()
	res, _ := measureParamCensus(t, nil)
	t.Logf("param census: %d of %d distinct repo-deck cards carry at least one unread param or unmodelled cost token; %d distinct param labels, %d distinct cost labels",
		len(res.labels), distinctRepoDeckCards(t), len(res.paramLabels), len(res.costLabels))
	for card, got := range res.labels {
		want, ok := knownUnsupportedParams[card]
		if !ok {
			t.Errorf("%s carries unread params %v, which is not in knownUnsupportedParams -- new gap, add it to the table", card, got)
			continue
		}
		if !sameSet(want, got) {
			t.Errorf("%s: knownUnsupportedParams says %v, measured %v -- update the table to match", card, want, got)
		}
	}
	for card, want := range knownUnsupportedParams {
		if _, still := res.labels[card]; !still {
			t.Errorf("%s now reads everything knownUnsupportedParams listed (%v) -- delete the stale entry", card, want)
		}
	}
}

// TestParamCensusScanIsComplete is the rot guard on its own: the scan must
// classify every Params read in effects and rules, and every read must sit
// inside an attribution root. Called from measureParamCensus too; this
// standalone form keeps the failure visible without a corpus present.
func TestParamCensusScanIsComplete(t *testing.T) {
	s := scanPackages(t)
	s.rotGuard(t)
	s.failGuard(t)
}

// TestParamCensusDetectsADeletedConsumer is the done-means probe: pretend the
// SacValid$ read in effects' Sacrifice implementation was deleted (the scan
// drops that key from that primitive's read set) and assert the census then
// reports param:api:Sacrifice.SacValid for exactly the repo-deck cards whose
// Sacrifice abilities carry SacValid$ -- while the real baseline reports none.
func TestParamCensusDetectsADeletedConsumer(t *testing.T) {
	t.Parallel()
	base, d := measureParamCensus(t, nil)
	if !d.api["Sacrifice"]["SacValid"] {
		t.Fatalf("scan no longer derives the SacValid$ read for api:Sacrifice -- the consumer was deleted for real; fix the census or re-seed")
	}
	for _, labels := range base.labels {
		for _, l := range labels {
			if l == "param:api:Sacrifice.SacValid" {
				t.Fatalf("baseline already reports param:api:Sacrifice.SacValid -- SacValid$ is unread on main; this probe is stale")
			}
		}
	}
	// The cards the probe must move: every deck card carrying SacValid$ on an
	// api:Sacrifice ability, computed by the same walk the census uses.
	carriers := map[string]bool{}
	reg := testutil.CorpusRegistry(t)
	for _, name := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, name) {
			cn := c.Faces[0].Name
			for _, f := range c.Faces {
				var w func(sa *cards.SA)
				w = func(sa *cards.SA) {
					if sa == nil {
						return
					}
					if sa.API == "Sacrifice" {
						if _, ok := sa.Params["SacValid"]; ok {
							carriers[cn] = true
						}
					}
					w(sa.Sub)
				}
				for _, a := range f.Abilities {
					w(a)
				}
				for _, tr := range f.Triggers {
					w(tr.Effect)
				}
				for _, r := range f.Repls {
					w(r.With)
				}
			}
		}
	}
	if len(carriers) == 0 {
		t.Fatalf("no repo-deck card carries SacValid$ on an api:Sacrifice ability -- probe measures nothing")
	}
	res, _ := measureParamCensus(t, map[string]map[string]bool{"api:Sacrifice": {"SacValid": true}})
	for card := range carriers {
		labels := res.labels[card]
		hit := false
		for _, l := range labels {
			if l == "param:api:Sacrifice.SacValid" {
				hit = true
				break
			}
		}
		if !hit {
			t.Errorf("%s: dropped SacValid$ read but census did not report it (labels %v)", card, labels)
			continue
		}
		t.Logf("deleted-consumer probe: %s reports param:api:Sacrifice.SacValid", card)
	}
	if t.Failed() {
		return
	}
	affected := 0
	for card, labels := range res.labels {
		hit := false
		for _, l := range labels {
			if l == "param:api:Sacrifice.SacValid" {
				hit = true
				break
			}
		}
		if hit {
			affected++
			if !carriers[card] {
				t.Errorf("%s reported param:api:Sacrifice.SacValid but carries no SacValid$ on a Sacrifice ability", card)
			}
		}
	}
	if affected != len(carriers) {
		t.Errorf("probe reported %d affected cards, %d carriers measured", affected, len(carriers))
	}
}

// TestParamCensusScopesSecondaryByPrimitive pins the Secondary$ scoping:
// on a stat the key is Forge's text-dedup marker, ignored mode-scoped via
// ignoredStatParams (so a synthetic Secondary$ Continuous static labels
// nothing even when the scan is told its read was deleted), while the
// trigger read (rules/trigger_match.go secondaryYields) stays measurable --
// dropping that read makes a synthetic Secondary$ trigger label. A bare
// ignoredParamKeys["Secondary"] entry would have suppressed BOTH halves;
// this probe fails if either side regresses.
func TestParamCensusScopesSecondaryByPrimitive(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	if d.stat["Continuous"]["Secondary"] {
		t.Fatalf("stat:Continuous now derives a Secondary$ read -- the ignoredStatParams classification is stale, delete the mode")
	}
	if !d.trig["Phase"]["Secondary"] {
		t.Fatalf("trig:Phase no longer derives a Secondary$ read -- secondaryYields was deleted for real; fix the census or re-classify")
	}
	staticCard, diags := cards.ParseBytes("census-probe-static.txt", []byte(
		"Name: Census Probe Static\nTypes: Creature\nS:Mode$ Continuous | Affected$ Card.Self | AddPower$ 1 | Secondary$ True\n"))
	if len(diags) != 0 {
		t.Fatalf("static probe card failed to parse: %v", diags)
	}
	trigCard, diags := cards.ParseBytes("census-probe-trig.txt", []byte(
		"Name: Census Probe Trigger\nTypes: Creature\nT:Mode$ Phase | Phase$ End of Turn | TriggerDescription$ probe | Secondary$ True\n"))
	if len(diags) != 0 {
		t.Fatalf("trigger probe card failed to parse: %v", diags)
	}
	dropped := map[string]map[string]bool{
		"stat:Continuous": {"Secondary": true},
		"trig:Phase":      {"Secondary": true},
	}
	for _, l := range cardCensusLabels(staticCard, d, dropped) {
		if l == "param:stat:Continuous.Secondary" {
			t.Errorf("static Secondary$ labelled despite the mode-scoped ignore -- the classification is not consulted")
		}
	}
	hit := false
	for _, l := range cardCensusLabels(trigCard, d, dropped) {
		if l == "param:trig:Phase.Secondary" {
			hit = true
		}
	}
	if !hit {
		t.Errorf("dropped trigger Secondary read did not label param:trig:Phase.Secondary -- trigger Secondary$ measurability is broken")
	}
}

// TestParamCensusIgnoresValidCardsDesc pins the ValidCardsDesc$
// classification (issue agent-20260920T073130Z-724f9676). ValidCardsDesc$
// is the UI description of a ValidCards$ spec -- Forge reads it only to
// render text (forge-game/src/main/java/forge/game/card/Card.java), the same
// consumer as the already-ignored ValidDescription -- so it is a key-global
// presentation key, not an engine gap. The test drives the real measured
// carrier (Indulgent Aristocrat's api:PutCounterAll) through
// cardCensusLabels: it first asserts the precondition that the card DOES
// carry the key, then that the classification suppresses the label. Removing
// ignoredParamKeys["ValidCardsDesc"] makes it fail with the label reported.
func TestParamCensusIgnoresValidCardsDesc(t *testing.T) {
	t.Parallel()
	_, d := measureParamCensus(t, nil)
	if !ignoredParam("api:PutCounterAll", "ValidCardsDesc") {
		t.Fatalf("ignoredParamKeys no longer classifies ValidCardsDesc as presentation-only -- the census will report a false positive")
	}
	reg := testutil.CorpusRegistry(t)
	c, ok := reg.Lookup("Indulgent Aristocrat")
	if !ok {
		t.Fatalf("corpus is missing Indulgent Aristocrat -- the measured ValidCardsDesc$ carrier")
	}
	carries := false
	for _, f := range c.Faces {
		for _, a := range f.Abilities {
			if a.API == "PutCounterAll" && a.Params["ValidCardsDesc"] != "" {
				carries = true
			}
		}
	}
	if !carries {
		t.Fatalf("Indulgent Aristocrat no longer carries ValidCardsDesc$ on a PutCounterAll ability -- the pin measures nothing")
	}
	if d.api["PutCounterAll"] == nil {
		t.Fatalf("api:PutCounterAll is no longer a registered primitive -- the census skips its params and this pin cannot fail")
	}
	for _, l := range cardCensusLabels(c, d, nil) {
		if l == "param:api:PutCounterAll.ValidCardsDesc" {
			t.Errorf("census reported %s despite the presentation-only classification", l)
		}
	}
}

// TestParamCensusPinsTheImportReviewExamples pins the examples the task
// brief was written from: Daze's Return<1/Island> alternative cost (ParseCost
// silently substituting generic mana), Chandra's SubCounter<X/LOYALTY>,
// Relic of Progenitus's bare Exile<1/Card.YouOwn> and Vexing Devil's
// DamageYou<1> -- all unmodelled heads when the pin was written. The cost
// token family work (announced PayLife<X>, bare Exile<N/Spec>, the Draw
// bucket, SubCounter<X/Kind>, DamageYou<N> in ParseCost) now models every
// one, so the pin asserts they are GONE -- cost: labels only ever shrink
// when a real ParseCost model lands. Force of Will's
// ExileFromHand<1/Card.Blue+Other> and Whirler Rogue's tapXType<2/Artifact>
// retired earlier with the alternative-cost work (the
// ExileFromHand/ExileFromGrave/Reveal/Behold/tapXType/Blight/Forage heads).
// (The brief's other example, Reanimate's GainControl$ on api:ChangeZone, is
// not in any repo deck; the same label appears in the baseline on Meathook
// Massacre II and retires the moment the read is implemented.)
func TestParamCensusPinsTheImportReviewExamples(t *testing.T) {
	res, _ := measureParamCensus(t, nil)
	// Every cost example this pin once demanded PRESENT has retired with a
	// real ParseCost model; they joined the gone-side assertions below. The
	// original pin's Daze/Force of Will/Whirler Rogue notes remain the
	// comment above.
	for card, label := range map[string]string{
		"Force of Will": "cost:ExileFromHand",
		"Whirler Rogue": "cost:tapXType",
		// Daze's Return<1/Island> alternative cost retired with the
		// Rakdos-params work: ParseCost models Return<N/Spec> (the source or a
		// matching battlefield permanent returned to its owner's hand), so the
		// silent one-generic substitution is gone.
		"Daze": "cost:Return",
		// The cost token family work (announced PayLife<X>, bare
		// Exile<N/Spec>, the Draw bucket, SubCounter<X/Kind>, DamageYou<N>).
		"Relic of Progenitus":       "cost:Exile",
		"Chandra, Awakened Inferno": "cost:SubCounter",
		"Vexing Devil":              "cost:DamageYou",
	} {
		for _, l := range res.labels[card] {
			if l == label {
				t.Errorf("%s: %s is back in the census -- ParseCost stopped modelling the token", card, label)
			}
		}
	}
}

// TestParseCostReportsUnmodelledCostTokens pins the reporting half: tokens
// ParseCost does not model land in Cost.Unknown; modelled ones never do.
// TestParamCensusCatchesAliasedParamsReads pins the alias finding: a
// consumer that reads the Params map through a local alias (`p := sa.Params;
// p["SacValid"]`) must be DERIVED by the scan -- the old selector-shape-only
// scan silently skipped it, and an unreachable aliased consumer passed
// TestParamCensusScanIsComplete. Both halves are pinned: the read is
// attributed, and the unreachable consumer fails the rot guard by name.
func TestParamCensusCatchesAliasedParamsReads(t *testing.T) {
	s := newScan()
	s.scanSource(t, "probe_alias.go", "effects", `package effects

import "github.com/adams-shaun/gorge/cards"

func censusProbeAlias(sa *cards.SA) string {
	params := sa.Params
	chained := params
	return chained["SacValid"]
}
`)
	if fi := s.fns["effects:censusProbeAlias"]; fi == nil || !fi.reads[bSA]["SacValid"] {
		t.Fatalf("aliased (and chained-alias) Params read not derived -- the scan is still shape-matched: %+v", fi)
	}
	s.rotGuard(t)
	flagged := false
	for _, err := range s.guardErrs {
		if strings.Contains(err, "censusProbeAlias") && strings.Contains(err, "unreachable") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("rot guard did not flag the unreachable aliased consumer; findings: %v", s.guardErrs)
	}
}

// TestParamCensusCatchesHelperPassedMaps pins the helper-passed-map finding:
// a function whose parameter is a map[string]string gets its index reads
// attributed through the call sites' arguments (so the CALLER carries the
// read with the argument's bucket), and a call passing a non-Params map --
// or no call at all -- fails the rot guard.
func TestParamCensusCatchesHelperPassedMaps(t *testing.T) {
	s := newScan()
	s.scanSource(t, "probe_helper.go", "effects", `package effects

import "github.com/adams-shaun/gorge/cards"

func censusProbeHelper(m map[string]string) string {
	return m["SacValid"]
}

func censusProbeCaller(sa *cards.SA) string {
	return censusProbeHelper(sa.Params)
}
`)
	s.propagateKeyReads()
	if fi := s.fns["effects:censusProbeCaller"]; fi == nil || !fi.reads[bSA]["SacValid"] {
		t.Fatalf("helper-passed Params read not attributed to the caller: %+v", fi)
	}
	s.rotGuard(t)
	// (The caller's own unreachability finding is inherent to synthetic code;
	// what must hold is the helper attribution: no non-Params / unattributed
	// finding for the helper or its call site.)
	for _, err := range s.guardErrs {
		if strings.Contains(err, "not a Params map") || strings.Contains(err, "no call site passes") {
			t.Fatalf("helper attribution failed: %s", err)
		}
	}

	// The same helper fed a non-Params map[string]string (here an alias of
	// the face's SVars table) must fail the guard instead of passing.
	s2 := newScan()
	s2.scanSource(t, "probe_helper2.go", "effects", `package effects

import "github.com/adams-shaun/gorge/cards"

func censusProbeHelper2(m map[string]string) string {
	return m["SacValid"]
}

func censusProbeCaller2(sa *cards.SA) string {
	svars := sa.SVars
	return censusProbeHelper2(svars)
}
`)
	s2.propagateKeyReads()
	s2.rotGuard(t)
	flagged := false
	for _, err := range s2.guardErrs {
		if strings.Contains(err, "censusProbeCaller2") && strings.Contains(err, "not a Params map") {
			flagged = true
		}
	}
	if !flagged {
		t.Fatalf("rot guard did not flag the non-Params map argument; findings: %v", s2.guardErrs)
	}
}

// TestParamCensusAttributesSpecialisedRulesPaths pins the api-specific
// attribution: the mana path's Amount$/Produced$ reads belong to api:Mana
// alone, the unless-pay resume's UnlessCost$ to Counter/CopySpellAbility, the
// Charm mode paths' Choices$/CharmNum$ to api:Charm -- so a generic rules
// path never masks another API's genuinely unread parameter.
//
// api:Sacrifice.Amount is no longer in the "should stay unread" set: the
// replacement-damage-counter ticket's effSacrifice (effects/zone.go) reads
// Amount$ for real now -- Dralnu, Dread Lord of the Accursed's DB$
// ReplaceDamage-driven "sacrifice that many permanents" redirect, gated on
// Ctx.ReplacementAmount > 0. That is effSacrifice's OWN registered read, not
// generic machinery bleeding across APIs (the class apiSpecificRulesSA
// guards against), so the census correctly attributes it to api:Sacrifice
// wherever the key is read at all -- the five repo-deck carriers below use
// Amount$ for an unrelated, still-unimplemented shape (a literal sacrifice
// count, never a damage-replacement redirect), and their knownUnsupportedParams
// entries were retired to match (the primitive is measured as read, exactly
// like knownUnsupported retires once a primitive registers even though a
// given card's shape is narrower than full coverage).
func TestParamCensusAttributesSpecialisedRulesPaths(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	want := map[string]map[string]bool{
		"Mana":             {"Amount": true, "Produced": true},
		"Counter":          {"UnlessCost": true},
		"CopySpellAbility": {"UnlessCost": true},
		"Charm":            {"CharmNum": true, "Choices": true, "CanRepeatModes": true},
		"Sacrifice":        {"Amount": true},
	}
	for api, keys := range want {
		for key := range keys {
			if !d.api[api][key] {
				t.Errorf("d.api[%q][%q] = false -- the specialised attribution lost a real read", api, key)
			}
		}
	}
	for _, wrong := range []struct{ api, key string }{
		{"Sacrifice", "Produced"},
		{"DealDamage", "CharmNum"}, {"DealDamage", "Produced"}, {"ChangeZone", "Amount"},
	} {
		if d.api[wrong.api][wrong.key] {
			t.Errorf("d.api[%q][%q] = true -- a specialised rules path still over-suppresses this API's gap", wrong.api, wrong.key)
		}
	}
}

// TestParamCensusScopesTheMayPlayStaticFamily pins the stat-bucket family
// split (apiSpecificRulesStat): mayPlayStatic/mayPlayGrant/warpGraveyardAllowed
// read only MayPlay$ Continuous statics, so their keys must NOT sit in the
// generic Continuous union -- Mogis/Purphoros's CheckSVar$/SVarCompare$ and
// Master of Etherium's CharacteristicDefining$ were all masked by that
// misattribution (review round findings-sol1, MAJOR; Angelic Overseer's
// IsPresent$ was too, until the continuous-gate wave genuinely read it on
// the generic bucket -- rules/layers.go's continuousGateHolds). Inside the
// family the genuinely evaluated gates (Condition$ PlayerTurn, IsPresent$,
// MayPlayLimit$) must read, while the fail-closed recognitions
// (mayPlayGateRejected's mayPlayUnreadGates family plus CheckSVar$/
// MayPlayPlayer$, stringMapParams-whitelisted) must stay unread: a
// withheld-whole MayPlay static's recognition key is a RECOGNITION, never a
// consumption, and no census label misreads it as a live gap the offer path
// would honour.
func TestParamCensusMayPlayRiderFixture(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	params := map[string]string{"MayPlay": "True", "Affected": "Card", "AffectedZone": "Graveyard", "MayPlayIgnoreColor": "True", "MayPlayIgnoreType": "True"}
	if params["MayPlay"] == "" || params["MayPlayIgnoreColor"] == "" || params["MayPlayIgnoreType"] == "" {
		t.Fatal("fixture must identify the MayPlay family and distinct rider values")
	}
	c := &cards.Card{Faces: []*cards.Face{{Name: "fixture", Statics: []cards.Static{{Mode: "Continuous", Params: params}}}}}
	if statCensusMode(c.Faces[0].Statics[0].Mode, params) != "Continuous.MayPlay" {
		t.Fatal("fixture static did not map to Continuous.MayPlay")
	}
	for _, label := range cardCensusLabels(c, d, nil) {
		if label == "param:stat:Continuous.MayPlay.MayPlayIgnoreColor" || label == "param:stat:Continuous.MayPlay.MayPlayIgnoreType" {
			t.Errorf("effects/mayPlayParams reads should be attributed; got %s", label)
		}
	}
}

func TestParamCensusScopesTheMayPlayStaticFamily(t *testing.T) {
	res, d := measureParamCensus(t, nil)
	// The MayPlay family's read set: the generic Continuous union PLUS the
	// genuinely evaluated MayPlay gates. MayPlayAltManaCost$ joined with the
	// alt-cost delivery (mayPlayAltCosts genuinely offers the priced
	// alternative -- Darksteel Monolith's "pay {0}") after having been a
	// fail-closed recognition.
	for _, key := range []string{"Condition", "IsPresent", "MayPlay", "Affected", "AffectedZone", "MayPlayLimit", "MayPlayAltManaCost", "RaiseCost", "MayPlayIgnoreColor", "MayPlayIgnoreType"} {
		if !d.stat["Continuous.MayPlay"][key] {
			t.Errorf("d.stat[Continuous.MayPlay][%q] = false -- the family attribution lost a real MayPlay-gate read", key)
		}
	}
	// ... and the fail-closed recognitions must never read, on the family or
	// the generic union. CheckSVar$/SVarCompare$ are NOT in that list since
	// the statics merge: rules/statics.go's own restriction/cost gates
	// genuinely evaluate them (checkSVarHolds, fail closed), so both buckets
	// legitimately carry those reads -- and since the continuous-gate wave
	// IsPresent$ is in the same position: rules/layers.go's
	// continuousGateHolds genuinely evaluates it on every generic Continuous
	// static (Angelic Overseer's Human check, Static Orb's untapped state).
	// CharacteristicDefining$ left the list with the static-PT wave:
	// rules/layers.go's staticEffects reads it to place a Set static in the
	// CR 613.4a CDA sublayer (Tarmogoyf, Krovikan Mist now derive their
	// announced P/T), so the read is genuine on the generic bucket.
	// ValidAfterStack$ is consumed by mayPlayStatic as a derived spell filter
	// (task mayplay-validafterstack); MayPlayPlayer$ remains fail-closed.
	if !d.stat["Continuous.MayPlay"]["ValidAfterStack"] {
		t.Error(`d.stat["Continuous.MayPlay"]["ValidAfterStack"] = false -- MayPlay characteristic gate lost its read`)
	}
	for _, key := range []string{"MayPlayPlayer"} {
		for _, mode := range []string{"Continuous", "Continuous.MayPlay"} {
			if d.stat[mode][key] {
				t.Errorf("d.stat[%q][%q] = true -- the fail-closed recognition read still over-suppresses this key", mode, key)
			}
		}
	}
	// MayPlayAltManaCost$ is read on the FAMILY (mayPlayAltCosts) but must
	// stay out of the generic Continuous union: a plain Continuous static's
	// unread key must not be masked by a MayPlay-only reader.
	if d.stat["Continuous"]["MayPlayAltManaCost"] {
		t.Errorf("d.stat[Continuous][MayPlayAltManaCost] = true -- the alt-cost reader over-suppresses the generic Continuous bucket")
	}
	// ... and the family-scoped reads must be OUT of the generic union.
	// Condition$ joined the generic bucket with the statics merge: the
	// generic restriction/cost gates (restrictionGateHolds,
	// costConditionHolds) genuinely read it on every Continuous static, and
	// since the continuous-gate wave IsPresent$ reads there too
	// (continuousGateHolds) -- MayPlayLimit$ is the one read that must stay
	// family-only. CharacteristicDefining$ joined the generic bucket with
	// the static-PT wave (staticEffects' CDA sublayer placement), so the
	// former generic-bucket evidence (Master of Etherium) retired.
	for _, key := range []string{"Condition", "IsPresent", "CharacteristicDefining"} {
		if !d.stat["Continuous"][key] {
			t.Errorf("d.stat[Continuous][%q] = false -- the generic Continuous reader lost a real gate read", key)
		}
	}
	for _, key := range []string{"MayPlayLimit", "MayPlayIgnoreColor", "MayPlayIgnoreType"} {
		if d.stat["Continuous"][key] {
			t.Errorf("d.stat[Continuous][%q] = true -- the MayPlay family reader still over-suppresses the generic Continuous bucket", key)
		}
	}
	// The census-level effect on the real repo decks, both directions:
	// a plain Continuous static's unread gate is labelled, a MayPlay
	// static's genuinely evaluated gate is not, and the withheld-whole
	// MayPlay static's recognition key is labelled under the family.
	// Angelic Overseer/Static Orb/Auriok Steelshaper's IsPresent$ labels are
	// GONE since the continuous-gate wave -- the formerly-labelled trio the
	// split was first pinned with; Mogis/Purphoros's RemoveType$ (a distinct,
	// still-unread grant param) is the surviving generic-bucket evidence,
	// and Master of Etherium's CharacteristicDefining$ retired with the
	// static-PT wave's genuine CDA read.
	for card, label := range map[string]string{
		"Mogis, God of Slaughter":     "param:stat:Continuous.RemoveType",
		"Purphoros, God of the Forge": "param:stat:Continuous.RemoveType",
	} {
		found := false
		for _, l := range res.labels[card] {
			if l == label {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: expected %s in the census (labels %v)", card, label, res.labels[card])
		}
	}
	// The former IsPresent labels must STAY gone: the generic gate read
	// (continuousGateHolds) retired all three -- and so must Master of
	// Etherium's CharacteristicDefining$ label since the CDA work read it.
	for _, card := range []string{"Angelic Overseer", "Auriok Steelshaper", "Static Orb", "Master of Etherium"} {
		for _, l := range res.labels[card] {
			t.Errorf("%s: census labels %s but the IsPresent gate is genuinely evaluated now -- the shrink regressed", card, l)
		}
	}
	for card, banned := range map[string]string{
		"Gravecrawler":       "param:stat:Continuous.MayPlay.IsPresent",          // genuinely evaluated, real card test
		"Evendo Brushrazer":  "param:stat:Continuous.MayPlay.Condition",          // Condition$ PlayerTurn is read
		"Darksteel Monolith": "param:stat:Continuous.MayPlay.MayPlayAltManaCost", // genuinely priced by mayPlayAltCosts
	} {
		for _, l := range res.labels[card] {
			if l == banned {
				t.Errorf("%s: census labels %s but the gate is genuinely evaluated -- the family attribution regressed", card, l)
			}
		}
	}
}

// TestParamCensusCatchesFaceOwnedCosts pins the cost sources that are not
// Params maps: the normal printed cost plus Kicker, Surge, Flashback and
// Miracle. It drives a two-face card through cardCensusLabels, the same
// card-side walk walkRepoDeckCensus uses, so removing faceUnknownCostLabels'
// production call makes this test fail even though no current repo-deck card
// carries one of these unknown face-owned cost tokens.
func TestParamCensusCatchesFaceOwnedCosts(t *testing.T) {
	c := &cards.Card{Faces: []*cards.Face{
		{
			ManaCost: "Waterbend<X>",
			Keywords: []string{
				"Kicker:ChooseCard<1/CARDNAME>",
				"Surge:PaySurge<1>",
			},
		},
		{Keywords: []string{
			"Flashback:ExileFromGrave<1/Card>",
			"Miracle:PayMiracle<1>",
		}},
	}}
	want := []string{
		"cost:Waterbend", "cost:ChooseCard", "cost:PaySurge",
		"cost:PayMiracle",
	}
	d := &derivedReads{api: map[string]map[string]bool{}, trig: map[string]map[string]bool{}, stat: map[string]map[string]bool{}, repl: map[string]map[string]bool{}}
	if got := cardCensusLabels(c, d, nil); !sameSet(got, want) {
		t.Errorf("card-side face-owned costs = %v, want %v", got, want)
	}
}

// TestParamCensusCatchesSVarBodyGaps pins the SVar-body walk: a body named
// by an execution-bearing SVar-reference parameter -- Charm's `Choices$`
// entries (effCharm resolves and runs each) or Repeat's `RepeatSubAbility$`
// (effRepeat resolves and runs it) -- must be censused like a linked
// sub-ability. Before that walk existed the outer primitives' own params
// (Choices$, RepeatSubAbility$) were all read, the bodies were never
// attached by Link, and a repo-deck card could carry an unread parameter or
// an unmodelled cost token inside a selected/repeated body that no ratchet
// saw. The fixture runs the real derived read sets (measureParamCensus),
// and each expected label is reachable ONLY through the SVar body: remove
// the face-SVar walk from cardCensusLabels and both labels disappear.
func TestParamCensusCatchesSVarBodyGaps(t *testing.T) {
	_, d := measureParamCensus(t, nil)
	// Preconditions: the outer primitives' parameters the fixture carries are
	// genuinely read (so the labels can only come from the bodies), and the
	// body keys are genuinely unread (so the fixture measures a real gap).
	// LoseLife's UnlessCost$ WAS the fixture's unread body key until the
	// shared unless gate made every API's UnlessCost$ a read (the
	// unlessProceed dispatch reads the parameter before any primitive
	// dispatch); the body key below moved to RememberObjects$, which no
	// LoseLife reader touches. (It was TargetingPlayer$ until the shared
	// target-ask read made that parameter read for every API; the
	// SVar-body-gap fixture is only meaningful while its key stays unread.)
	// PayEnergy<X> WAS the fixture's unmodelled
	// cost token until ParseCost gained a real Energy field; the body cost
	// below moved to the fictional Waterbend<X>, which ParseCost can never
	// model.
	if !d.api["Charm"]["Choices"] || !d.api["Repeat"]["RepeatSubAbility"] {
		t.Fatalf("outer Choices$/RepeatSubAbility$ reads lost -- fixture premise broken")
	}
	if d.api["LoseLife"]["RememberObjects"] {
		t.Fatalf("api:LoseLife now reads RememberObjects$ -- re-point the fixture at a genuinely unread key")
	}
	c := &cards.Card{Faces: []*cards.Face{{
		// A modal spell whose one mode loses life for the player who targeted
		// its source (the RememberObjects$ spelling no LoseLife reader
		// touches -- TargetingPlayer$ WAS this fixture's unread body key
		// until this ticket's shared target-ask read made it read for every
		// API), and a repeat whose body carries an energy cost ParseCost
		// does not model (the Chthonian Nightmare shape, reached through
		// RepeatSubAbility$).
		Abilities: []*cards.SA{
			{Kind: "SP", API: "Charm", Params: map[string]string{"Choices": "DBMode,DBMoney", "CharmNum": "1"}},
			{Kind: "SP", API: "Repeat", Params: map[string]string{"RepeatNum": "2", "RepeatSubAbility": "DBMoney"}},
		},
		SVars: map[string]string{
			"DBMode":  "DB$ LoseLife | RememberObjects$ True | Defined$ Remembered",
			"DBMoney": "DB$ LoseLife | Cost$ Waterbend<X>",
		},
	}}}
	want := []string{"param:api:LoseLife.RememberObjects", "cost:Waterbend"}
	if got := cardCensusLabels(c, d, nil); !sameSet(got, want) {
		t.Errorf("SVar-body census = %v, want %v -- an unread key or unmodelled token inside a Choices$/RepeatSubAbility$ body is not being reported", got, want)
	}
	// The drop plumbing reaches the bodies too: pretending the LoseLife
	// RememberObjects$ read existed (it does not) must not un-report the
	// body's gap through some other path.
	if got := cardCensusLabels(c, d, map[string]map[string]bool{"api:LoseLife": {"RememberObjects": true}}); !sameSet(got, want) {
		t.Errorf("drop-simulated census = %v, want %v", got, want)
	}
}

func TestParseCostReportsUnmodelledCostTokens(t *testing.T) {
	cases := []struct {
		cost string
		want []string
	}{
		// The Chthonian Nightmare shape is now MODELLED (PayEnergy<X> is the
		// announced X bound by the payer's energy total; Return<1/CARDNAME> is
		// the source returned to its owner's hand), so nothing is degraded.
		{"PayEnergy<X> Sac<1/Creature> Return<1/CARDNAME>", nil},
		// A head ParseCost still does not model keeps reporting (the census
		// fixture's own token: Waterbend, Kor Bladewhirl's ability cost).
		{"Waterbend<X>", []string{"Waterbend"}},
		{"PayLife<5>", nil},
		// The announced PayLife<X> form is now MODELLED (the cast announces X,
		// bounded by the payer's life; the settle pays it), so nothing is
		// degraded. The malformed instance still reports the known head.
		{"PayLife<X>", nil},
		// The other cost-token-family heads, in their exact repo-deck shapes:
		// bare Exile (battlefield), the Draw bucket (previously mis-modelled
		// as a SubCounter removal), the announced SubCounter and DamageYou.
		{"1 Exile<1/CARDNAME>", nil},
		{"Draw<1/You>", nil},
		// The dynamic-amount Draw<X/Spec> form (Champion of Wits, Titan of
		// Littjara and 7 more: Cost$ Draw<X/You> with a card-level SVar:X
		// body) is now MODELLED too -- ParseCost records it as a Draw part
		// with Dyn naming the source SVar, resolved at payment, so no
		// generic mana is substituted and no cost:Draw label is reported.
		{"Draw<X/You>", nil},
		{"SubCounter<X/LOYALTY>", nil},
		{"DamageYou<4>", nil},
		// The PutCardToLibFrom<Zone> family (the printed activation costs of
		// Timestream Navigator, Leashling, Battlefield Scrounger, Ardent
		// Dustspeaker, Penance and friends): modelled for Hand, Grave and
		// Battlefield. The first field is the count, the second the library
		// position (-1 bottom / 0 top) and the third the filter spec.
		{"2 U U T PutCardToLibFromBattlefield<1/-1/CARDNAME>", nil},
		{"PutCardToLibFromGrave<3/-1/Card>", nil},
		{"PutCardToLibFromGrave<1/-1/Sorcery;Instant>", nil},
		{"PutCardToLibFromHand<1/0/Card>", nil},
		// A recognised head whose INSTANCE this build cannot place (an
		// out-of-range position) is still reported, and an unnamed zone head
		// is not modelled.
		{"PutCardToLibFromGrave<1/7/Card>", []string{"PutCardToLibFromGrave"}},
		{"PutCardToLibFromExile<1/-1/Card>", []string{"PutCardToLibFromExile"}},
		// Recognised heads whose INSTANCE is malformed or out of range: the
		// head is known, the instance is not modelled -- reported too.
		{"PayLife<99999999999999999999>", []string{"PayLife"}},
		{"Sac<99999999999999999999/Creature>", []string{"Sac"}},
		{"AddCounter<99999999999999999999/LOYALTY>", []string{"AddCounter"}},
		{"PayLife<abc>", []string{"PayLife"}},
		{"Sac</Creature>", []string{"Sac"}},
		{"2 U U Sac<1/Creature>", nil},
		// A source-anchored AddCounter<N/KIND> is modelled (Wall of Roots);
		// a chooser-anchored one is still the reported fallback.
		{"AddCounter<1/M1M1>", nil},
		{"AddCounter<1/M1M1/Creature.YouCtrl/a creature you control>", []string{"AddCounter"}},
		// The ExiledMoveToGrave family (the Eldrazi processor costs and
		// Shelob, Dread Weaver's {2}{B} ability) is now MODELLED -- cards
		// matching Spec move from exile to their owner's graveyard, with no
		// phantom generic pip and no Unknown entry. The exact corpus
		// spellings, including Forge's trailing description, plus the
		// malformed-instance report of the recognised head.
		{"2 B ExiledMoveToGrave<1/Creature.ExiledWithSource>", nil},
		{"ExiledMoveToGrave<1/Card.OppOwn/card an opponent owns>", nil},
		{"ExiledMoveToGrave<2/Card.OppOwn>", nil},
		{"ExiledMoveToGrave<99999999999999999999/Creature>", []string{"ExiledMoveToGrave"}},
		// XMin<N> (task cost-xmin1) is the announced-X LOWER BOUND, "X can't
		// be 0": modelled as Cost.XMin with no phantom generic pip and no
		// Unknown entry, for both corpus values (XMin1 and XMin4).
		{"XMin1 X", nil},
		{"XMin4", nil},
		{"", nil},
	}
	for _, tc := range cases {
		got := ParseCost(tc.cost).Unknown
		if !sameSet(orEmpty(got), orEmpty(tc.want)) {
			t.Errorf("ParseCost(%q).Unknown = %v, want %v", tc.cost, got, tc.want)
		}
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// distinctRepoDeckCards counts the distinct cards the census walked (for the
// report line).
func distinctRepoDeckCards(t *testing.T) int {
	t.Helper()
	reg := testutil.CorpusRegistry(t)
	seen := map[string]bool{}
	for _, name := range testutil.RepoDeckNames() {
		for _, c := range testutil.RepoDeck(t, reg, name) {
			seen[c.Faces[0].Name] = true
		}
	}
	return len(seen)
}
