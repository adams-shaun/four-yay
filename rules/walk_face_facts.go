package rules

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// walkFaceFacts are the priority offer walk's per-face verdicts: which
// zones a face's printed activated abilities can be offered from at all, and
// whether it carries any printed mana ability. Each is a pure function of the
// face's own ability list (the same Params the walk's per-ability skips read),
// computed once per configured face when the compiledText is built, so the
// walk can pass over an object none of whose abilities could survive the
// first skips -- the common case for every creature in a hand or graveyard
// and every land's non-mana sweep -- without walking its ability list.
//
// Every bit only ever SKIPS work whose result is provably empty; a face the
// table does not hold (a runtime copy face, an unconfigured card) takes the
// full walk. In the rules test binary walkSkipVerify runs the skipped work
// anyway and panics if it would have produced anything.
type walkFaceFacts struct {
	face *cards.Face
	// abFirst/abLen guard the facts against a face whose ability list was
	// replaced or grown after the table was built (the cardText shape
	// discipline): a mismatch reads as a miss.
	abFirst **cards.SA
	abLen   int
	// abZones bit z (z < 32) is set when some ability survives the offer
	// loop's kind, zone and mana-ness skips in zone z: an AB$ whose
	// ActivationZone$ admits z and which is not a (non-loyalty) mana ability.
	abZones uint32
	// abZonesActivator is abZones restricted to abilities with a non-blank
	// Activator$ -- the only ones a player other than the source's controller
	// can activate (activatorAllows' blank arm is the controller test).
	abZonesActivator uint32
	// mana: the face has a printed mana ability (ManaAbilities() non-empty)
	// or a ManaReflected ability -- the only printed members the mana walk
	// (appendAvailableManaAbilitiesGate) can return.
	mana bool
	// manaReflected: some ability's API is ManaReflected.
	manaReflected bool
	// manaAllTap: every printed mana ability's Cost$ includes {T}, so none
	// is payable from a tapped source (activationTapCostUnavailable).
	manaAllTap bool
	// manaControllerOnly: every printed mana and ManaReflected ability has a
	// blank Activator$, so only the source's controller can activate one.
	manaControllerOnly bool
	// kwGranted: some printed keyword line's head is one of grantedKWHeads
	// (KeywordLinesHaveHead), guarded by kwFirst/kwLen like the abilities.
	kwGranted bool
	kwFirst   *string
	kwLen     int
	// ph is printedHeadsOf(f), under the same keyword guard.
	ph printedHeads
	// altCosts is the alternative-cost keyword family's compiled printed cost
	// on this face (rules/altcast_modes.go): altCastModes[i]'s K: parameter,
	// parsed once here and frozen, so the offer walk, the command-zone offer,
	// the potential-plan pricing and the charge read altCastMode.faceCost's
	// sidecar arm instead of reparsing the parameter on every offer. A pure
	// function of the keyword list, so it rides the keywordsCurrent guard.
	//
	// It is SPARSE: only the PRESENT rows are stored, in row order, because
	// altFaceCost embeds an 824-byte Cost and the overwhelming majority of
	// faces carry no alt-cost keyword. altCostMask bit i marks that
	// altCastModes[i] is present; the position of row id in altCosts is the
	// population count of the mask's lower bits, so a face with none
	// allocates nothing and pays only the 1-byte mask.
	altCosts    []altFaceCost
	altCostMask uint8
	// impendingCount is the compiled N from K:Impending:N:cost, read under
	// the same keyword-list identity guard as altCosts.
	impendingCount int32
	// name and the option labels the walk offers for this face, built once
	// so a walk shares them instead of concatenating per option (Go strings
	// are immutable; a shared label is the same value the concatenation
	// produces). A label is served only while f.Name still equals name.
	name                            string
	castLabel, playLabel, manaLabel string
	// scan is the face's text-scan verdicts (computeFaceScan), served by
	// faceScanHas instead of a per-engine memo every clone rebuilt.
	scan faceScan
	// trigZones is faceTriggerZones' answer (computeFaceTriggerZones),
	// guarded by trigFirst/trigLen like the abilities.
	trigZones uint8
	// trigSig / trigSigOther / trigLookBack are computeFaceTrigSigs' and
	// computeFaceLookBackZones' answers (trigger_kinds.go), under the same
	// guard.
	trigSig      trigSig
	trigSigOther trigSig
	trigLookBack uint8
	// grantsTrig is faceGrantsTriggers' answer (trigger_grantfree.go).
	grantsTrig bool
	trigFirst  *cards.Trigger
	trigLen    int
	// statFirst/statLen, replFirst/replLen and svars/svarsLen record the
	// face's static, replacement and SVar lists the facts were computed
	// over: with the guards above they make the facts fully current
	// (fullyCurrent), the bar an entry published on the face must clear.
	statFirst *cards.Static
	statLen   int
	replFirst *cards.Repl
	replLen   int
	svars     unsafe.Pointer
	svarsLen  int
	// staticOn / staticOff / mayPlay are the face's object-class bits
	// (walk_objclass.go): faceStaticHotOn, faceStaticHotOff and
	// faceMayPlayHot, read only while the facts are fullyCurrent.
	staticOn, staticOff, mayPlay bool
}

// verifyFresh panics when a field of ff a reader may use differs from a
// recompute over f: the keyword, trigger and label halves are compared only
// while their own guards (keywordsCurrent, triggersCurrent, the name test)
// pass, since every reader of a guarded half checks its guard first, and
// the list-identity guards themselves are not facts.
func (ff *walkFaceFacts) verifyFresh(f *cards.Face) {
	fresh := computeWalkFaceFacts(f)
	got := *ff
	if !ff.keywordsCurrent(f) {
		got.kwGranted, got.kwFirst, got.kwLen, got.ph = fresh.kwGranted, fresh.kwFirst, fresh.kwLen, fresh.ph
		got.altCosts, got.altCostMask = fresh.altCosts, fresh.altCostMask
		got.impendingCount = fresh.impendingCount
	}
	if !ff.triggersCurrent(f) {
		got.trigZones, got.trigSig, got.trigSigOther, got.trigLookBack = fresh.trigZones, fresh.trigSig, fresh.trigSigOther, fresh.trigLookBack
		got.grantsTrig, got.trigFirst, got.trigLen = fresh.grantsTrig, fresh.trigFirst, fresh.trigLen
	}
	if ff.name != f.Name {
		got.name, got.castLabel, got.playLabel, got.manaLabel = fresh.name, fresh.castLabel, fresh.playLabel, fresh.manaLabel
	}
	if !ff.fullyCurrent(f) {
		got.scan, got.staticOn, got.staticOff, got.mayPlay = fresh.scan, fresh.staticOn, fresh.staticOff, fresh.mayPlay
	}
	got.statFirst, got.statLen, got.replFirst, got.replLen = fresh.statFirst, fresh.statLen, fresh.replFirst, fresh.replLen
	got.svars, got.svarsLen = fresh.svars, fresh.svarsLen
	// DeepEqual, not ==: altCosts holds Cost slices, which are not
	// comparable. This verify path runs only in the rules test binary.
	if !reflect.DeepEqual(got, fresh) {
		panic(fmt.Sprintf("rules: walk face facts for %q are stale (%+v vs %+v)", f.Name, *ff, fresh))
	}
}

// svarsIdentity is the identity of f's SVar map (its runtime header).
func svarsIdentity(f *cards.Face) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&f.SVars))
}

// fullyCurrent reports whether ff was computed for f over every list f
// holds now (and its name): an entry published on a face by any
// configuration's table is served only then, so a face edited after another
// configuration published it reads as absent.
func (ff *walkFaceFacts) fullyCurrent(f *cards.Face) bool {
	return ff.currentFor(f) && ff.keywordsCurrent(f) && ff.triggersCurrent(f) &&
		ff.statLen == len(f.Statics) && (ff.statLen == 0 || ff.statFirst == &f.Statics[0]) &&
		ff.replLen == len(f.Repls) && (ff.replLen == 0 || ff.replFirst == &f.Repls[0]) &&
		ff.svarsLen == len(f.SVars) && ff.svars == svarsIdentity(f) && ff.name == f.Name
}

// keywordsCurrent reports whether the facts' keyword half was computed over
// f's current keyword list.
func (ff *walkFaceFacts) keywordsCurrent(f *cards.Face) bool {
	return ff.kwLen == len(f.Keywords) && (ff.kwLen == 0 || ff.kwFirst == &f.Keywords[0])
}

// walkSkipVerify: see derivedMemoVerify. Set by the rules test binary.
var walkSkipVerify = derivedMemoVerifyFlag != ""

func computeWalkFaceFacts(f *cards.Face) walkFaceFacts {
	ff := walkFaceFacts{face: f, abLen: len(f.Abilities), kwLen: len(f.Keywords), name: f.Name,
		castLabel: "Cast " + f.Name, playLabel: "Play " + f.Name, manaLabel: "Activate " + f.Name + " for mana"}
	if len(f.Abilities) > 0 {
		ff.abFirst = &f.Abilities[0]
	}
	ff.scan = computeFaceScan(f)
	ff.trigZones, ff.trigLen = computeFaceTriggerZones(f, phaseSpecValid), len(f.Triggers)
	ff.trigSig, ff.trigSigOther = computeFaceTrigSigs(f, phaseSpecValid)
	ff.trigLookBack = computeFaceLookBackZones(f)
	ff.grantsTrig = faceGrantsTriggers(f)
	if len(f.Triggers) > 0 {
		ff.trigFirst = &f.Triggers[0]
	}
	ff.statLen, ff.replLen, ff.svarsLen, ff.svars = len(f.Statics), len(f.Repls), len(f.SVars), svarsIdentity(f)
	ff.staticOn, ff.staticOff, ff.mayPlay = faceStaticHotOn(f), faceStaticHotOff(f), faceMayPlayHot(f)
	if len(f.Statics) > 0 {
		ff.statFirst = &f.Statics[0]
	}
	if len(f.Repls) > 0 {
		ff.replFirst = &f.Repls[0]
	}
	ff.ph = printedHeadsOf(f)
	ff.impendingCount = impendingCount(f)
	for i := range altCastModes {
		c, ok := altCastModes[i].faceCostRaw(f)
		if !ok {
			continue
		}
		ff.altCostMask |= 1 << altCastID(i)
		ff.altCosts = append(ff.altCosts, altFaceCost{cost: freezeCost(c), ok: true})
	}
	if len(f.Keywords) > 0 {
		ff.kwFirst = &f.Keywords[0]
		for _, h := range grantedKWHeads {
			if f.KeywordLinesHaveHead(h.S, h.ID) {
				ff.kwGranted = true
			}
		}
	}
	for _, ab := range f.Abilities {
		if ab == nil {
			continue
		}
		if ab.API == "ManaReflected" {
			ff.manaReflected = true
		}
		if ab.Kind != "AB" {
			continue
		}
		if cards.IsManaAbilityAPI(ab.API) && !loyaltyAbilityText(ab) {
			continue
		}
		m := abilityZoneMask(ab)
		ff.abZones |= m
		if !blankActivator(ab) {
			ff.abZonesActivator |= m
		}
	}
	mas := f.ManaAbilities()
	ff.mana = ff.manaReflected || len(mas) > 0
	ff.manaAllTap, ff.manaControllerOnly = true, true
	for _, ma := range mas {
		if ma == nil {
			ff.manaAllTap, ff.manaControllerOnly = false, false
			break
		}
		if !ParseCost(ma.ParamStr(cards.PKCost)).Tap {
			ff.manaAllTap = false
		}
		if !blankActivator(ma) {
			ff.manaControllerOnly = false
		}
	}
	for _, ab := range f.Abilities {
		if ab != nil && ab.API == "ManaReflected" && !blankActivator(ab) {
			ff.manaControllerOnly = false
		}
	}
	return ff
}

// loyaltyAbilityText is isLoyaltyAbility without an engine: the cost text is
// parsed directly rather than read through the engine's compiled-cost table,
// which holds the same frozen parse.
func loyaltyAbilityText(ab *cards.SA) bool {
	raw := ab.ParamStr(cards.PKCost)
	if !containsLoyaltyFold(raw) {
		return isLoyaltyMarked(ab)
	}
	c := ParseCost(raw)
	return isLoyaltyAbilityRef(ab, &c)
}

func blankActivator(ab *cards.SA) bool {
	v, ok := ab.Param(cards.PKActivator)
	if !ok {
		return true
	}
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case ' ', '\t', '\n', '\r', '\v', '\f':
		default:
			return false
		}
	}
	return true
}

// zoneBit reports whether mask carries zone z; a zone past the mask's width
// answers true (no claim is made about it).
func zoneBit(mask uint32, z state.Zone) bool {
	return z >= 32 || mask&(1<<z) != 0
}

// walkFaceTable is an open-addressed, read-only table of walkFaceFacts keyed
// by face pointer, built once per compiledText. The slot a face lands in
// depends on its address, but a lookup's ANSWER does not (a hit is a pointer
// match, a miss is the slow path), so nothing observable depends on layout.
type walkFaceTable struct {
	slots []walkFaceFacts
	mask  uintptr
}

func faceSlotHash(f *cards.Face) uintptr {
	h := uint64(uintptr(unsafe.Pointer(f)) >> 4)
	h ^= h >> 29
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 32
	return uintptr(h)
}

func buildWalkFaceTable(faces []*cards.Face) walkFaceTable {
	n := 16
	for n < 2*len(faces) {
		n *= 2
	}
	t := walkFaceTable{slots: make([]walkFaceFacts, n), mask: uintptr(n - 1)}
	for _, f := range faces {
		if f == nil {
			continue
		}
		i := faceSlotHash(f) & t.mask
		for t.slots[i].face != nil && t.slots[i].face != f {
			i = (i + 1) & t.mask
		}
		if t.slots[i].face == nil {
			t.slots[i] = computeWalkFaceFacts(f)
		}
	}
	// Publish each entry on its face (cards.ExtSlot), so a read follows the
	// face's pointer instead of hashing it. The first table to publish a
	// face wins, and every table's entry for it is the same pure function of
	// the face. The published entry is a copy, not a pointer into t.slots:
	// the slot is write-once and lives as long as the registry, so an
	// interior pointer kept the WHOLE first table (every configured face,
	// token scripts included) alive for every face it was first for. An
	// embedder building one configuration per card (the compliance gate,
	// cardfuzz) grew ~1 MB per distinct card: 24 sets in one process went
	// 0.6 GB -> 3.3 GB, the full corpus past 15 GB.
	for i := range t.slots {
		if f := t.slots[i].face; f != nil && f.ExtSlot().Load() == nil {
			ff := t.slots[i]
			f.ExtSlot().Store(unsafe.Pointer(&ff))
		}
	}
	return t
}

// currentFor reports whether ff holds f's facts over f's current ability
// list (the table lookup's guard).
func (ff *walkFaceFacts) currentFor(f *cards.Face) bool {
	return ff.face == f && ff.abLen == len(f.Abilities) && (ff.abLen == 0 || ff.abFirst == &f.Abilities[0])
}

// lookup returns f's facts, or nil when the table does not hold a current
// entry for it.
func (t *walkFaceTable) lookup(f *cards.Face) *walkFaceFacts {
	if len(t.slots) == 0 || f == nil {
		return nil
	}
	i := faceSlotHash(f) & t.mask
	for {
		s := &t.slots[i]
		if s.face == f {
			if s.abLen != len(f.Abilities) || (s.abLen > 0 && s.abFirst != &f.Abilities[0]) {
				return nil
			}
			if walkSkipVerify {
				s.verifyFresh(f)
			}
			return s
		}
		if s.face == nil {
			return nil
		}
		i = (i + 1) & t.mask
	}
}

// triggersCurrent reports whether the facts' trigger half was computed over
// f's current trigger list.
func (ff *walkFaceFacts) triggersCurrent(f *cards.Face) bool {
	return ff.trigLen == len(f.Triggers) && (ff.trigLen == 0 || ff.trigFirst == &f.Triggers[0])
}

// walkFaceFactsOf returns f's facts (nil without a compiledText): the
// entry published on the face itself when it is f's own and current over
// f's ability list (a by-value face copy shares its original's slot and
// fails the identity test), else the engine's table lookup -- the table's
// own guard. Every other half of the facts is read under its own guard
// (keywordsCurrent, triggersCurrent, the name test, fullyCurrent for the
// text scan and the object-class bits), so an entry another configuration
// published before a face was edited is never read stale.
func (e *Engine) walkFaceFactsOf(f *cards.Face) *walkFaceFacts {
	if e == nil || e.compiledText == nil || f == nil {
		return nil
	}
	if p := f.ExtSlot().Load(); p != nil {
		if ff := (*walkFaceFacts)(p); ff.currentFor(f) {
			if walkSkipVerify {
				ff.verifyFresh(f)
			}
			return ff
		}
	}
	return e.compiledText.faces.lookup(f)
}

// printedHeads is printedHeadsOf(f), read from the face facts when their
// keyword half is current.
func (w *legalWalk) printedHeads(f *cards.Face) printedHeads {
	if ff := w.e.walkFaceFactsOf(f); ff != nil && ff.keywordsCurrent(f) {
		return ff.ph
	}
	return printedHeadsOf(f)
}

// castLabel is "Cast " + f.Name, shared from the face facts when current.
func (w *legalWalk) castLabel(f *cards.Face) string {
	if ff := w.e.walkFaceFactsOf(f); ff != nil && ff.name == f.Name {
		return ff.castLabel
	}
	return "Cast " + f.Name
}

// playLabel is "Play " + f.Name, shared from the face facts when current.
func (w *legalWalk) playLabel(f *cards.Face) string {
	if ff := w.e.walkFaceFactsOf(f); ff != nil && ff.name == f.Name {
		return ff.playLabel
	}
	return "Play " + f.Name
}

// manaLabel is manaActivateLabel(f.Name), shared from the face facts when
// current.
func (w *legalWalk) manaLabel(f *cards.Face) string {
	if ff := w.e.walkFaceFactsOf(f); ff != nil && ff.name == f.Name {
		return ff.manaLabel
	}
	return w.e.manaActivateLabel(f.Name)
}
