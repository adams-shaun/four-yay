// Layer application is CR 613: characteristics settle in a fixed layer order
// (copy, control, text, type, color, abilities, power/toughness), and within
// layer 7 in a further sublayer order (characteristic-defining, setting,
// modifying, counters, switching). M1 only produces effects in layers 6 and
// 7, but the full ladder is defined now so a later layer 2 control-change or
// layer 4 type-change effect is an addition to this file, not a rewrite of
// it — that retrofit is the project's own top-named risk.
package rules

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// emptySVars is the shared empty SVar table a merged under-card face with no
// SVars of its own is stamped with, so ContinuousEffect.SVars stays non-nil
// for every merged face (nil means "read the source object's active face"
// downstream). It is never written to.
var emptySVars = map[string]string{}

// staticEffects reads every battlefield permanent's own S:Mode$ Continuous
// statics into ContinuousEffects, so a card whose entire rules text is a
// static (an Equipment's EquippedBy pump, an Aura's EnchantedBy pump, a
// vanilla lord's global pump) actually applies instead of silently doing
// nothing. Task 14 is what first wires static text into the layer system:
// until it, every S: line this build cared about was a play restriction
// (CantBeCast etc.), and the Mode$ Continuous statics every creature-lord
// and echo of Equipment/Enchant text carries were parsed but never turned
// into an effect -- a card with nothing but static text read as "does
// nothing".
//
// Reading these live off the battlefield permanent (the same scan activeStatics
// performs for restrictions) rather than registering them at ETB means a
// permanent placed on the battlefield by any path -- cast, a raw MoveZone in
// a test -- is covered, the effect expiry problem is solved for free (active()
// only walks battlefield permanents, so a departed source contributes nothing
// this call), and there is no registration event to keep in step with replay.
// Each static breaks into one effect per layer it touches -- AddPower/
// AddToughness is a layer-7 modify, AddKeyword a layer-6 grant, AddTypes a
// layer-4 type change -- so the layer ordering Derived applies (CR 613) still
// holds when one static carries both a pump and a keyword (exactly the Sword
// of Fire and Ice / Umezawa shape Task 14's tests build). The scan order is
// deterministic: AliveFrom(0) walks seats in fixed APNAP order, each
// battlefield zone is a slice, and each face's Statics is its parsed script
// order -- nothing here ranges a map, so the resulting option/view/settle
// order stays reproducible run to run (determinism requirement 3 of the
// dispatch).
//
// dst is the caller-owned static memo's reusable outer storage, distinct from
// activeBuf. This scan calls no callbacks and cannot re-enter; each nested
// keyword/type slice is freshly parsed and remains read-only after active()
// copies the effect values. Only the outer slots are overwritten here.
func (e *Engine) staticEffects(dst []ContinuousEffect) []ContinuousEffect {
	out := e.staticEffectsWalk(dst, true)
	if staticZoneSkipVerify {
		full := e.staticEffectsWalk(nil, false)
		if len(full) != len(out) || (len(out) > 0 && !reflect.DeepEqual(full, out)) {
			panic(fmt.Sprintf("rules: static zone skip changed staticEffects at log %d (%d vs %d effects)", len(e.L.Events), len(out), len(full)))
		}
	}
	return out
}

// staticEffectsWalk is staticEffects' scan; skip visits only the static-hot
// subsequence of each summarized off-battlefield zone (static_zoneskip.go),
// which every object it leaves out would have been skipped at the
// ContinuousStaticsMayFunctionOffBattlefield gate below anyway.
func (e *Engine) staticEffectsWalk(dst []ContinuousEffect, skip bool) []ContinuousEffect {
	// This walk is a full rescan, so it is the place the per-build gate flag
	// is recomputed. staticEffects calls no callbacks and cannot re-enter
	// (see its doc), so reset/set ordering is safe. A verify-only second walk
	// (staticZoneSkipVerify) resets it again below and sets it identically.
	e.staticMemoGated = false
	e.staticMemoStateRead = false
	out := dst[:0]
	for pi, p := range e.G.AliveFrom(0) {
		// staticSourceZones (below) walks the battlefield FIRST so every
		// battlefield static keeps today's relative emission order, then the
		// non-battlefield zones an EffectZone$ can name. The shared stack is
		// walked exactly once, under the first alive seat -- the same
		// collectCostStatics discipline, which without it would collect each
		// stack card's statics once per seat.
		for _, z := range staticSourceZones {
			if z == state.ZStack && pi > 0 {
				continue
			}
			ids := e.G.Zone(z, p)
			if skip {
				ids = e.staticSourceIDs(p, z)
			}
			for _, id := range ids {
				o := e.G.Obj(id)
				if o == nil {
					continue
				}
				f := o.Face()
				if f == nil {
					continue
				}
				onBattlefield := z == state.ZBattlefield
				if !onBattlefield && !f.ContinuousStaticsMayFunctionOffBattlefield() {
					// Off the battlefield only the object's own face is walked
					// (an unlocked Room face and a mutated pile's under-cards
					// are battlefield-only, below), so a face printing no
					// statics emits nothing -- most of every library is this --
					// and neither does one none of whose Continuous statics can
					// pass the source-zone gate below off the battlefield (the
					// face's derived probe; see cards.Face.
					// ContinuousStaticsMayFunctionOffBattlefield). Every such
					// static would be skipped at the Mode or zone check before
					// emitting or queueing anything.
					continue
				}
				if onBattlefield && e.faceDownPrintedHides(o) {
					// CR 708.8: a face-down permanent's printed statics do not
					// exist while it is face down (the one gate shared with
					// activeStatics, the trigger scan and the offer loops).
					continue
				}
				if onBattlefield && o.PhasedOut {
					// CR 702.25b/d: a phased-out permanent is treated as though it
					// does not exist, so its own statics do not function (the same
					// one gate shared with activeStatics).
					continue
				}
				// Enchantment Rooms (rules/rooms.go): once the room's second door
				// is unlocked, the ALTERNATE face's statics are live too -- a room
				// permanent's rules text is both halves' combined after the
				// unlock (CR 309.6), each face's Statics its own scan. A room's
				// unlocked face exists only on the battlefield.
				faces := []*cards.Face{f}
				if onBattlefield && o.Unlocked && isRoom(o) && len(o.Card.Faces) == 2 && int(o.FaceIdx) < len(o.Card.Faces) {
					faces = append(faces, o.Card.Faces[1-int(o.FaceIdx)])
				}
				// CR 702.140d: a mutated pile's under-card statics are live too,
				// exactly like the Rooms alternate face above. They are appended
				// in pile order AFTER the top face (and after the unlocked room
				// face, which is itself the top card's other half), so the
				// emission order -- and therefore every timestamp tie-break -- is
				// deterministic. A merged card exists only on the battlefield.
				mergedFrom := len(faces)
				if onBattlefield {
					for i := range o.MergedCards {
						if mf := o.MergedFaceAt(i); mf != nil {
							faces = append(faces, mf)
						}
					}
				}
				for fi, fc := range faces {
					// ContinuousEffect.SVars carries a table ONLY for a merged
					// under-card face -- a face that is a DIFFERENT CARD from the
					// one o.Face() resolves, so every downstream SVar read
					// (staticAmount, grantedAbilities' AddAbility$ body, the
					// staticView specCtx) would otherwise silently read the pile
					// TOP's table. The pile's own top face and an unlocked Room's
					// alternate face are faces of the SAME card and stay nil, so
					// those readers keep their o.Face() fallback: an alternate
					// face's grant resolves against the ACTIVE face's table
					// exactly as it did before merged faces joined this walk
					// (TestActionStaticMembershipPreservesOrderAndActiveFace locks
					// that -- the back face's `AddAbility$ Back` must not mint a
					// live {B} mana ability while the front face is up). A merged
					// face with no SVar table of its own gets an empty one rather
					// than nil, so "the under-card has no such SVar" resolves to
					// no grant instead of falling back to the top card's body.
					var faceSVars map[string]string
					if fi >= mergedFrom {
						faceSVars = fc.SVars
						if faceSVars == nil {
							faceSVars = emptySVars
						}
					}
					// grantQueue is the AddStaticAbility$ work queue, REUSED across
					// scans on the Engine's own buffer (staticQueueBuf): the face's
					// own statics at depth 0, then every granted static appended with
					// its depth, so a granted static's emission is the SAME body a
					// printed one runs -- one grant grammar (task
					// inbox-paramcensus-static-grant-misc). The depth-0 check below
					// bounds the recursion; the warm-rescan allocation budget
					// (static_effects_buffer_test) is why the buffer is reused,
					// never re-made per face.
					grantQueue := e.staticQueueBuf[:0]
					for _, st := range fc.Statics {
						grantQueue = append(grantQueue, staticWork{st: st})
					}
					for qi := 0; qi < len(grantQueue); qi++ {
						w := grantQueue[qi]
						st := w.st
						if st.Mode != "Continuous" {
							continue
						}
						// EffectZone$ -- the zone the static's SOURCE must sit in for
						// it to be live (Forge's StaticAbilityContinuous EffectZone$,
						// default the battlefield). This is the ONE read the other
						// static families (CantBeCast, RaiseCost/ReduceCost/SetCost,
						// the may-play walks) already make through effectZoneOK, and
						// the Continuous path now shares it. A battlefield-scoped or
						// default static keeps today's admission exactly; a
						// graveyard/command/exile-scoped static stops wrongly
						// applying while its source is on the battlefield (Anger's
						// haste grant belongs to the graveyard alone) and is instead
						// collected from the zone it names by the zone walk above.
						// An unrecognised value denies -- the fail-closed direction
						// effectZoneOK documents.
						// ExcludeZone$ -- the zone(s) the static's SOURCE must NOT sit in
						// for it to be live (Forge's mirror of EffectZone$; Grist, the
						// Hunger Tide's "As long as Grist isn't on the battlefield, it's a
						// 1/1 Insect creature in all other zones"). An exclusion with no
						// explicit EffectZone$ REPLACES the battlefield default: the static
						// is live in every other zone -- exactly the CR 604.3 every-zone
						// CDA reading minus the excluded zone(s). staticZoneAdmits (below)
						// is the ONE read both this gate and cdaPTStatic make, so the
						// emitted characteristic grant and the layer-7a P/T claim can
						// never disagree about where the static is live.
						if !e.stackSelfStaticOK(st, o) && !staticZoneAdmits(st.ParamStr(cards.PKExcludeZone), st.ParamStr(cards.PKEffectZone), o.Zone) {
							continue
						}
						affects := st.ParamStr(cards.PKAffected)
						// Forge omits Affected$ on a self-only characteristic-defining
						// static (Tarmogoyf, Krovikan Mist). Its default is the host
						// card, not "no affected object".
						if affects == "" {
							affects = "Card.Self"
						}
						// The "as long as" recheck gates (Forge's intervening-if on a
						// continuous static): IsPresent$/IsPresent2$ (an existence count
						// over every battlefield, PresentCompare$ pricing the count with
						// GE1 the default) and CheckSVar$/SVarCompare$ (the named SVar
						// compared under the threshold). staticEffects re-runs once per
						// emitted event (the staticContinuous memo's epoch key), so the
						// gate is a genuine continuous recheck: the board moves, the
						// grant follows -- Angelic Overseer's Human, Static Orb's
						// untapped state, Auriok Steelshaper's equipped state, Kiyomaro's
						// hand size. A gate this build cannot evaluate fails CLOSED
						// (the shipped statics convention rules/statics.go's
						// checkSVarHolds documents): the grant is withheld whole, never
						// silently always-applied.
						if st.MayHaveAnyParam(continuousGateKeys) {
							// A gate-carrying static was ENCOUNTERED -- set the flag even
							// when the gate holds, because a later battlefield-composition
							// change can flip a passing gate off. layercache.go's
							// staticSafeSince refuses a TokenCreate re-stamp whenever this
							// is set, so the memo is only reused across a token entry on a
							// board with no gate-carrying Continuous static anywhere.
							e.staticMemoGated = true
							if !e.continuousGateHolds(staticView{Source: id, Controller: o.Controller, Params: st.Params, PS: st.ParamSetOf(), SVars: faceSVars}) {
								continue
							}
						}
						base := ContinuousEffect{
							Source:     id,
							Timestamp:  o.Timestamp,
							Controller: o.Controller,
							Affects:    affects,
							SVars:      faceSVars,
						}
						if st.HasParam(cards.PKAddPower) || st.HasParam(cards.PKAddToughness) {
							pt := base
							pt.Layer, pt.Sub = LPT, SubModify
							pt.AddPowerExpr = st.ParamStr(cards.PKAddPower)
							pt.AddToughnessExpr = st.ParamStr(cards.PKAddToughness)
							pt.AddPowerAffected = effects.AffectedXStaticAmount(pt.AddPowerExpr)
							pt.AddToughnessAffected = effects.AffectedXStaticAmount(pt.AddToughnessExpr)
							out = append(out, pt)
						}
						if st.HasParam(cards.PKAddKeyword) || st.HasParam(cards.PKRemoveKeyword) || st.HasParam(cards.PKCantHaveKeyword) {
							kw := base
							kw.Layer = LAbilities
							kw.AddKeywords = statKeywords(st)
							kw.RemoveKeywords = statRemoveKeywords(st)
							kw.CantHaveKeywords = statCantHaveKeywords(st)
							kw.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
							if len(kw.AddKeywords) > 0 || len(kw.RemoveKeywords) > 0 || len(kw.CantHaveKeywords) > 0 {
								out = append(out, kw)
							}
						}
						// A printed Continuous AddAbility$ static (Ichormoon Gauntlet's
						// "Planeswalkers you control have [0]: Proliferate", a lord
						// granting an activated ability, an Equipment granting
						// "{T}: deal 1 damage") is a layer-6 ability GRANT (CR
						// 613.1f): one ContinuousEffect whose AddAbilities names the
						// SVar bodies on THIS source's face, consumed by legal.go's
						// grantedAbilities (the offer) and mana_activation.go's
						// granted-mana loop (the tap gate and payment window). The
						// grantor is base.Source and the recipient is whatever
						// Affects matches, so the two may differ -- the whole point of
						// a cross-object grant. statList splits the ` & ` and `,`
						// multi-value forms (6 corpus carriers). An AddAbility$ name
						// whose body is missing or is not an AB degrades to no grant
						// in grantedAbilities (the same totality every SVar
						// resolution takes), so no validation is needed here.
						if st.HasParam(cards.PKAddAbility) {
							ga := base
							ga.Layer = LAbilities
							ga.AddAbilities = statList(st, "AddAbility")
							ga.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
							if len(ga.AddAbilities) > 0 {
								out = append(out, ga)
							}
						}
						// A has-all-abilities-of static (CR 613.1f): Forge's
						// GainsAbilitiesOf$ / GainsTriggerAbsOf$ name a CARD filter, and
						// every object the static's Affected$ matches gains all
						// activated and/or triggered abilities of each named card while
						// it sits in the GainsAbilitiesOfZones$ zones (Idris, Soul of
						// the TARDIS: "NICKNAME has all activated and triggered
						// abilities of the exiled card", zones Exile). Unlike
						// AddAbility$ the bodies are compiled SAs on the FOREIGN card,
						// not SVar names on this source, so the effect carries the
						// foreign faces (state.GainedFace) and the offer/trigger walks
						// read them directly. The spec is evaluated with src = this
						// static's own source, which is exactly what makes
						// `Card.ExiledWithSource` resolve (effects' ExiledWith
						// provenance). A spec that matches nothing emits no effect, the
						// fail-closed direction every grant takes; the scan re-runs per
						// event, so a card exiled later is gained on the next rescan
						// and a card that leaves the scoped zones loses its grant.
						if st.MayHaveAnyParam(gainsAbilitiesKeys) && gainsAbilitiesOf(st) {
							e.staticMemoStateRead = true
							// The two parameters are resolved SEPARATELY and carried on
							// separate face lists: GainsAbilitiesOf$ means ACTIVATED
							// abilities only and GainsTriggerAbsOf$ TRIGGERED only (a
							// shared untyped list made a GainsAbilitiesOf-only card fire
							// the foreign card's phase triggers and a
							// GainsTriggerAbsOf-only card offer its activated ones -- the
							// round-2 review's break). A GainsValidAbilities$ filter and a
							// GainsAbilitiesLimitPerTurn$ cap ride the ACTIVATED half
							// (both parameters are activated-ability vocabulary).
							gg := base
							gg.Layer = LAbilities
							gg.GainedZones = strings.TrimSpace(st.Params["GainsAbilitiesOfZones"])
							gg.GainsValidAbilities = strings.TrimSpace(st.Params["GainsValidAbilities"])
							gg.GainsLimitPerTurn = gainsLimitPerTurn(st)
							if spec := strings.TrimSpace(st.Params["GainsAbilitiesOf"]); spec != "" {
								gg.GainedFaces = e.gainedFacesForSpec(st, spec, id, o.Controller)
							}
							if spec := strings.TrimSpace(st.Params["GainsAbilitiesOfDefined"]); spec != "" {
								ctx := &effects.Ctx{Source: id, Controller: o.Controller}
								gg.GainedFaces = append(gg.GainedFaces, effects.GainedFacesOfDefined(e, ctx, spec)...)
							}
							if spec := strings.TrimSpace(st.Params["GainsTriggerAbsOf"]); spec != "" {
								gg.GainedTriggerFaces = e.gainedFacesForSpec(st, spec, id, o.Controller)
							}
							if len(gg.GainedFaces) > 0 || len(gg.GainedTriggerFaces) > 0 {
								out = append(out, gg)
							}
						}
						if rawName, ok := st.Param(cards.PKSetName); ok {
							if name, ok := resolveChosenName(rawName, o); ok {
								n := base
								n.Layer = LText
								n.SetName = name
								out = append(out, n)
							}
						}
						// RemoveType$ (the layer-4 reverse grant, CR 613.1d: the named
						// type words are removed from every object the static's Affected$
						// matches, before this same effect's AddTypes apply -- CR 205.1b's
						// "isn't a creature" family, the Theros god cycle) usually rides
						// an AddType$ (Luxior's equipped walker stops being a planeswalker
						// and becomes a creature) but STANDS ALONE on the devotion gods,
						// so the emission cannot gate on the AddType family.
						if st.HasParam(cards.PKAddType) || st.HasParam(cards.PKAddTypes) || st.HasParam(cards.PKAddAllCreatureTypes) || strings.TrimSpace(st.Params["RemoveType"]) != "" {
							ty := base
							ty.Layer = LType
							ty.AddTypes = statList(st, "AddTypes")
							if len(ty.AddTypes) == 0 {
								ty.AddTypes = statList(st, "AddType")
							}
							// AddAllCreatureTypes$ True (Maskwood Nexus's "creatures you
							// control are every creature type", the manland family) rides
							// the same LType emission as a flag, never a materialised
							// type list: typeCharacteristics appends the CreatureTypeWords
							// vocabulary for affected objects, so the answer stays live
							// and no non-creature word (Arcane/Alara/Ajani) can leak.
							ty.AddAllCreatureTypes = st.HasParam(cards.PKAddAllCreatureTypes)
							// AddType$ ChosenType (22 corpus files: Adaptive Automaton's
							// "CARDNAME is the chosen type in addition to its other
							// types" and its siblings): the VALUE is the static's host
							// object's own recorded "as this enters" choice, not a
							// literal type word — resolve it against the host's
							// ChosenType (staticContinuous re-runs once per event, so a
							// later Choose event re-derives the grant live). A host with
							// no recorded choice grants nothing: a chosen type this
							// build cannot read must not leak a literal "ChosenType"
							// type word onto the object.
							if resolved, ok := resolveChosenTypes(ty.AddTypes, o); ok {
								ty.AddTypes = resolved
							} else {
								ty.AddTypes = nil
							}
							// Duplicant's AddType$ ImprintedCreatureType reads the
							// last still-exiled creature card, not a literal type word.
							for i, word := range ty.AddTypes {
								if word != "ImprintedCreatureType" {
									continue
								}
								e.staticMemoStateRead = true
								ty.AddTypes = append(ty.AddTypes[:i], ty.AddTypes[i+1:]...)
								for j := len(o.Imprinted) - 1; j >= 0; j-- {
									im := e.G.Obj(o.Imprinted[j])
									if im == nil || im.Zone != state.ZExile || im.Face() == nil || !slices.Contains(im.Face().Types, "Creature") {
										continue
									}
									for _, subtype := range im.Face().Types {
										if effects.CreatureTypeWords(subtype) {
											ty.AddTypes = append(ty.AddTypes, subtype)
										}
									}
									break
								}
								break
							}
							// The strip flags ride the AddType emission (measured: every
							// corpus S: line carrying RemoveCardTypes$/RemoveCreatureTypes$
							// also carries AddType$): a strip-only static -- an AddType$
							// ChosenType the host has not resolved -- must still emit so
							// the strip is not silently dropped. RemoveType$ is the one
							// strip that DOES stand alone (the gods' devotion gate), so it
							// enters the emission condition too.
							ty.RemoveCardTypes = st.HasParam(cards.PKRemoveCardTypes)
							ty.RemoveCreatureTypes = st.HasParam(cards.PKRemoveCreatureTypes)
							ty.RemoveTypes = statList(st, "RemoveType")
							ty.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
							if len(ty.AddTypes) > 0 || ty.RemoveCardTypes || ty.RemoveCreatureTypes || ty.AddAllCreatureTypes || len(ty.RemoveTypes) > 0 {
								out = append(out, ty)
							}
						}
						// CR 613.1e colour static (Forge's SetColor$, Imprisoned in the Moon /
						// Kenrith's Transformation / Leyline of the Guildpact): the affected
						// object's colours are exactly the named set, REPLACING its printed
						// colours and every earlier layer-5 grant in timestamp order
						// (SetColor$ overwrites; it never extends). Its sibling AddColor$
						// ("...in addition to its other colors", Blade of the Oni / Angelic
						// Armaments / Deep Freeze) is the same layer-5 walk WITHOUT the
						// overwrite, so the object keeps its printed colours and gains the
						// named ones. Both share the colour-word parser: a named colour, a
						// comma list, "All" (every colour) and, for SetColor$, "Colorless"
						// (the empty set, a real overwrite to colourless). A value it cannot
						// fully parse declines through resolveChosenColors, which is the
						// layer-5 twin of resolveChosenTypes: a value naming the host's
						// recorded choice (the corpus's "ChosenColor" family -- Alloy
						// Golem, Shifting Sky, Shimmerwilds Growth's AddColor siblings)
						// resolves to the colour, while a host with NO recorded choice
						// (an unanswered ETB ask, a non-commander CDA carrier) fails
						// CLOSED: no effect is emitted and the object keeps its printed
						// colours, the same direction effAnimate's Colors$ gate takes.
						// No Note is emitted because this scan re-runs on every event;
						// a per-derivation Note would flood the log.
						if raw, isSet := st.Param(cards.PKSetColor); isSet {
							// A resolvable characteristic-defining self SetColor$ (the
							// Transguild Courier / Sphinx of the Guildpact "CARDNAME is
							// all colors", Ghostfire "CARDNAME is colorless" class) is
							// NOT emitted from this scan: a CDA works in EVERY zone
							// (CR 604.3/208.2), so effects.ColorMaskOf's base read now
							// applies the claim there and at the layer-5 base below,
							// and emitting here too would apply it twice -- the same
							// withholding the P/T CDA below takes. The shared
							// effects.CDASetColourClaimStatic classifier is what both
							// paths read, so they cannot disagree. A CDA the helper
							// rejects (the ChosenColor family) is NOT withheld: it
							// flows to resolveChosenColors, which resolves it against
							// the host's recorded choice or fails closed. A CDA that
							// narrows itself with AffectedZone$ would be a different
							// shape -- no corpus carrier carries one (measured), and a
							// CDA's zone width is every zone by CR 604.3 anyway.
							if _, isCDA, parsed := effects.CDASetColourClaimStatic(st); isCDA && parsed {
								// withheld: the base read applies it in every zone
							} else if cols, ok := resolveChosenColors(raw, o); ok {
								sc := base
								sc.Layer = LColor
								sc.AddColors = cols
								sc.OverwriteColors = true
								sc.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
								out = append(out, sc)
							}
						}
						if raw, isAdd := st.Param(cards.PKAddColor); isAdd || st.ParamStr(cards.PKAddColors) != "" {
							if !isAdd {
								raw = st.ParamStr(cards.PKAddColors)
							}
							if cols, ok := resolveChosenColors(raw, o); ok {
								sc := base
								sc.Layer = LColor
								sc.AddColors = cols
								sc.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
								out = append(out, sc)
							}
						}
						// CR 613.1f / 613.4b (Humility): a base-setting static runs in
						// layer 7b (SubSet), before the 7c modify a later Pump adds; and
						// a RemoveAllAbilities static is a layer-6 ability removal.
						// A P/T-setting characteristic-defining static (CharacteristicDefining$
						// True) is NOT emitted from this scan: cdaSetPT reads it directly
						// off the object's own face in derivedScalar, in EVERY zone
						// (CR 604.3/208.2 -- the layer-7a base this battlefield-only walk
						// cannot express), and emitting here too would apply the set
						// twice. A CDA whose value this build cannot resolve keeps
						// today's emission -- fail closed is the same degrade direction
						// every static gate takes.
						if st.HasParam(cards.PKSetPower) || st.HasParam(cards.PKSetToughness) {
							skip := false
							if strings.TrimSpace(st.ParamStr(cards.PKCharacteristicDefining)) != "" {
								e.staticMemoStateRead = true
								if _, _, hp, ht := e.cdaPTStatic(st, &effects.Ctx{Source: id, Controller: o.Controller, SVars: fc.SVars}); hp || ht {
									skip = true
								}
							}
							if !skip {
								set := base
								set.Layer, set.Sub = LPT, SubSet
								if strings.EqualFold(st.ParamStr(cards.PKCharacteristicDefining), "true") {
									set.Sub = SubCDA
								}
								set.SetPowerExpr = st.ParamStr(cards.PKSetPower)
								set.SetToughnessExpr = st.ParamStr(cards.PKSetToughness)
								set.SetPowerPresent = st.HasParam(cards.PKSetPower)
								set.SetToughnessPresent = st.HasParam(cards.PKSetToughness)
								set.StaticSet = true
								set.HasSet = true
								out = append(out, set)
							}
						}
						if st.HasParam(cards.PKRemoveAllAbilities) {
							ra := base
							ra.Layer = LAbilities
							ra.RemoveAbilities = true
							out = append(out, ra)
						}
						// A may-play-from-zone grant (M2d?): the "You may play lands from
						// your graveyard" static (Conduit of Worlds, Crucible of Worlds,
						// Ramunap Excavator, ...). It changes no characteristic, so it is
						// NOT a layer effect and is carried as a rules-mod on the effect
						// itself (MayPlay + AffectedZone) rather than as a layer mark;
						// rules/legal.go's may-play walks consult it. The implemented
						// shape is the unconditional MayPlay$ True grant plus its two
						// readable riders (MayPlayIgnoreColor$ -- mana as any colour --
						// and MayPlayLimit$ 1, the once-per-turn cap); the
						// mayPlayShape guard rejects a richer grant (MayPlayIgnoreType$/
						// MayPlayWithoutManaCost$/MayPlayText$, Condition$/
						// ValidAfterStack$/Secondary$ qualifiers) so it fails closed
						// (MayPlay stays false) rather than being silently over-applied
						// against the ordinary LandsPlayed limit. Expiry is the ordinary
						// source-leaves rule (CR 611.3b) via active()'s battlefield scan.
						if st.HasParam(cards.PKMayPlay) && mayPlayGrant(st) {
							mp := base
							mp.MayPlay = true
							mp.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
							mp.MayPlayIgnoreColor, mp.MayPlayIgnoreType, mp.MayPlayLimit, mp.MayPlayPlayerTurn, _ = effects.MayPlayStaticParams(st.Params)
							out = append(out, mp)
						}
						// An additional-land-drops grant (Azusa, Lost but Seeking's "You
						// may play two additional lands on each of your turns", Oracle of
						// Mul Daya, Exploration, Icetill Explorer). Like the may-play
						// grant it changes no characteristic, so it is NOT a layer effect
						// and is carried as a rules-mod on the effect itself
						// (AdjustLandPlays); rules/legal.go's land-play gates consult it
						// through Engine.adjustLandPlays. The implemented shape is the
						// plain one -- a literal positive integer value and only display
						// metadata around it; the Affects spec is evaluated at the gate
						// with MatchesPlayerSpecFrom, whose own fail-closed rule (an
						// unhandled qualifier matches nobody) rejects the richer
						// Affected$ forms. A richer VALUE or rider fails closed here: an
						// AdjustLandPlays$ Unlimited/Z (Fastbond, an X-driven grant)
						// must not silently become "one more", and an IsPresent$/
						// Secondary$ qualifier changes when the grant lives. The explicit
						// whitelist, rather than a blacklist of currently-known gating
						// keys, means a newly encountered semantic parameter also fails
						// closed. Expiry is the ordinary source-leaves rule (CR 611.3b)
						// via active()'s battlefield scan; the turn scoping ("each of
						// your turns") is the offer gate itself -- a play_land option is
						// only offered to the active player in a main phase -- and the
						// per-turn reset stays events' TurnChange LandsPlayed = 0.
						if n, ok := adjustLandPlaysGrantOf(st); ok {
							al := base
							al.AdjustLandPlays = n
							out = append(out, al)
						}
						// --- the four static-grant kinds the parameter census named
						// (task inbox-paramcensus-static-grant-misc). Each reads its
						// own key and fails closed on a shape it cannot evaluate, like
						// every grant branch above. ---

						// A static-that-grants-a-static (Exploration Broodship's
						// "STATION 3+"): AddStaticAbility$ names an SVar on the granting
						// face whose body is itself a Mode$ Continuous static, granted
						// for exactly as long as the OUTER static is live (its gate has
						// already run above, and the inner static's own gate runs when
						// its queue entry is emitted -- the same fail-closed rule). The
						// grant's HOST is the object the OUTER Affected$ spec matches
						// (the scan rebuilds per event, so the counters move, the grant
						// follows); the inner static's own Affected$ scopes what IT
						// affects, resolved against the host -- the Broodship's STATION
						// 3+ grant is an AdjustLandPlays$ 1 to You, and You is the
						// host's controller. cards.ParseStaticLines gives the body the
						// same shape a printed S: line would have, so EVERY grant branch
						// above applies to the inner static unchanged. A body this
						// parser refuses, one whose mode is not Continuous, or a host
						// the outer spec no longer matches, grants nothing.
						if name := strings.TrimSpace(st.ParamStr(cards.PKAddStaticAbility)); name != "" && w.depth == 0 {
							e.staticMemoStateRead = true
							if inners, ok := cards.ParseStaticLines(fc.SVars[name]); ok {
								for _, inner := range inners {
									if inner.Mode == "Continuous" &&
										e.matchesSpecFrom(affects, id, o.Controller, id) {
										grantQueue = append(grantQueue, staticWork{st: inner, depth: w.depth + 1})
									}
									// A granted COST-MODIFIER static (Jubilant
									// Skybonder's "Creatures you control with
									// flying have 'Spells your opponents cast that
									// target this creature cost {2} more'",
									// Acolyte of Bahamut's commander grant): the
									// objects the OUTER Affected$ matches each HAVE
									// the static while the outer one is live. It
									// rides this scan as a granted cost static
									// (state.ContinuousEffect.CostStaticGranted);
									// rules' appendGrantedCostStatic binds it to
									// every matching host at collection time, the
									// same registry route the Animate and
									// CopyPermanent grants take. The Effect route's
									// whitelist gates it: an unread scoping key
									// grants nothing rather than a blanket modifier.
									if effects.IsGrantableCostStaticMode(inner.Mode) && effects.CostStaticParamsReadable(inner.Params) {
										cg := base
										cg.CostStaticMode = inner.Mode
										cg.CostStaticParams = inner.Params
										cg.CostStaticSVars = fc.SVars
										cg.CostStaticGranted = true
										cg.AffectedZone = strings.TrimSpace(st.ParamStr(cards.PKAffectedZone))
										out = append(out, cg)
									}
								}
							}
						}
						// A triggered-ability grant (Hearthhull's "STATION 8+ Whenever
						// you sacrifice a land"): AddTrigger$ names an SVar on the
						// granting face whose body is a T:-shaped trigger; the objects
						// the static's Affected$ matches gain it while the static is
						// live. cards.ParseTriggerLine gives the body the same shape a
						// printed T: line would have; rules/trigger_match.go's granted-
						// trigger walk (checkGrantedStaticTriggers, the granted-Ward/
						// granted-Dethrone precedent) matches it like any other trigger
						// and links its Execute$ from the GRANTING face's own SVar
						// table -- the table events.Apply's GrantTriggerPush resolves
						// from (the grantor rides the event's Amount), so the live
						// queue and a replayed one mint the same stack object. A
						// self-grant degenerates to the affected object; a body that
						// fails to parse grants nothing.
						if raw := strings.TrimSpace(st.ParamStr(cards.PKAddTrigger)); raw != "" {
							// The value may name SEVERAL SVar triggers joined by Forge's
							// " & " separator (Mirror Shield's TrigBlocks &
							// TrigBecomeBlocked). Split through the ONE exported grammar
							// helper the K:Class: grant path also uses -- reading the
							// whole value as one name would look up a nil SVar and
							// silently grant nothing. Order is the value's left-to-right
							// order, so replay is deterministic; each name still fails
							// closed on its own (a missing or unparseable body grants
							// nothing, and no longer suppresses its valid sibling).
							for _, name := range cards.SplitGrantNames(raw) {
								if t, ok := cards.ParseTriggerLine(fc.SVars[name]); ok {
									gt := base
									gt.AddTrigger = &t
									out = append(out, gt)
								}
							}
						}
						// A named-variable grant (Sword of Fire and Ice): AddSVar$ names an SVar
						// on the granting face whose body is Forge's
						// "SVar:<Name>:<Value>" grant shape -- the affected object GAINS
						// that named variable while the static is live. The corpus's
						// granted SVars are AI-evaluation hints (AE, AITap,
						// MustBeBlocked) no rules consumer reads; the engine records the
						// grant and resolves it through Engine.GrantedSVar, the lookup a
						// later CheckSVar$-style consumer of the affected object's
						// variables reads. A body in any other shape grants nothing.
						if raw := strings.TrimSpace(st.ParamStr(cards.PKAddSVar)); raw != "" {
							if n, v, ok := parseSVarGrant(fc.SVars[raw]); ok {
								gv := base
								gv.AddSVars = map[string]string{n: v}
								out = append(out, gv)
							}
						}
						// A look-permission grant (Oracle of Mul Daya): MayLookAt$ says
						// the affected player may look at the object the Affected$ spec
						// matches -- in the corpus always the top card of the
						// controller's own library (Affected$ Card.TopLibrary+YouCtrl,
						// AffectedZone$ Library). The value names WHO may look:
						// You/Player/True are all the static's controller in every
						// corpus shape (73/34/1 raw lines at the pin); anything else
						// fails closed. Consumed by Engine.MayLookAtLibraryTop, the
						// view's reveal of that top card.
						if raw := strings.TrimSpace(st.ParamStr(cards.PKMayLookAt)); raw != "" {
							if strings.EqualFold(raw, "You") || strings.EqualFold(raw, "Player") || strings.EqualFold(raw, "True") {
								lv := base
								lv.MayLookAt = true
								out = append(out, lv)
							}
						}
						// A control-change static (Mind Control's "You control enchanted
						// creature", Fealty to the Realm's "The monarch controls
						// enchanted creature"): GainControl$ on a Mode$ Continuous static
						// hands every object the Affected$ spec matches to the player the
						// value names, for exactly as long as the static is live. Like
						// MayPlay it changes no characteristic, so it is carried as a
						// rules-mod (GainControl) and realized by rules'
						// reconcileControlStatics (rules/control_static.go): that pass
						// registers a real tracked control grant and emits
						// events.ControlChange only where the object's controller
						// actually differs, and the tracked grant's liveness (grantEnded)
						// is this scan's own output, so an ended static -- source left,
						// gate flipped, Aura moved bearers, named player changed -- hands
						// the bearer back through expireControl's ordinary Previous
						// chain. The VALUE itself is not validated here (the scan cannot
						// resolve players): resolution happens in the reconcile, where a
						// value that names nobody -- or several players -- yields no
						// grant, the fail-closed direction. The static's "as long as"
						// gate (IsPresent$/CheckSVar$) already ran above for every
						// branch. Measured corpus population (GNU /usr/bin/grep): 42 raw
						// S:Mode$ Continuous lines carrying GainControl$, every one
						// shaped Mode/Affected/GainControl/Description with Affected$
						// *.EnchantedBy and the value You (41) or Player.isMonarch (1,
						// Fealty to the Realm); none is in any repo deck, so the golden
						// heads and the ratchet are untouched by construction.
						if raw := strings.TrimSpace(st.ParamStr(cards.PKGainControl)); raw != "" {
							gc := base
							gc.GainControl = raw
							out = append(out, gc)
						}
					}
					e.staticQueueBuf = grantQueue
				}
			}
		}
	}
	if len(out) < len(dst) {
		clear(dst[len(out):])
	}
	return out
}

// active returns the effects that still exist, sorted into CR 613 order:
// layer, then sublayer, then timestamp. Ties within a (layer, sublayer,
// timestamp) triple — two effects created in the same AddContinuous batch
// without distinct timestamps — keep the order they were registered in,
// because sort.SliceStable never reorders equal elements; that registration
// order is itself deterministic (single goroutine, no map iteration), so the
// whole sort is reproducible run to run and safe for replay.
//
// Effects whose source has left the battlefield are dropped, which is what
// makes a lord's static bonus vanish the instant the lord dies. An effect
// marked UntilEOT is different: it is a one-shot pump that already resolved
// (Giant Growth), so it outlives its source and is only removed by
// EndOfTurnCleanup.
func (e *Engine) active() []ContinuousEffect {
	// The exact-hit test below, hoisted into this inlinable wrapper: the hit
	// is by far the common call (every Derived and restriction read asks),
	// and on it activeBuild's depth bump and deferred restore bracket no
	// work at all, so returning here is the same answer at no call cost.
	if e.activeEpoch == len(e.L.Events) && e.activeVersion == e.continuousVersion && e.activeStaticSeq == e.staticBuildSeq {
		return e.activeBuf
	}
	return e.activeBuild()
}

// activeBuild is active() past its exact-hit test: the layer-inert re-stamp
// or a full rebuild.
func (e *Engine) activeBuild() []ContinuousEffect {
	e.activeDepth++
	defer func() { e.activeDepth-- }()
	// Cached hit: derived only reads the returned slice, never mutates it, so
	// every Derived call of a board build shares this one sorted list. The
	// key is the log head plus the continuous-mutation version; a mismatch
	// means something the list depends on changed and the cache is stale. The
	// static memo's build is part of the key too: staticControlWants refreshes
	// staticContinuous outside active(), so an unmoved log head and
	// continuousVersion do not imply an unmoved static list.
	if e.activeEpoch == len(e.L.Events) && e.activeVersion == e.continuousVersion && e.activeStaticSeq == e.staticBuildSeq {
		return e.activeBuf
	}
	// Layer-inert reuse (layercache.go): the log moved only by events whose
	// Apply writes nothing active() reads, and neither e.continuous nor the
	// object table moved, so the cached list is still the answer. Only a
	// depth-1 call adopts it (a re-entrant build never owns activeBuf). The
	// static memo's build must still match for the same reason as above.
	if e.activeDepth == 1 && e.activeVersion == e.continuousVersion && e.activeObjs == len(e.G.Objs) && e.activeStaticSeq == e.staticBuildSeq && e.layerInertSince(e.activeEpoch) {
		if layerInertVerify {
			e.verifyInertActive()
		}
		e.activeEpoch = len(e.L.Events)
		return e.activeBuf
	}
	e.activeEpoch = len(e.L.Events)
	e.activeVersion = e.continuousVersion
	e.activeObjs = len(e.G.Objs)
	e.activeBuildSeq++
	// Double-buffered (derived_transparent.go): the build writes the other
	// array, so the previous list survives intact for the transparency
	// comparison below.
	buf := e.activeBufAlt[:0]
	if e.activeDepth > 1 {
		// Re-entrant (a nested Derived mid-rebuild): own a private list rather
		// than overwrite the outer call's result mid-range. Same guard Task A2
		// uses for forEachObject. (This path is effectively unreachable — a
		// Derived call never emits an event, so the epoch cannot move mid-
		// range — but it keeps the buffer discipline airtight.
		buf = nil
	}
	// Duration-honouring expiry. A Permanent one-shot lasts until the end of
	// the game (CR 611.2a) regardless of where its source went; an
	// UntilEndOfCombat one-shot lasts only through the combat phase (CR
	// 511.2), so it is kept while the step is a combat step and dropped the
	// moment play moves past end combat. These take precedence over the
	// UntilEOT/source-leaves rules below, which model the other two
	// lifetimes.
	//
	// The list is assembled as pointers to its sources first (src), sorted
	// there when it is not already in order, and copied into buf once:
	// sorting the ~1 KB ContinuousEffect values themselves copied two of them
	// per comparison. A stable sort of the same sequence under the same order
	// is the same permutation, so buf is exactly what sorting it in place
	// produced. A re-entrant build owns a private src, as it owns buf.
	var src []*ContinuousEffect
	if e.activeDepth <= 1 {
		src = e.activeSrc[:0]
	}
	for i := range e.continuous {
		if e.continuousLive(&e.continuous[i]) {
			src = append(src, &e.continuous[i])
		}
	}
	// The static-derived effects come from the memoized scan (see
	// Engine.staticContinuous): refreshed once per emitted event, not per
	// Derived call, so a board-wide scan does not dominate the hottest path.
	// A version-only rebuild (EndOfTurnCleanup dropping an UntilEOT pump) is a
	// subset of the rebuild condition that leaves the battlefield permanent
	// set, and therefore the static memo, untouched — so the two are checked
	// independently exactly as before.
	e.refreshStaticContinuous()
	// Record the static memo build this buffer is built from, after the
	// refresh: the hit paths above require it, so an out-of-band refresh
	// (staticControlWants) invalidates this buffer on the next active() call.
	e.activeStaticSeq = e.staticBuildSeq
	for i := range e.staticContinuous {
		src = append(src, &e.staticContinuous[i])
	}
	// A rebuild's list is usually already in CR 613 order (registration and
	// the static scan both run in timestamp order), and a stable sort of a
	// sorted list is the identity, so test that first.
	if !continuousPtrsSorted(src) {
		slices.SortStableFunc(src, compareContinuousPtr)
	}
	for _, p := range src {
		buf = append(buf, *p)
	}
	if e.activeDepth <= 1 {
		clear(src)
		e.activeSrc = src[:0]
	}
	if e.activeDepth <= 1 {
		// Keep the grown, sorted buffer on the Engine for the next build or
		// cache hit; a re-entrant build's private buffer is discarded on return.
		if !e.derivedRebuildTransparent(e.activeBuf, buf) {
			e.derivedSeq++
		}
		e.derivedNoteBuild()
		e.activeBufAlt, e.activeBuf = e.activeBuf, buf
		e.activeKWHeads = appendKWHeads(e.activeKWHeads[:0], buf)
	} else {
		e.derivedSeq++
	}
	return buf
}

// continuousPtrsSorted reports whether active()'s source pointers are
// already non-decreasing under compareContinuousPtr.
func continuousPtrsSorted(src []*ContinuousEffect) bool {
	for i := 1; i < len(src); i++ {
		if compareContinuousPtr(src[i-1], src[i]) > 0 {
			return false
		}
	}
	return true
}

// compareContinuousPtr is active()'s CR 613 order: layer, then sublayer,
// then timestamp, then the layer-6 removal-before-grant tie-break below.
func compareContinuousPtr(a, b *ContinuousEffect) int {
	if a.Layer != b.Layer {
		if a.Layer < b.Layer {
			return -1
		}
		return 1
	}
	if a.Sub != b.Sub {
		if a.Sub < b.Sub {
			return -1
		}
		return 1
	}
	if a.Timestamp != b.Timestamp {
		if a.Timestamp < b.Timestamp {
			return -1
		}
		return 1
	}
	// A full tie inside layer 6 between an ability/keyword-removing effect
	// and an ability-granting one applies removal first: static lines that
	// strip and grant together follow CR 613.1f's removal-then-grant reading
	// of a simultaneous pair. Without this tie-break the stable sort keeps
	// scanner emission order and may wipe the grant. Timestamps still
	// dominate: a LATER removal (Humility entering after) still wipes an
	// earlier grant.
	aRemovesKeywords := a.RemoveAbilities || len(a.RemoveKeywords) > 0
	bRemovesKeywords := b.RemoveAbilities || len(b.RemoveKeywords) > 0
	if a.Layer == LAbilities && aRemovesKeywords != bRemovesKeywords {
		if aRemovesKeywords {
			return -1
		}
		return 1
	}
	return 0
}
