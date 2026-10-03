package rules

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// derivedScalar returns only an object's derived power and toughness — the
// subset of Derived that combat, legal, cast, trigger and bot predicates read
// constantly (and, through the Chars interface, every projected view). It
// never builds the keyword/type slices Derived carries, and its effect list
// comes from active()'s cached, buffer-reused build. Its P/T is identical to
// what the full Derived computes because it first derives layer-4 types and
// hands them to every filter used by a layer-7 effect. A layer-6 KEYWORD
// grant can affect P/T applicability (an `Affected$ ...+with<Keyword>`
// pump like Windstorm Drake's), so when any layer-7 effect's Affected$
// spec actually reads the keyword list the walk delegates to derivedWith --
// the one place that builds the finished layer-6 list exactly -- instead
// of duplicating the keyword-evolution logic. The probe is a cheap
// strings.Contains short-circuit per layer-7 effect, so boards without a
// keyword-gated pump keep the old no-keyword-slice cost; layer-4 type
// changes are handled by typeCharacteristics below regardless.

func (e *Engine) derivedScalar(id state.ObjID) (power, toughness int32) {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, 0
	}
	f := o.Face()
	active := e.active()
	if p, t, ok := e.printedPT(o, f, active); ok {
		return p, t
	}
	for i := range active {
		if ce := &active[i]; ce.Layer == LPT && effects.SpecReadsKeywords(ce.Affects) {
			d := e.derivedWith(id, 0)
			return d.Power, d.Toughness
		}
	}
	p, t, _, _ := e.derivedScalarFrom(id, o, f, active, nil, nil, false)
	return p, t
}

// derivedScalarFrom is the layer-7 P/T walk proper. kw is the object's
// FINISHED layer-6 keyword list (printed, intrinsic, marker-counter, status
// and every applied layer-6 grant) bound through matchesWithChars the way
// the layer-6 walk binds its own keywords-so-far list -- a nil kw means no
// caller needed the list, which matchesWithChars reads as the printed-face
// fallback exactly as before the binding existed.
//
// It returns the current P/T and the BASE P/T. basePower/baseToughness track
// the value through layer 7b (CR 613.4): they are initialised from the same
// printed/CDA/face-down basis the current walk starts from and are advanced by
// every SubCDA/SubSet set but by NO SubModify modify and by NO 7d counter --
// so a 7c pump or a +1/+1 counter moves power/toughness while leaving the base
// pair where it was. That is the value the base filter predicates read.
//
// types/haveTypes: a caller that already holds typeCharacteristics(id, 0)'s
// exact result (derivedCompute on a live-zone derivation) passes it with
// haveTypes set, so the layer-4 walk is not run a second time; otherwise the
// walk computes it.
func (e *Engine) derivedScalarFrom(id state.ObjID, o *state.Object, f *cards.Face, active []ContinuousEffect, kw []string, types []string, haveTypes bool) (power, toughness, basePower, baseToughness int32) {
	frameIndex := len(e.derivedPTFrames)
	e.derivedPTFrames = append(e.derivedPTFrames, derivedPTSnapshot{id: id})
	defer func() { e.derivedPTFrames = e.derivedPTFrames[:frameIndex] }()
	// Layer 7d's contribution is fixed for the whole walk (counters do not
	// change mid-derivation), so sum every P/T counter KIND once here -- the
	// frame snapshot and the 7d tail below then add the same pair, and no
	// step of the walk can disagree about what a counter does to P/T. All
	// P/T counter kinds (P1P1, M0M1, P2P0, ...) are honoured through the one
	// state.CounterPTDelta parser; before this only P1P1 and M1M1 were.
	var counterDPower, counterDToughness int32
	if o != nil {
		counterDPower, counterDToughness = o.CounterPTTotals()
	}
	setFrame := func() {
		preCounterPower, preCounterToughness := power, toughness
		currentPower := power + counterDPower
		currentToughness := toughness + counterDToughness
		e.derivedPTFrames[frameIndex] = derivedPTSnapshot{
			id:                  id,
			power:               currentPower,
			toughness:           currentToughness,
			basePower:           basePower,
			baseToughness:       baseToughness,
			preCounterPower:     preCounterPower,
			preCounterToughness: preCounterToughness,
		}
	}
	if o != nil && o.FaceDown && o.Zone == state.ZBattlefield {
		// CR 708.5's base: a face-down battlefield permanent is a 2/2
		// creature; its printed P/T and any printed characteristic-defining
		// ability do not exist while it is face down. A FaceDownSetType$ that
		// does not include Creature derives 0/0 (Yedora's Forest land), and a
		// FaceDownPower$/FaceDownToughness$ pair overrides the 2/2 default
		// (Magar's 3/3). Layer-7 effects on top still apply in the walk below.
		power, toughness = 2, 2
		if !o.EffectiveIsCreature() {
			power, toughness = 0, 0
		}
		if o.FaceDownHasPT {
			power, toughness = o.FaceDownPower, o.FaceDownToughness
		}
	} else {
		power, toughness = int32(f.Power()), int32(f.Toughness())
		// Layer 7a (CR 613.4a): the object's own characteristic-defining ability
		// (CharacteristicDefining$ True) sets the base P/T that every later
		// layer applies on top of, in EVERY zone (CR 604.3/208.2 -- Master of
		// Etherium is its artifact count in hand and graveyard too, which the
		// battlefield-only static scan cannot express). Applied before the
		// effect walk below, so a layer-7b set still overrides it and a 7c
		// modify still stacks on it. staticEffects withholds the resolvable CDAs
		// from its emission exactly so this read is not applied twice.
		if p, tp, hp, ht := e.cdaSetPT(o); hp || ht {
			if hp {
				power = p
			}
			if ht {
				toughness = tp
			}
		}
	}
	// The base pair starts at the same 7a basis and is advanced only by a 7b
	// set below.
	basePower, baseToughness = power, toughness
	setFrame()
	// typeCharacteristics is 837910f4's layer-4-aware type derivation; the
	// active list comes in as a parameter (230574a2's plumbing) because
	// active() is a cached, idempotent read — same slice, no recomputation.
	if !haveTypes {
		types = e.typeCharacteristics(id, 0)
	}
	for i := range active {
		ce := &active[i]
		if ce.Layer != LPT {
			continue
		}
		// kw is the FINISHED layer-6 keyword list bound by the caller (see
		// derivedScalarFrom): a layer-7 pump gated on a granted keyword
		// (`Affected$ ...+withFlying`) must see the grant CR 613 ordered
		// below it. matchesWithChars reads a nil list as the printed-face
		// fallback, so a caller that did not build one is unchanged.
		setFrame()
		if !e.matchesWithCharsPT(ce, id, types, kw, 0, power, toughness, basePower, baseToughness, true) {
			continue
		}
		switch ce.Sub {
		case SubCDA, SubSet:
			if ce.HasSet {
				if ce.StaticSet {
					// A static can set just power or just toughness. Its omitted
					// parameter must leave the printed/earlier-layer value alone,
					// rather than treating the empty expression as numeric zero.
					if ce.SetPowerPresent {
						power = ce.SetPower
						if ce.SetPowerExpr != "" {
							power = e.staticAmount(ce, ce.SetPowerExpr)
						}
					}
					if ce.SetToughnessPresent {
						toughness = ce.SetToughness
						if ce.SetToughnessExpr != "" {
							toughness = e.staticAmount(ce, ce.SetToughnessExpr)
						}
					}
				} else {
					// Effects created through the original numeric API (Animate
					// and direct ContinuousEffect callers) predate per-component
					// presence flags and deliberately retain their paired setter
					// semantics.
					power, toughness = ce.SetPower, ce.SetToughness
				}
			}
			// A 7b set moves the BASE too (CR 613.4: a set is part of the base,
			// unlike a 7c modify). Andrios' SetPower$ 16 / SetToughness$ 9 on a
			// base-4/3 creature must read base 16/9 while a 7c pump on top still
			// reads base 16/9.
			basePower, baseToughness = power, toughness
		case SubModify:
			// AffectedX names Forge's per-affected-object P/T convention: its
			// count reads the recipient (Knight of New Alara). Ordinary named
			// expressions retain the static's grantor as their source (Mace of
			// the Valiant counts charge counters on the Mace, not its bearer).
			addPower, addToughness := ce.AddPower, ce.AddToughness
			if ce.AddPowerExpr != "" {
				anchor := ce.Source
				if ce.AddPowerAffected {
					anchor = id
				}
				addPower = e.staticAmountOn(ce, ce.AddPowerExpr, anchor)
			}
			if ce.AddToughnessExpr != "" {
				anchor := ce.Source
				if ce.AddToughnessAffected {
					anchor = id
				}
				addToughness = e.staticAmountOn(ce, ce.AddToughnessExpr, anchor)
			}
			if ce.DoublePower {
				addPower = power
			}
			if ce.DoubleToughness {
				addToughness = toughness
			}
			power = addPT(power, addPower)
			toughness = addPT(toughness, addToughness)
		}
	}
	// 7d: counters apply after every other layer-7 effect (CR 613.4). Every
	// P/T counter kind contributes its CR 122.1a delta, summed once above.
	power += counterDPower
	toughness += counterDToughness
	return power, toughness, basePower, baseToughness
}

// Derived computes an object's current characteristics: printed values from
// its face, then every applicable continuous effect in layer order, then
// layer 7d counters last. A malformed or missing object degrades to the
// zero Derived rather than panicking — layer inputs ultimately come from
// parsed card text, and a nonexistent ObjID or an ability/token object with
// no Face() must never crash the match goroutine.
//
// The Keywords and Types slices alias the Engine's scratch buffers
// (derivedKW / derivedTypes, engine.go) and are reused across calls: after
// the first call's buffers grow to size they are rewritten, never
// reallocated, so repeated Derived builds are allocation-free. That is only
// sound because every caller treats the returned slices as read-only and
// does not retain them past building its own view — view.Project and
// botpolicy both copy (append([]string(nil), ...)) synchronously, and the
// loops in HasKeyword and protectedFrom only range. Sharing would be wrong
// if a caller held one Derived's slices while calling Derived again (the
// next call would rewrite the shared buffers), so the discipline is
// load-bearing; derivedDepth guards re-entry the way active()'s activeDepth
// guards its cache (a nested Derived mid-build gets private owned buffers
// instead of clobbering the outer build's).
//
// Inside a legal-actions walk (legalActionsPriced) a top-level Derived is
// memoized per object for that walk only (rules/derivedmemo.go); a memo hit's
// slices are owned by the memo entry rather than the scratch, and the same
// read-only, do-not-retain discipline applies to them.
func (e *Engine) Derived(id state.ObjID) Derived {
	return e.derivedWith(id, 0)
}

// inProgressDerivedPT returns only an active layer-7 frame; unlike
// FilterDerivedPT it never starts a fresh derivation when no frame exists.
func (e *Engine) inProgressDerivedPT(id state.ObjID) (derivedPTSnapshot, bool) {
	for i := len(e.derivedPTFrames) - 1; i >= 0; i-- {
		if frame := e.derivedPTFrames[i]; frame.id == id && id != 0 {
			return frame, true
		}
	}
	return derivedPTSnapshot{}, false
}

// InProgressDerivedPT exposes the current layer-7 value to effects-side P/T
// references. A reference made by the static currently deriving this object
// must observe the value before that effect, rather than recursively deriving
// the same object. It intentionally has no fallback derivation, and it reads
// the PRE-COUNTER pair: the running value excludes layer 7d counters (CR
// 613.4 orders counters after every 7c modify), so a 7c amount sized from
// this object's P/T does not depend on its own +1/+1 counters.
func (e *Engine) InProgressDerivedPT(id state.ObjID) (power, toughness int32, ok bool) {
	frame, ok := e.inProgressDerivedPT(id)
	if !ok {
		return 0, 0, false
	}
	return frame.preCounterPower, frame.preCounterToughness, true
}

// FilterDerivedPT exposes a value snapshot for effects-side zone counts. The
// effects package cannot depend on rules, so its Count$Valid fold discovers
// this bridge through an optional interface and binds the values into the
// candidate's SpecContext.
func (e *Engine) FilterDerivedPT(id state.ObjID) (power, toughness, basePower, baseToughness int32, ok bool) {
	if frame, found := e.inProgressDerivedPT(id); found {
		return frame.power, frame.toughness, frame.basePower, frame.baseToughness, true
	}
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return 0, 0, 0, 0, false
	}
	d := e.Derived(id)
	return d.Power, d.Toughness, d.BasePower, d.BaseToughness, true
}

// Characteristics returns the three derived facts botpolicy projects in one
// pass. Keywords aliases Engine scratch storage exactly as Derived does; a
// caller that keeps it across another characteristics query must copy it.
func (e *Engine) Characteristics(id state.ObjID) (power, toughness int32, keywords []string) {
	if p, t, kw, ok := e.printedCharacteristics(id, true); ok {
		return p, t, kw
	}
	// Derived(id), read in place: the memo entry by pointer when derivedWith
	// would serve the memo, else one uncached build.
	if e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		if d := e.derivedMemoRef(id, 0); d != nil {
			return d.Power, d.Toughness, d.Keywords
		}
	}
	d := e.derivedCompute(id, 0)
	return d.Power, d.Toughness, d.Keywords
}

// derivedWith is Derived with an optional ZONE OVERRIDE for the AffectedZone$
// gate: atStack != 0 evaluates the grants against that zone instead of the
// object's live one. The convoke announcement (CR 601.2b) happens while the
// announced spell is still in hand -- the engine pushes it to the stack only
// later in its own cast flow -- so convokeCost/hasCastConvoke evaluate an
// AffectedZone$ Stack grant against ZStack via this override; everything
// else reads the live zone.
func (e *Engine) derivedWith(id state.ObjID, atStack state.Zone) Derived {
	if (atStack == 0 || atStack == state.ZStack) && e.derivedMemoDepth > 0 && e.derivedMemoUsable() {
		return e.derivedMemoizedAt(id, atStack)
	}
	return e.derivedCompute(id, atStack)
}

// derivedCompute is derivedWith's uncached build: the full layer walk, its
// Keywords/Types aliasing the derivedKW/derivedTypes scratch as documented on
// Derived. derivedmemo.go's walk-scoped memo calls it on a miss (and on every
// hit in verify mode) and copies the result into owned storage.
func (e *Engine) derivedCompute(id state.ObjID, atStack state.Zone) Derived {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return Derived{}
	}
	f := o.Face()
	// CR 708.5: while a battlefield object is face down its printed face
	// does not exist -- the derived basis is a vanilla 2/2 Creature face
	// (derivedScalarFrom pins the 2/2 base; this synthetic face carries no
	// keywords or printed types, and the colour basis below is overridden
	// to none). Layer effects from OTHER permanents still apply on top (an
	// Anthem pumps a manifested 2/2 to 3/3); the printed-face scans never
	// reach here because faceDownPrintedHides gates them all off (CR 708.8).
	faceDown := o.FaceDown && o.Zone == state.ZBattlefield
	if faceDown {
		f = faceDownBasis
	}
	active := e.active()
	zone := o.Zone
	if atStack != 0 {
		zone = atStack
	}
	e.derivedDepth++
	kw := e.derivedKW
	ty := e.derivedTypes
	if e.derivedDepth > 1 {
		// Re-entrant (a nested Derived mid-build): own private buffers rather
		// than overwrite the outer call's backing arrays mid-range. Same guard
		// Task A2 uses for forEachObject and this file uses for active(). (This
		// path is effectively unreachable — MatchesSpecFrom reads faces, never
		// calls Derived — but it keeps the buffer discipline airtight.)
		kw = nil
		ty = nil
	}
	kw = derivedBaseKeywords(kw, o, f, faceDown)
	// Layer 4 runs first through typeCharacteristics (see above), so every
	// later effect's Affected$ filter — and every layer-4 effect's own —
	// sees the derived type list, not the printed face.
	tyRaw := e.typeCharacteristics(id, atStack)
	ty = append(ty[:0], tyRaw...)
	// A faced object's keyword and type lists are always BOUND, even when
	// empty, from here through the layer walk to the returned Derived: a nil
	// ExtraKeywords/ExtraTypes is effects.SpecContext's "unbound, read the
	// printed face". A nil here came only from a scratch buffer that had never
	// grown (a fresh engine, a replay or a Clone before its first keyworded
	// derive), so the same face-down 2/2 matched its printed face's keywords
	// in one engine and not in another. []string{} does not allocate.
	if kw == nil {
		kw = []string{}
	}
	if ty == nil {
		ty = []string{}
	}
	// Layer 5's base is the face's colour set (the mana cost, an explicit
	// Colors: line, Devoid-applied). The letters compose in a fixed [5]bool so
	// the layer walk below never touches a map.
	// ColorMaskOf is ColorsOf's compact bitmask (230574a2); the match keeps
	// 837910f4's type-aware wrapper — a bare SpecContext carries no
	// ExtraTypes, so MatchesSpecCtx here would regress to printed types only.
	name := f.Name
	text := f.Oracle
	if faceDown {
		// CR 708.5: a face-down permanent's printed rules text does not exist,
		// the same way its printed types and colours do not.
		text = ""
	}
	col := effects.ColorMaskOf(o)
	if faceDown {
		col = 0 // CR 708.5: a face-down permanent has no colours
	}
	// CR 613.6: within layer 6 the walk applies keyword-gated effects after
	// the grant they depend on, not in raw timestamp order (see
	// abilityDependencyOrder). Layers 3/5 keep timestamp order: a SetName or
	// colour change never gates another layer's match on this corpus, and
	// layer 4 settled above.
	seq := e.abilityDependencyOrder(active, id, ty, kw, atStack)
	var cantHaveKeywords [][]string
	for i := range seq {
		ce := &seq[i]
		// Only layers 3, 5 and 6 (and a CantHaveKeywords$ prohibition, which
		// any layer's effect may carry) act in this walk: layer 4 settled in
		// typeCharacteristics above and layer 7 is derivedScalarFrom's walk
		// below. Matching an effect this walk would then ignore is pure cost
		// (the match has no side effect), so skip it before the match.
		if len(ce.CantHaveKeywords) == 0 && ce.Layer != LText && ce.Layer != LColor && ce.Layer != LAbilities {
			continue
		}
		// kw is the walk's keywords-so-far list for THIS object (printed
		// keywords, IntrinsicKeywords, marker-counter grants and every
		// layer-6 grant applied so far), bound exactly as ty is: an
		// `Affected$ ...+with<Keyword>` lord must see a keyword an earlier
		// effect granted. At the walk's end the FINISHED list is what
		// derivedScalarFrom's layer-7 walk binds -- CR 613 orders layer 6
		// strictly before layer 7, so the P/T applicability gate reads the
		// completed grant stream (Windstorm Drake over Levitation).
		if !e.matchesWithChars(ce, id, ty, kw, atStack) {
			continue
		}
		// An AffectedZone$ qualifier on a characteristic grant narrows where
		// the granted characteristics function (Chief Engineer's "Artifact
		// spells you cast have convoke" carries AffectedZone$ Stack, so the
		// grant reaches the spell while it is on the stack and never a copy
		// of the same card sitting in hand). Parse failure stays closed.
		if ce.AffectedZone != "" && !ce.MayPlay {
			if zones, all, ok := effects.ParseZones(ce.AffectedZone); !ok || (!all && !slices.Contains(zones, zone)) {
				continue
			}
		}
		if len(ce.CantHaveKeywords) > 0 {
			cantHaveKeywords = append(cantHaveKeywords, ce.CantHaveKeywords)
		}
		switch ce.Layer {
		case LText:
			if ce.SetName != "" {
				name = ce.SetName
			}
			// CR 613.1d / CR 612: a text-changing effect replaces the text.
			// An outright TextSet (api:ExchangeTextBox's exchanged box, which
			// already carries the other object's substituted text) replaces
			// what the walk has so far; each TextFrom/TextTo then substitutes
			// in timestamp order, so two chained ChangeText effects compose the
			// way their timestamps order them.
			if ce.TextSetSet {
				text = ce.TextSet
			}
			if ce.TextFrom != "" {
				text = substituteTextWord(text, ce.TextFrom, ce.TextTo)
			}
		case LAbilities:
			// CR 613.1f / 613.4b: an ability-removing effect (Humility)
			// clears the object's printed and earlier-granted keywords before
			// later layer-6 grants re-add anything.
			if ce.RemoveAbilities {
				kw = kw[:0]
			}
			if len(ce.RemoveKeywords) > 0 {
				// CR 613.1f: this effect's own named keywords leave the
				// accumulated list BEFORE its AddKeywords append, so a
				// single effect that both removes and grants (mirage
				// phalanx's RemoveKeywords$ Soulbond | AddKeywords$ Haste)
				// yields the card text's result regardless of how the
				// timestamps order neighbour effects. A keyword is matched
				// by its HEAD (cards.KeywordHead), so a parameterised print
				// is removable by name.
				keptKW := kw[:0]
				for _, k := range kw {
					if !containsKeywordHead(ce.RemoveKeywords, k) {
						keptKW = append(keptKW, k)
					}
				}
				kw = keptKW
			}
			kw = append(kw, ce.AddKeywords...)
		case LType:
			// Already applied in typeCharacteristics above — layer 4 must
			// settle before any filter that tests a type runs.
		case LColor:
			// CR 613.1e: colour-set and colour-add effects apply in timestamp
			// order; an OverwriteColors grant replaces everything so far (an
			// empty set means an overwrite to colourless, the Animate
			// Colors$ Colorless shape), a plain one extends it.
			if ce.OverwriteColors {
				col = 0
			}
			// Letter elements are bounds-checked: state.ContinuousEffect is
			// exported, so a malformed element (empty, or not a WUBRG letter)
			// must be skipped, never an index panic -- a parse path in this
			// walk never crashes the match goroutine.
			for _, l := range ce.AddColors {
				if len(l) == 0 {
					continue
				}
				if i := strings.IndexByte("WUBRG", l[0]); i >= 0 {
					col |= effects.ColorMask(1 << i)
				}
			}
		}
	}
	// CR 613.1f / Forge Card.updateKeywords: a CantHaveKeyword$ prohibition is
	// a final filter after every layer-6 grant. It suppresses printed keywords,
	// marker-counter grants and later AddKeyword$ grants alike.
	for _, prohibited := range cantHaveKeywords {
		kept := kw[:0]
		for _, k := range kw {
			if !containsKeywordHead(prohibited, k) {
				kept = append(kept, k)
			}
		}
		kw = kept
	}
	colors := col.String()
	if e.derivedDepth <= 1 {
		// Keep the grown buffers on the Engine for the next build; a re-entrant
		// build's private buffers are discarded on return.
		e.derivedKW = kw
		e.derivedTypes = ty
	}
	// Layer 7 (P/T) runs AFTER the layer-3/5/6 walk above. CR 613's layers are
	// strictly ordered — no layer-7 result feeds a layer-5 characteristic — so
	// hoisting the read is exact, and it is what makes a layer-7 pump
	// expression that counts the affected object's OWN colours (Knight of New
	// Alara's AffectedX:Count$CardNumColors) terminate: the stash below serves
	// the object's finished layer-5 answer to Colors without re-entering
	// Derived, which would re-run this very P/T walk forever. Save/restore
	// keeps the stash correct when derivations nest (deriving Y inside X's
	// scalar walk stashes Y and restores X's on the way out).
	prevStashID, prevStashColors, prevStashSet := e.derivingColorsID, e.derivingColors, e.derivingColorsSet
	e.derivingColorsSet, e.derivingColorsID, e.derivingColors = true, id, colors
	power, toughness, basePower, baseToughness := e.derivedScalarFrom(id, o, f, active, kw, tyRaw, atStack == 0)
	e.derivingColorsSet, e.derivingColorsID, e.derivingColors = prevStashSet, prevStashID, prevStashColors
	e.derivedDepth--
	return Derived{Power: power, Toughness: toughness, BasePower: basePower, BaseToughness: baseToughness,
		Keywords: kw, Types: ty, Name: name, Text: text, Colors: colors, Controller: e.controllerOf(id)}
}

// derivedName is Derived(id).Name without the rest of the walk. The name is
// written only by a layer-3 SetName$ effect, and every layer-3 effect sorts
// ahead of the layer-6 group abilityDependencyOrder reorders, so when the
// full walk reaches one its keyword list is still the base list and its type
// list is typeCharacteristics'. This walk evaluates exactly those effects,
// with exactly those bindings (nil-ness included) and the same derivedDepth
// framing, in the same order, so it names what derivedCompute names. The
// keyword/type lists are built only when some SetName$ effect survives the
// Card.Self early rejection matchesWithCharsPT itself applies first.
func (e *Engine) derivedName(id state.ObjID) string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return ""
	}
	f := o.Face()
	faceDown := o.FaceDown && o.Zone == state.ZBattlefield
	if faceDown {
		f = faceDownBasis
	}
	name := f.Name
	active := e.active()
	var kw, ty []string
	built := false
	for i := range active {
		ce := &active[i]
		if ce.Layer != LText || ce.SetName == "" {
			continue
		}
		if ce.Affects == "Card.Self" && id != ce.Source {
			continue
		}
		if !built {
			built = true
			e.derivedDepth++
			if e.derivedDepth == 1 {
				kw, ty = e.derivedKW, e.derivedTypes
			}
			kw = derivedBaseKeywords(kw, o, f, faceDown)
			ty = append(ty[:0], e.typeCharacteristics(id, 0)...)
			if kw == nil {
				kw = []string{}
			}
			if ty == nil {
				ty = []string{}
			}
		}
		if !e.matchesWithChars(ce, id, ty, kw, 0) {
			continue
		}
		if ce.AffectedZone != "" && !ce.MayPlay {
			if zones, all, ok := effects.ParseZones(ce.AffectedZone); !ok || (!all && !slices.Contains(zones, o.Zone)) {
				continue
			}
		}
		name = ce.SetName
	}
	if built {
		if e.derivedDepth == 1 {
			e.derivedKW, e.derivedTypes = kw, ty
		}
		e.derivedDepth--
	}
	if derivedMemoVerify {
		if want := e.derivedCompute(id, 0).Name; want != name {
			panic(fmt.Sprintf("rules: derivedName(%d) = %q, full walk %q", id, name, want))
		}
	}
	return name
}

// derivedBaseKeywords is the keyword list the layer walk starts from --
// printed, intrinsic, marker-counter and status keywords, in that order --
// appended into kw[:0]. derivedCompute and derivedName share it so both see
// the same basis.
func derivedBaseKeywords(kw []string, o *state.Object, f *cards.Face, faceDown bool) []string {
	kw = append(kw[:0], f.Keywords...)
	kw = append(kw, o.IntrinsicKeywords...)
	// CR 122.1b: a marker counter whose kind names a keyword grants that
	// keyword to the permanent it sits on (Forge's CounterKeywordType emits a
	// Mode$ Continuous | AddKeyword$ static, EffectZone$ All). Appended here,
	// ahead of the layer walk, so the grant is a base keyword the layer-6
	// walk then removes or replaces exactly as it would Forge's static -- a
	// RemoveAbilities/RemoveKeywords effect clears it and a later layer-6
	// grant re-adds on top. Iterating o.Counters (a fixed-order slice) keeps
	// this deterministic; cards.CounterKeyword is the single classifier, so
	// every counter-to-keyword read agrees. Order is buttoned by the counter
	// slice, which is append-order stable.
	for _, c := range o.Counters {
		if kwName, ok := cards.CounterKeyword(c.Kind); ok && c.N > 0 {
			kw = append(kw, kwName)
		}
	}
	// CR 708.5's cloak variant: a CLOAKED face-down card is a 2/2 creature
	// with ward {2} -- the ward is part of the cloak status itself, not a
	// printed or granted ability (the printed face does not exist while face
	// down, CR 708.8, and faceDownBasis carries no keywords). Appending it
	// here -- ahead of the layer walk, exactly where a layer-6 grant would
	// land -- is what feeds checkGrantedWardTriggers's derived-keyword scan
	// (rules/trigger_match.go), so targeting a cloaked 2/2 meets the real
	// pay-or-counter ask. Leaving the battlefield clears both flags together
	// (events.Apply's Move reset), so the ward drops with the face-down
	// status.
	if faceDown && o.Cloaked {
		kw = append(kw, "Ward:2")
	}
	// CR 702.157b: a suspected creature has menace. The designation is a
	// status, not an ability, so appending it here -- ahead of the layer
	// walk, exactly where the cloak's status ward lands -- is the same grant
	// shape; leaving the battlefield or another player gaining control
	// clears it (events.Apply's Move and ControlChange folds), so the menace
	// drops with the designation.
	if o.Suspected {
		kw = append(kw, "Menace")
	}
	// A Pump/PumpAll "it gains suspend" grant is event-backed because the
	// target may be in exile (where ordinary continuous effects still apply),
	// and because cast legality and filters must agree after replay. Keep it in
	// the same derived keyword stream as printed and layer-6 keywords.
	if o.SuspendGranted {
		kw = append(kw, "Suspend")
	}
	return kw
}

// substituteTextWord replaces every whole-word, case-insensitive instance of
// from in text with to (CR 612's "replace all instances of one ... word"):
// the match is bounded by non-letter characters on both sides, so substituting
// "Wall" never rewrites "Wallop" and substituting "Elf" never rewrites
// "Elves". The replacement is inserted verbatim (the corpus's replacement
// words are printed forms like "Vampire", "blue", "Mountain"), so a
// reproduced word keeps the card's own spelling. An empty from never matches;
// an empty to deletes the matched word.

// abilityKWAfter applies one layer-6 effect's keyword action to a COPY of
// the walk's keyword list -- the same three steps the main walk's LAbilities
// arm performs, in the same order -- so the dependency simulation can test a
// match against the list as the effect would leave it.
func abilityKWAfter(ce *ContinuousEffect, kw []string) []string {
	out := append([]string(nil), kw...)
	if ce.RemoveAbilities {
		out = out[:0]
	}
	if len(ce.RemoveKeywords) > 0 {
		kept := out[:0]
		for _, k := range out {
			if !containsKeywordHead(ce.RemoveKeywords, k) {
				kept = append(kept, k)
			}
		}
		out = kept
	}
	out = append(out, ce.AddKeywords...)
	if len(ce.CantHaveKeywords) > 0 {
		kept := out[:0]
		for _, k := range out {
			if !containsKeywordHead(ce.CantHaveKeywords, k) {
				kept = append(kept, k)
			}
		}
		out = kept
	}
	return out
}

// abilityDependencyOrder applies CR 613.6's dependency reordering to the
// walk's layer-6 (LAbilities) effects. Timestamp order (active()'s sort) is
// the default, but a layer-6 effect whose Affected$ spec reads the walk's
// keyword list -- effects.SpecReadsKeywords's `with<Keyword>`/
// `without<Keyword>` predicates and Affinity base -- is DEPENDENT on any
// other layer-6 effect whose application would change what it applies to
// (CR 613.8's test: applying the other would change the match), and CR
// 613.6 applies a dependent effect after the one it depends on. The measured
// miss is Cavalry Master's `Creature.Other+withFlanking+YouCtrl` lord that
// entered BEFORE a Sidewinder Sliver: raw timestamp order evaluated the
// lord's `withFlanking` against the pre-grant keyword list, the gate failed,
// and the Sliver's own grant never produced the second instance (CR
// 702.25b). A negative gate (`without<Keyword>`) is the same dependency
// pointing the other way: the dependent lord applies after the grant and
// stops matching the now-empowered object, exactly the Muraganda
// Petroglyphs ruling's reading. The per-pair test is CR 613.8's own
// simulation -- B's match with the pre-group keyword list versus that list
// after A's action -- so a pair whose match does not move keeps timestamp
// order. Several dependent effects on one dependency apply in timestamp
// order after it; a dependency cycle falls back to timestamp order (CR
// 613.6). Everything is deterministic: the selection pass scans candidates
// in the fixed timestamp-ordered sequence and the simulation reads only
// fixed lists, so no map iteration reaches an event.
func (e *Engine) abilityDependencyOrder(active []ContinuousEffect, id state.ObjID, ty, kw []string, atStack state.Zone) []ContinuousEffect {
	// The layer-6 effects are contiguous in active()'s (layer, timestamp)
	// sort; only they can act on the walk's keyword list.
	start := -1
	for i := range active {
		if active[i].Layer == LAbilities {
			start = i
			break
		}
	}
	if start < 0 {
		return active
	}
	end := start
	for end < len(active) && active[end].Layer == LAbilities {
		end++
	}
	group := active[start:end]
	var gated, mods []int
	for gi := range group {
		ce := &group[gi]
		if ce.RemoveAbilities || len(ce.RemoveKeywords) > 0 || len(ce.CantHaveKeywords) > 0 || len(ce.AddKeywords) > 0 {
			mods = append(mods, gi)
		}
		if effects.SpecReadsKeywords(ce.Affects) {
			gated = append(gated, gi)
		}
	}
	if len(gated) == 0 || len(mods) == 0 {
		return active
	}
	// dep[j] holds the group indices effect j must FOLLOW (its
	// dependencies), discovered by the CR 613.8 simulation: B's match with
	// the pre-group keyword list against that list after A's action. The
	// pre-group list is the right basis because CR 613.8's second step takes
	// into account what currently applies and what earlier layers already
	// applied, but not what any other effect in the same layer is doing.
	dep := make([][]int, len(group))
	edges := 0
	for _, gj := range gated {
		b := &group[gj]
		base := e.matchesWithChars(b, id, ty, kw, atStack)
		for _, gm := range mods {
			if gm == gj {
				continue
			}
			after := e.matchesWithChars(b, id, ty, abilityKWAfter(&group[gm], kw), atStack)
			if after != base {
				dep[gj] = append(dep[gj], gm)
				edges++
			}
		}
	}
	if edges == 0 {
		return active
	}
	// Kahn's algorithm over the timestamp-ordered group: repeatedly emit the
	// timestamp-earliest effect whose dependencies are all emitted, so the
	// order stays timestamp order wherever dependencies do not bind. If a
	// pass makes no progress the remaining effects form a dependency cycle,
	// which CR 613.6 ignores in timestamp order.
	out := make([]ContinuousEffect, 0, len(active))
	out = append(out, active[:start]...)
	done := make([]bool, len(group))
	remaining := len(group)
	for remaining > 0 {
		picked := -1
		for gi := 0; gi < len(group); gi++ {
			if done[gi] {
				continue
			}
			ready := true
			for _, m := range dep[gi] {
				if !done[m] {
					ready = false
					break
				}
			}
			if ready {
				picked = gi
				break
			}
		}
		if picked < 0 {
			for gi := 0; gi < len(group); gi++ {
				if !done[gi] {
					picked = gi
					break
				}
			}
		}
		done[picked] = true
		remaining--
		out = append(out, group[picked])
	}
	out = append(out, active[end:]...)
	return out
}

// Name returns the current layer-3 name of an object. Callers that render or
// compare characteristics must use this rather than the printed face name.
func (e *Engine) Name(id state.ObjID) string { return e.Derived(id).Name }

// Text returns the object's current layer-3 text (CR 613.1d / CR 612): its
// printed Oracle text after every applicable text-changing effect. Callers
// that render or compare an object's rules text must use this rather than
// o.Face().Oracle.
func (e *Engine) Text(id state.ObjID) string { return e.Derived(id).Text }

// substituteTextWord replaces every whole-word, case-insensitive instance of
// from in text with to (CR 612's "replace all instances of one ... word").
// A match is a run equal to `from` under EqualFold bounded by non-letter
// characters, so "Wall" never rewrites "Wallop" and "Elf" never rewrites
// "Elves"; the replacement is inserted verbatim. Deterministic and
// allocation-light: it walks the bytes once, appending into a builder only
// when a match is found.
func substituteTextWord(text, from, to string) string {
	if from == "" {
		return text
	}
	lowerText := strings.ToLower(text)
	lowerFrom := strings.ToLower(from)
	var b strings.Builder
	changed := false
	i := 0
	for i < len(text) {
		j := strings.Index(lowerText[i:], lowerFrom)
		if j < 0 {
			break
		}
		start := i + j
		end := start + len(from)
		// Whole-word boundaries: the character before start and after end (if
		// any) must not be a letter. indexOf runs over bytes; the corpus's
		// words are ASCII, and a non-ASCII byte is not a letter by isLetter's
		// byte test, so a Unicode word boundary degrades conservatively (it
		// never splits a multi-byte rune inside a match because from is only
		// matched as a byte run and cannot start mid-rune when from is ASCII).
		if (start == 0 || !isLetterByte(text[start-1])) && (end >= len(text) || !isLetterByte(text[end])) {
			if !changed {
				b.Grow(len(text))
				changed = true
			}
			b.WriteString(text[i:start])
			b.WriteString(to)
			i = end
			continue
		}
		// Not a whole word: keep searching from the character after this
		// occurrence's start so an overlapping later match is still found.
		if !changed {
			b.Grow(len(text))
			changed = true
		}
		b.WriteString(text[i : start+1])
		i = start + 1
	}
	if !changed {
		return text
	}
	b.WriteString(text[i:])
	return b.String()
}

// isLetterByte reports whether c is an ASCII letter, the whole-word boundary
// test substituteTextWord uses (a digit or underscore counts as a boundary,
// matching CR 612's word sense closely enough for the corpus's words).
func isLetterByte(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func (e *Engine) Power(id state.ObjID) int32 {
	p, _ := e.derivedScalar(id)
	return p
}

func (e *Engine) Toughness(id state.ObjID) int32 {
	_, t := e.derivedScalar(id)
	return t
}

// HasKeyword matches case-insensitively, like its sibling cards.Face.HasKeyword
// (Ruling T19-b) — every existing call site already goes through that
// case-insensitive comparison, so an exact-match Engine.HasKeyword would have
// been a silent trap for the first caller with non-canonical-cased input.
func (e *Engine) HasKeyword(id state.ObjID, kw string) bool {
	return e.hasKeywordH(id, kwHeadOf(kw))
}

// hasKeywordH is HasKeyword for a precompiled head (rules/keyword_heads.go).
func (e *Engine) hasKeywordH(id state.ObjID, h kwHead) bool {
	kw := h.s
	if !e.mayHaveDerivedKeywordH(id, h) {
		if derivedMemoVerify {
			e.verifyKeywordPrecheck(id, kw)
		}
		return false
	}
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), kw) {
			return true
		}
	}
	return false
}

// derivedKeywordParam is Face.KeywordParam over the object's CURRENT derived
// keyword list (printed plus layer-6 granted), so a keyword a continuous
// effect delivered (Underworld Breach's AddKeyword$ Escape grant, Snapcaster
// Mage's Flashback) is readable exactly where the printed one would be.
func (e *Engine) derivedKeywordParam(id state.ObjID, head string) (string, bool) {
	return e.derivedKeywordParamH(id, kwHeadOf(head))
}

// derivedKeywordParamH is derivedKeywordParam for a precompiled head.
func (e *Engine) derivedKeywordParamH(id state.ObjID, h kwHead) (string, bool) {
	head := h.s
	if !e.mayHaveDerivedKeywordH(id, h) {
		if derivedMemoVerify {
			e.verifyKeywordPrecheck(id, head)
		}
		return "", false
	}
	for _, k := range e.Derived(id).Keywords {
		if strings.EqualFold(cardsKeywordHead(k), head) {
			if i := strings.IndexByte(k, ':'); i >= 0 {
				return strings.TrimSpace(k[i+1:]), true
			}
			return "", true
		}
	}
	return "", false
}

// ToxicValue is the object's total toxic N (CR 702.164), SUMMED over every
// `Toxic:<N>` entry on its CURRENT derived keyword list -- CR 702.164c makes
// multiple toxic instances cumulative (a printed Toxic 2 Ixhel equipped by
// Prosthetic Injector's AddKeyword$ Toxic:1 is toxic 3), so the read cannot
// stop at the first entry the way the singleton derivedKeywordParam helper
// does. A layer-6 grant (the Rat lord, an Aura, an Equipment) is readable
// exactly where the printed K:Toxic line is, the same derived read
// HasKeyword/derivedKeywordParam give every other keyword. Each entry whose
// parameter is absent or not a positive integer contributes 0 (a non-numeric
// N can only be a malformed script, so failing closed to no poison from that
// entry is the conservative direction); the whole read reports 0 when the
// object has no toxic at all.
func (e *Engine) ToxicValue(id state.ObjID) int {
	total := 0
	for _, k := range e.Derived(id).Keywords {
		if !strings.EqualFold(cardsKeywordHead(k), "Toxic") {
			continue
		}
		raw := ""
		if i := strings.IndexByte(k, ':'); i >= 0 {
			raw = strings.TrimSpace(k[i+1:])
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			continue
		}
		total += n
	}
	return total
}

// derivedTypesOf is Derived(id).Types' content -- the layer-4 result
// typeCharacteristics computes, which is exactly what derivedCompute copies
// into Types -- without the layer-3/5/6/7 walks the rest of Derived runs.
// It keeps derivedCompute's framing (the faceless-object guard and the
// derivedDepth bump a nested read sees), so every read is the same answer.
// Only a read-only caller that tests membership may use it: the result may
// alias the printed face's slice, and its nil-ness is not Derived's (an
// empty list may be nil here, where Derived binds []string{}), so a
// SpecContext ExtraTypes binding must still read Derived.
func (e *Engine) derivedTypesOf(id state.ObjID) []string {
	o := e.G.Obj(id)
	if o == nil || o.Face() == nil {
		return nil
	}
	e.derivedDepth++
	ty := e.typeCharacteristics(id, 0)
	e.derivedDepth--
	if derivedMemoVerify {
		if want := e.derivedCompute(id, 0).Types; !slices.Equal(ty, want) {
			panic(fmt.Sprintf("rules: derivedTypesOf(%d) = %v, full walk %v", id, ty, want))
		}
	}
	return ty
}

// IsCreature reads the current layer-derived type list. In particular, a
// planeswalker animated by a layer-4 effect is a creature for damage marking,
// even though its printed face is not.
func (e *Engine) IsCreature(id state.ObjID) bool {
	for _, typ := range e.derivedTypesOf(id) {
		if typ == "Creature" {
			return true
		}
	}
	return false
}

// IsLand reads the current layer-derived type list -- IsCreature's twin for
// the CR 704.5n Fortification legality SBA: a bearer that LOST its Land type
// to a layer-4 static is no longer a legal Fortification bearer, and one that
// gained a type (an animated manland) still is.
func (e *Engine) IsLand(id state.ObjID) bool {
	for _, typ := range e.derivedTypesOf(id) {
		if typ == "Land" {
			return true
		}
	}
	return false
}

// Colors is the object's current layer-5 colour set as WUBRG letters (see
// Derived.Colors); "" is a colourless object. Every rules-side colour read
// about a live object goes through this (objColors below for callers that
// already hold the *state.Object) rather than effects.ColorsOf's face read,
// so an animated manland's granted colours are real everywhere the engine
// consults them -- protection qualities, Fear's black-blocker test, convoke's
// colour contributions, the Count$...$Colors heads.
func (e *Engine) Colors(id state.ObjID) string {
	// A read of an object whose own characteristics are being derived right
	// now (its layer-7 pump expression counting its own colours) is served
	// from the stash derivedWith set before its layer-7 walk — re-entering
	// Derived here would recurse forever. CR 613's layers are ordered: no
	// layer-7 result feeds layer 5, so the stashed answer is the finished
	// layer-5 result, exact rather than an approximation.
	if e.derivingColorsSet && e.derivingColorsID == id {
		return e.derivingColors
	}
	return e.Derived(id).Colors
}

// objColors is Colors for a caller holding the object rather than the id:
// a battlefield permanent reads its derived (layer-5) colours; anything off
// the battlefield has no continuous characteristics (CR 613.6 -- a spell on
// the stack shows its face's colours) and falls back to the face read,
// which also covers LKI snapshots keyed by an id that may no longer resolve.
func (e *Engine) objColors(o *state.Object) string {
	if o != nil && o.Zone == state.ZBattlefield {
		return e.Colors(o.ID)
	}
	return effects.ColorsOf(o)
}

// Keywords exists for Ruling F2: Task 23's view.Chars interface needs a
// Keywords(state.ObjID) []string method, and Engine.Derived already returns
// a Derived struct — a method of the same name on Engine could not satisfy
// an interface expecting a slice. This is that method; Derived(id).Keywords
// remains the field other engine-internal code should read when it also
// wants Power/Toughness/Types in the same call.
func (e *Engine) Keywords(id state.ObjID) []string {
	// The printed fast path (derived_printed.go) answers with the face's own
	// list, which no later derivation rewrites.
	if kw, ok := e.printedKeywordsOnly(id); ok {
		return kw
	}
	return e.Derived(id).Keywords
}
