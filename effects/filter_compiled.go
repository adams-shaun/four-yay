package effects

import (
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// The compiled filter form. matchesObjectText (the textual oracle) re-splits
// a spec on ',' / '.' / '+', trims it and re-classifies every predicate token
// through matchPositive's chain of string compares and map lookups on EVERY
// object it is asked about -- the dominant string-processing cost of a game.
// compiledSpecFor parses each distinct spec string once into an immutable
// compiledSpec whose evaluation performs exactly the oracle's sequence of
// object/context reads with the string grammar already resolved:
//
//   - the EACH split, the alternative split (filterAlternatives), the trim
//     and the base/predicate cut are done once;
//   - a base is reduced to its non-parity and a kind (the matchesBase switch);
//   - a predicate token is reduced to its leading-'!' polarity and to the
//     FIRST matchPositive branch whose purely textual condition it satisfies,
//     with that branch's static lookup (the predicates map entry, the keyword
//     entry, the controlReferent split, the wordPredicate/nonPredicate
//     classification) carried precomputed.
//
// Every branch whose applicability depends on the evaluation context (the
// ExtraKeywords binding, an unbound referent, the chosen-colour source, a
// numeric right-hand side resolved through SVars) is still decided at
// evaluation time exactly as the oracle decides it, so the unknown/fail-closed
// contract is unchanged. Tokens handled by matchPositive's leading
// special-case block (context referents such as IsRemembered or
// TriggeredCard, the greatest/lowest families) simply call matchPositive with
// the token text. TestCompiledFilterMatchesTextualOracle holds the two forms
// equal over the repo decks' filter specs.
//
// compiledSpec values are immutable after construction and shared by every
// game in the process (botbench plays games on parallel goroutines); they hold
// no game, object or event state.
type compiledSpec struct {
	// each is non-nil for Forge's EACH A & B form (eachAlternatives): the
	// ordinary matcher ORs the compiled sub-specs instead of alts.
	each []*compiledSpec
	// alts is filterAlternatives(spec) regardless of EACH: the zone-aware
	// matcher (matchesZoneSpecCtx) never took the EACH split.
	alts []compiledAlt
}

type compiledBaseKind uint8

const (
	cbType compiledBaseKind = iota // hasTypeCtx(o, typ, sc)
	cbAny
	cbCard
	cbPermanent
	cbAffinity
	cbPermanentCard
	cbSpell // Spell (including derived AsStack) and SpellAbility (actual stack only)
)

type compiledAlt struct {
	// base is the alternative's raw base text: the CARDNAME test and the
	// sameName context referent (sameNameContextReferent) read it.
	base     string
	cardname bool
	// baseNeg is the parity of the leading "non" prefixes matchesBase
	// strips recursively; kind/typ classify what remains.
	baseNeg bool
	kind    compiledBaseKind
	typ     string
	// typSub is changelingType(typ), precomputed for hasTypeCtxSub.
	typSub bool
	// contextualSameName is sameNameContextBase(base, rest).
	contextualSameName bool
	// spellTargeting marks Forge's base-qualified Spell.IsTargeting form (and
	// the SpellAbility spelling), optionally '!'-negated: the alternative is
	// ONE unit whose whole argument (not a '+' token) is the target spec.
	// spellTargetingOK is false for an argument this grammar cannot answer
	// whole, which fails the whole alternative closed (never a partial
	// evaluation). spellTargetingSpec is the normalised target spec.
	spellTargeting     bool
	spellTargetingOK   bool
	spellTargetingNeg  bool
	spellTargetingSpec string
	preds              []compiledPred
}

type compiledStage uint8

const (
	// csUnknown: matchPredicate's ok is false for every object/context.
	csUnknown compiledStage = iota
	// csSpecial: one of matchPositive's leading special-case tokens; the
	// oracle function is called with the token text.
	csSpecial
	csControl
	csType
	// csChain: the tail of matchPositive after typePredicate -- the keyword
	// binding, NamedCard, the predicates map, numericPred, non<X> and the
	// wordPredicate classifier, each flag below precomputed.
	csChain
)

type compiledPred struct {
	// raw is the token as written (the contextual sameName "Permanent"
	// auxiliary compares it); pos is raw without its '!'.
	raw string
	pos string
	neg bool

	stage compiledStage

	ctlOp, ctlRef string

	hasKP bool
	kp    keywordPredicate
	named bool // p == "NamedCard" (namePredicate)
	fn    predFn
	num   bool // numericPred recognises the shape (its ok is textual)
	nonOK bool
	nonK  wordKind
	nonV  string
	wordK wordKind
	wordV string
}

// specialPositiveToken reports whether p is handled by matchPositive's leading
// special-case block (everything before the controlReferent test). It must
// list exactly those conditions; a token it misses would be dispatched to a
// later branch the oracle never reaches for it.
func specialPositiveToken(p string) bool {
	switch p {
	case "token$DifferentCardNames",
		"ChosenCard", "ChosenCardStrict", "nonChosenCard",
		"RememberedPlayerCtrl",
		"CanBeTargetedByTriggeredSpellAbility",
		"TriggeredNewCard", "TriggeredCard",
		"blockingTriggeredAttacker",
		"EffectSource",
		"IsGoaded",
		"IsRemembered", "IsTriggerRemembered":
		return true
	}
	// Forge's base-qualified `Spell.IsTargeting <target-spec>` form is handled
	// at the alternative level (compileSpec/compiledMatch), never as a bare
	// predicate token: its whole argument is a target spec, so a token-level
	// classifier could only see its truncated '+' head. A compiled predicate
	// carrying that text therefore classifies as csUnknown and fails closed.
	return strings.HasPrefix(p, "ChosenMode") && len(p) > len("ChosenMode") ||
		strings.HasPrefix(p, "greatestPower") ||
		strings.HasPrefix(p, "greatestCMC_") ||
		strings.HasPrefix(p, "lowestCMC") ||
		hasAbilityToken(p)
}

// typePredicateToken lists typePredicate's switch cases.
func typePredicateToken(p string) bool {
	switch p {
	case "Legendary", "Basic", "Snow", "nonLand", "nonCreature", "nonBasic",
		"ChosenType", "IsNotChosenType", "ChosenCtrl":
		return true
	}
	return false
}

func compilePred(raw string) compiledPred {
	c := compiledPred{raw: raw, pos: raw}
	if x, has := strings.CutPrefix(raw, "!"); has {
		if x == "" {
			return c // csUnknown
		}
		c.pos, c.neg = x, true
	}
	p := c.pos
	if specialPositiveToken(p) {
		c.stage = csSpecial
		return c
	}
	if op, ref, ok := controlReferent(p); ok {
		c.stage, c.ctlOp, c.ctlRef = csControl, op, ref
		return c
	}
	if typePredicateToken(p) {
		c.stage = csType
		return c
	}
	c.kp, c.hasKP = keywordPredicateFor(p)
	c.named = p == "NamedCard"
	c.fn = predicates[p]
	_, c.num = numericPred(p, nil, &state.Object{}, SpecContext{})
	c.nonK, c.nonV, c.nonOK = nonPredicate(p)
	c.wordK, c.wordV = wordPredicate(p)
	if c.hasKP || c.named || c.fn != nil || c.num || c.nonOK || c.wordK != wordUnknown {
		c.stage = csChain
	}
	return c
}

// compiledPositive is matchPositive(g, c.pos, o, sc) with the textual dispatch
// precomputed. The compiled evaluators are plain functions, not methods, so
// rules' paramcensus call graph (which follows package-local function calls)
// still reaches matchPositive and the Params readers beneath it.
func compiledPositive(c *compiledPred, g *state.Game, o *state.Object, sc *SpecContext) (result, ok bool) {
	switch c.stage {
	case csSpecial:
		return matchPositive(g, c.pos, o, *sc)
	case csControl:
		return matchControlReferent(g, o, *sc, c.ctlOp, c.ctlRef)
	case csType:
		return typePredicate(c.pos, g, o, *sc)
	case csChain:
	default:
		return false, false
	}
	if c.hasKP {
		has := false
		if sc.ExtraKeywords == nil {
			has = objectHasKeyword(o, c.kp.keyword)
		} else {
			for _, x := range sc.ExtraKeywords {
				if strings.EqualFold(cards.KeywordHead(x), c.kp.keyword) {
					has = true
					break
				}
			}
		}
		if c.kp.negated {
			has = !has
		}
		return has, true
	}
	if c.named {
		return namePredicate(c.pos, g, o, *sc)
	}
	if c.fn != nil {
		return c.fn(g, o, sc.You, sc.Source), true
	}
	if c.num {
		return numericPred(c.pos, g, o, *sc)
	}
	if c.nonOK {
		return !wordMatches(c.nonK, c.nonV, g, o, *sc), true
	}
	if kind := c.wordK; kind != wordUnknown {
		if kind == wordCastProvenance {
			return false, false
		}
		if kind == wordChosenColor {
			src := g.Obj(sc.Source)
			if src == nil || colourLetter(src.ChosenColor) == 0 {
				return false, false
			}
		}
		if !contextPredicateBound(g, kind, c.wordV, *sc) {
			return false, false
		}
		return wordMatches(kind, c.wordV, g, o, *sc), true
	}
	return false, false
}

// compiledPredEval is matchPredicate(g, c.raw, o, sc).
func compiledPredEval(c *compiledPred, g *state.Game, o *state.Object, sc *SpecContext) (result, ok bool) {
	r, rok := compiledPositive(c, g, o, sc)
	if !rok {
		return false, false
	}
	if c.neg {
		return !r, true
	}
	return r, true
}

func compileSpec(spec string) *compiledSpec {
	cs := &compiledSpec{}
	if subs, ok := eachAlternatives(spec); ok {
		cs.each = make([]*compiledSpec, len(subs))
		for i, sub := range subs {
			cs.each[i] = compileSpec(sub)
		}
	}
	for alt := range filterAlternatives(spec) {
		alt = strings.TrimSpace(alt)
		if alt == "" {
			continue
		}
		base, rest, _ := strings.Cut(alt, ".")
		a := compiledAlt{
			base:               base,
			cardname:           base == "CARDNAME",
			contextualSameName: sameNameContextBase(base, rest),
		}
		b := base
		for {
			neg, has := strings.CutPrefix(b, "non")
			if !has {
				break
			}
			a.baseNeg = !a.baseNeg
			b = neg
		}
		switch b {
		case "Any":
			a.kind = cbAny
		case "Card":
			a.kind = cbCard
		case "Permanent":
			a.kind = cbPermanent
		case "Affinity":
			a.kind = cbAffinity
		case "PermanentCard":
			a.kind = cbPermanentCard
		case "Spell", "SpellAbility":
			a.kind = cbSpell
		default:
			a.kind, a.typ, a.typSub = cbType, b, changelingType(b)
		}
		// Forge's base-qualified Spell.IsTargeting form (and the SpellAbility
		// spelling) is ONE alternative: the whole rest after `IsTargeting `
		// is the target spec, so it is never split into candidate predicates.
		// An argument the shared grammar cannot answer whole fails the whole
		// alternative closed (spellTargetingOK stays false), which is the
		// compiled twin of matchesObjectText's early continue.
		if _, stNeg, shape := spellIsTargetingShape(base, rest); shape {
			a.spellTargeting = true
			a.spellTargetingNeg = stNeg
			if arg, _, ok := spellIsTargetingAlt(base, rest); ok && spellIsTargetingArgRecognised(arg) {
				spec, _ := spellIsTargetingInner(arg)
				a.spellTargetingSpec, a.spellTargetingOK = spec, true
			}
		} else {
			for p := range strings.SplitSeq(rest, "+") {
				if p == "" {
					continue
				}
				a.preds = append(a.preds, compilePred(p))
			}
		}
		cs.alts = append(cs.alts, a)
	}
	return cs
}

// compiledBaseMatch is matchesBase(g, a.base, o, sc) (or, with zone set,
// matchesBaseInZone) for a non-CARDNAME alternative.
func compiledBaseMatch(a *compiledAlt, o *state.Object, sc *SpecContext, zone state.Zone, inZone bool) bool {
	var m bool
	switch a.kind {
	case cbAny:
		m = hasTypeCtxSub(o, "Creature", subCreature, sc) || hasTypeCtxSub(o, "Planeswalker", subPlaneswalker, sc) ||
			hasTypeCtxSub(o, "Battle", subBattle, sc)
	case cbCard:
		m = true
	case cbPermanent:
		if inZone && zone != state.ZBattlefield {
			m = hasTypeCtxSub(o, "Artifact", subArtifact, sc) || hasTypeCtxSub(o, "Creature", subCreature, sc) ||
				hasTypeCtxSub(o, "Enchantment", subEnchantment, sc) || hasTypeCtxSub(o, "Land", subLand, sc) ||
				hasTypeCtxSub(o, "Planeswalker", subPlaneswalker, sc) || hasTypeCtxSub(o, "Battle", subBattle, sc)
		} else {
			m = o.Zone == state.ZBattlefield
		}
	case cbAffinity:
		if sc.ExtraKeywords != nil {
			for _, k := range sc.ExtraKeywords {
				if strings.EqualFold(cards.KeywordHead(k), "Affinity") {
					m = true
					break
				}
			}
		} else {
			m = o.Face() != nil && o.Face().HasKeyword("Affinity")
		}
	case cbPermanentCard:
		m = o.Face() != nil && o.Face().IsPermanent()
	case cbSpell:
		m = o.Zone == state.ZStack || (a.base == "Spell" && sc.AsStack)
	default:
		m = hasTypeCtxSub(o, a.typ, a.typSub, sc)
	}
	if a.baseNeg {
		return !m
	}
	return m
}

// changelingType of the fixed base words matchBase tests.
var (
	subCreature     = changelingType("Creature")
	subPlaneswalker = changelingType("Planeswalker")
	subBattle       = changelingType("Battle")
	subArtifact     = changelingType("Artifact")
	subEnchantment  = changelingType("Enchantment")
	subLand         = changelingType("Land")
)

// compiledMatch is matchesObjectText(g, spec, o, sc) for the spec cs was compiled
// from.
func compiledMatch(cs *compiledSpec, g *state.Game, o *state.Object, sc *SpecContext) bool {
	if o == nil {
		return false
	}
	if o.IsCopy && o.Zone != state.ZStack && o.Zone != state.ZBattlefield {
		return false
	}
	if cs.each != nil {
		for _, sub := range cs.each {
			if compiledMatch(sub, g, o, sc) {
				return true
			}
		}
		return false
	}
	for i := range cs.alts {
		a := &cs.alts[i]
		// asc is the predicate context: sc itself, or a copy re-sourced to
		// the sameName referent. Pointers, not copies: SpecContext is large
		// and this loop runs per object per match.
		asc := sc
		if a.contextualSameName {
			ref, bound := sameNameContextReferent(g, a.base, *sc)
			if !bound {
				continue
			}
			rs := *sc
			rs.Source = ref
			asc = &rs
			// base = "Card": always matches.
		} else if a.cardname {
			if sc.Source == 0 || o.ID != sc.Source {
				continue
			}
		} else if !compiledBaseMatch(a, o, sc, 0, false) {
			continue
		}
		if a.spellTargeting {
			if a.spellTargetingOK {
				met := spellIsTargetingMatchesPtr(g, a.spellTargetingSpec, o, asc)
				if a.spellTargetingNeg {
					met = !met
				}
				if met {
					return true
				}
			}
			continue
		}
		all := true
		for j := range a.preds {
			p := &a.preds[j]
			if a.contextualSameName && p.raw == "Permanent" {
				if !isPermanentCard(o) {
					all = false
					break
				}
				continue
			}
			res, ok := compiledPredEval(p, g, o, asc)
			if !ok || !res {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// compiledMatchZone is the alternative loop of matchesZoneSpecCtx for a zone other
// than the battlefield (the caller has already applied the IsCopy rejection).
func compiledMatchZone(cs *compiledSpec, g *state.Game, o *state.Object, sc *SpecContext, zone state.Zone) bool {
	for i := range cs.alts {
		a := &cs.alts[i]
		if a.cardname {
			if sc.Source == 0 || o.ID != sc.Source {
				continue
			}
		} else if !compiledBaseMatch(a, o, sc, zone, true) {
			continue
		}
		if a.spellTargeting {
			if a.spellTargetingOK {
				met := spellIsTargetingMatchesPtr(g, a.spellTargetingSpec, o, sc)
				if a.spellTargetingNeg {
					met = !met
				}
				if met {
					return true
				}
			}
			continue
		}
		all := true
		for j := range a.preds {
			res, ok := compiledPredEval(&a.preds[j], g, o, sc)
			if !ok || !res {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// compiledSpecCacheMax bounds the process-wide cache. Specs come from card
// scripts (a few thousand distinct strings in a match); a caller that builds
// spec text dynamically past the bound is still answered, by a compile that is
// simply not retained.
const compiledSpecCacheMax = 1 << 16

// specCache is a copy-on-write map: readers load an immutable snapshot with
// one atomic read and no lock; a miss takes the mutex, compiles into the
// dirty map and republishes the snapshot once enough misses have accumulated
// to amortise the copy (the sync.Map promotion rule, without its interface
// boxing of the string key).
type specCache struct {
	ro     atomic.Pointer[map[string]*compiledSpec]
	mu     sync.Mutex
	dirty  map[string]*compiledSpec
	misses int
}

var compiledSpecs specCache

// specFront is a direct-mapped front for the map snapshot, indexed by the
// spec string's data pointer and length. Spec text comes overwhelmingly from
// the immutable card IR, so the same call site passes the same backing array
// every time and a hit is one pointer-equal string compare instead of a hash
// of the whole spec. The entry holds the string itself, so the compare is
// exact (a different string that lands in the slot merely misses) and the
// backing array cannot be reused while the entry lives. Entries are
// immutable and published atomically, so concurrent games race only to
// replace a slot, never to read a torn one.
type specFrontEntry struct {
	spec string
	cs   *compiledSpec
}

const specFrontBits = 14

var specFront [1 << specFrontBits]atomic.Pointer[specFrontEntry]

func specFrontSlot(spec string) uint {
	p := uintptr(unsafe.Pointer(unsafe.StringData(spec)))
	h := uint64(p>>3) ^ uint64(len(spec))*0x9e3779b97f4a7c15
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uint(h) & (1<<specFrontBits - 1)
}

func compiledSpecFor(spec string) *compiledSpec {
	slot := &specFront[specFrontSlot(spec)]
	if e := slot.Load(); e != nil && e.spec == spec {
		return e.cs
	}
	var cs *compiledSpec
	if m := compiledSpecs.ro.Load(); m != nil {
		cs = (*m)[spec]
	}
	if cs == nil {
		cs = compiledSpecs.slow(spec)
	}
	slot.Store(&specFrontEntry{spec: spec, cs: cs})
	return cs
}

func (c *specCache) slow(spec string) *compiledSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dirty == nil {
		c.dirty = map[string]*compiledSpec{}
	}
	cs, ok := c.dirty[spec]
	if !ok {
		cs = compileSpec(spec)
		if len(c.dirty) >= compiledSpecCacheMax {
			return cs
		}
		c.dirty[spec] = cs
	}
	c.misses++
	ro := c.ro.Load()
	if ro == nil || c.misses >= len(*ro) {
		snap := make(map[string]*compiledSpec, len(c.dirty))
		for k, v := range c.dirty {
			snap[k] = v
		}
		c.ro.Store(&snap)
		c.misses = 0
	}
	return cs
}
