package chars

import (
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// PT is the layer-7 P/T walk proper. kw is the object's
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
// types/haveTypes: a caller that already holds Types(b, b.Active(), id, 0)'s
// exact result (Compute on a live-zone derivation) passes it with
// haveTypes set, so the layer-4 walk is not run a second time; otherwise the
// walk computes it.
func PT(b Board, s *Scratch, id state.ObjID, o *state.Object, f *cards.Face, active []state.ContinuousEffect, kw []string, types []string, haveTypes bool) (power, toughness, basePower, baseToughness int32) {
	frameIndex := len(s.PTFrames)
	s.PTFrames = append(s.PTFrames, PTFrame{ID: id})
	defer func() { s.PTFrames = s.PTFrames[:frameIndex] }()
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
		s.PTFrames[frameIndex] = PTFrame{
			ID:                  id,
			Power:               currentPower,
			Toughness:           currentToughness,
			BasePower:           basePower,
			BaseToughness:       baseToughness,
			PreCounterPower:     preCounterPower,
			PreCounterToughness: preCounterToughness,
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
		if p, tp, hp, ht := CDASetPT(b, o); hp || ht {
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
	// Types is 837910f4's layer-4-aware type derivation; the
	// active list comes in as a parameter (230574a2's plumbing) because
	// active() is a cached, idempotent read — same slice, no recomputation.
	if !haveTypes {
		types = Types(b, b.Active(), id, 0)
	}
	for i := range active {
		ce := &active[i]
		if ce.Layer != state.LPT {
			continue
		}
		// kw is the FINISHED layer-6 keyword list bound by the caller (see
		// PT): a layer-7 pump gated on a granted keyword
		// (`Affected$ ...+withFlying`) must see the grant CR 613 ordered
		// below it. matchesWithChars reads a nil list as the printed-face
		// fallback, so a caller that did not build one is unchanged.
		setFrame()
		if !b.Matches(ce, id, types, kw, 0, PTBind{Power: power, Toughness: toughness, BasePower: basePower, BaseToughness: baseToughness, Has: true}) {
			continue
		}
		switch ce.Sub {
		case state.SubCDA, state.SubSet:
			if ce.HasSet {
				if ce.StaticSet {
					// A static can set just power or just toughness. Its omitted
					// parameter must leave the printed/earlier-layer value alone,
					// rather than treating the empty expression as numeric zero.
					if ce.SetPowerPresent {
						power = ce.SetPower
						if ce.SetPowerExpr != "" {
							power = b.StaticAmount(ce, ce.SetPowerExpr, ce.Source)
						}
					}
					if ce.SetToughnessPresent {
						toughness = ce.SetToughness
						if ce.SetToughnessExpr != "" {
							toughness = b.StaticAmount(ce, ce.SetToughnessExpr, ce.Source)
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
		case state.SubModify:
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
				addPower = b.StaticAmount(ce, ce.AddPowerExpr, anchor)
			}
			if ce.AddToughnessExpr != "" {
				anchor := ce.Source
				if ce.AddToughnessAffected {
					anchor = id
				}
				addToughness = b.StaticAmount(ce, ce.AddToughnessExpr, anchor)
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

// Compute is the full layer walk (CR 613): the object's printed values, then
// every applicable continuous effect in layer order, then layer 7d counters
// last. It is rules' uncached Derived build (Engine.derivedCompute): its
// Keywords/Types alias the s.KW/s.Types scratch, rewritten in place by the
// next build, so a caller treats them as read-only and does not retain them
// past building its own view. rules/derivedmemo.go's walk-scoped memo calls
// it on a miss (and on every hit in verify mode) and copies the result into
// owned storage. A malformed or missing object degrades to the zero record.
func Compute(b Board, s *Scratch, id state.ObjID, atStack state.Zone) effects.Chars {
	o := b.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return effects.Chars{}
	}
	f := o.Face()
	// CR 708.5: while a battlefield object is face down its printed face
	// does not exist -- the derived basis is a vanilla 2/2 Creature face
	// (PT pins the 2/2 base; this synthetic face carries no
	// keywords or printed types, and the colour basis below is overridden
	// to none). Layer effects from OTHER permanents still apply on top (an
	// Anthem pumps a manifested 2/2 to 3/3); the printed-face scans never
	// reach here because faceDownPrintedHides gates them all off (CR 708.8).
	faceDown := o.FaceDown && o.Zone == state.ZBattlefield
	if faceDown {
		f = faceDownBasis
	}
	active := b.Active()
	zone := o.Zone
	if atStack != 0 {
		zone = atStack
	}
	s.Depth++
	kw := s.KW
	ty := s.Types
	if s.Depth > 1 {
		// Re-entrant (a nested Derived mid-build): own private buffers rather
		// than overwrite the outer call's backing arrays mid-range. Same guard
		// Task A2 uses for forEachObject and rules uses for active(). (This
		// path is effectively unreachable — MatchesSpecFrom reads faces, never
		// calls Derived — but it keeps the buffer discipline airtight.)
		kw = nil
		ty = nil
	}
	kw = BaseKeywords(kw, o, f, faceDown)
	// Layer 4 runs first through Types (see above), so every
	// later effect's Affected$ filter — and every layer-4 effect's own —
	// sees the derived type list, not the printed face.
	tyRaw := Types(b, b.Active(), id, atStack)
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
	seq := abilityDependencyOrder(b, active, id, ty, kw, atStack)
	var cantHaveKeywords [][]string
	for i := range seq {
		ce := &seq[i]
		// Only layers 3, 5 and 6 (and a CantHaveKeywords$ prohibition, which
		// any layer's effect may carry) act in this walk: layer 4 settled in
		// Types above and layer 7 is PT's walk
		// below. Matching an effect this walk would then ignore is pure cost
		// (the match has no side effect), so skip it before the match.
		if len(ce.CantHaveKeywords) == 0 && ce.Layer != state.LText && ce.Layer != state.LColor && ce.Layer != state.LAbilities {
			continue
		}
		// kw is the walk's keywords-so-far list for THIS object (printed
		// keywords, IntrinsicKeywords, marker-counter grants and every
		// layer-6 grant applied so far), bound exactly as ty is: an
		// `Affected$ ...+with<Keyword>` lord must see a keyword an earlier
		// effect granted. At the walk's end the FINISHED list is what
		// PT's layer-7 walk binds -- CR 613 orders layer 6
		// strictly before layer 7, so the P/T applicability gate reads the
		// completed grant stream (Windstorm Drake over Levitation).
		if !matchesWithChars(b, ce, id, ty, kw, atStack) {
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
		case state.LText:
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
		case state.LAbilities:
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
		case state.LType:
			// Already applied in Types above — layer 4 must
			// settle before any filter that tests a type runs.
		case state.LColor:
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
	if s.Depth <= 1 {
		// Keep the grown buffers on the scratch for the next build; a re-entrant
		// build's private buffers are discarded on return.
		s.KW = kw
		s.Types = ty
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
	prevStashID, prevStashColors, prevStashSet := s.ColorsID, s.Colors, s.ColorsSet
	s.ColorsSet, s.ColorsID, s.Colors = true, id, colors
	power, toughness, basePower, baseToughness := PT(b, s, id, o, f, active, kw, tyRaw, atStack == 0)
	s.ColorsSet, s.ColorsID, s.Colors = prevStashSet, prevStashID, prevStashColors
	s.Depth--
	return effects.Chars{Power: power, Toughness: toughness, BasePower: basePower, BaseToughness: baseToughness,
		Keywords: kw, Types: ty, Name: name, Text: text, Colors: colors, Controller: b.ControllerOf(id)}
}

// Name is Derived(id).Name without the rest of the walk. The name is
// written only by a layer-3 SetName$ effect, and every layer-3 effect sorts
// ahead of the layer-6 group abilityDependencyOrder reorders, so when the
// full walk reaches one its keyword list is still the base list and its type
// list is Types'. This walk evaluates exactly those effects,
// with exactly those bindings (nil-ness included) and the same s.Depth
// framing, in the same order, so it names what Compute names. The
// keyword/type lists are built only when some SetName$ effect survives the
// Card.Self early rejection the engine's Board.Matches itself applies first.
func Name(b Board, s *Scratch, id state.ObjID) string {
	o := b.Game().Obj(id)
	if o == nil || o.Face() == nil {
		return ""
	}
	f := o.Face()
	faceDown := o.FaceDown && o.Zone == state.ZBattlefield
	if faceDown {
		f = faceDownBasis
	}
	name := f.Name
	active := b.Active()
	var kw, ty []string
	built := false
	for i := range active {
		ce := &active[i]
		if ce.Layer != state.LText || ce.SetName == "" {
			continue
		}
		if ce.Affects == "Card.Self" && id != ce.Source {
			continue
		}
		if !built {
			built = true
			s.Depth++
			if s.Depth == 1 {
				kw, ty = s.KW, s.Types
			}
			kw = BaseKeywords(kw, o, f, faceDown)
			ty = append(ty[:0], Types(b, b.Active(), id, 0)...)
			if kw == nil {
				kw = []string{}
			}
			if ty == nil {
				ty = []string{}
			}
		}
		if !matchesWithChars(b, ce, id, ty, kw, 0) {
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
		if s.Depth == 1 {
			s.KW, s.Types = kw, ty
		}
		s.Depth--
	}
	return name
}

// BaseKeywords is the keyword list the layer walk starts from --
// printed, intrinsic, marker-counter and status keywords, in that order --
// appended into kw[:0]. Compute and Name share it so both see
// the same basis.
func BaseKeywords(kw []string, o *state.Object, f *cards.Face, faceDown bool) []string {
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
func abilityKWAfter(ce *state.ContinuousEffect, kw []string) []string {
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
func abilityDependencyOrder(b Board, active []state.ContinuousEffect, id state.ObjID, ty, kw []string, atStack state.Zone) []state.ContinuousEffect {
	// The layer-6 effects are contiguous in active()'s (layer, timestamp)
	// sort; only they can act on the walk's keyword list.
	start := -1
	for i := range active {
		if active[i].Layer == state.LAbilities {
			start = i
			break
		}
	}
	if start < 0 {
		return active
	}
	end := start
	for end < len(active) && active[end].Layer == state.LAbilities {
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
		bce := &group[gj]
		base := matchesWithChars(b, bce, id, ty, kw, atStack)
		for _, gm := range mods {
			if gm == gj {
				continue
			}
			after := matchesWithChars(b, bce, id, ty, abilityKWAfter(&group[gm], kw), atStack)
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
	out := make([]state.ContinuousEffect, 0, len(active))
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
