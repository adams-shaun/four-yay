package rules

import (
	"fmt"
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
	// flash is whether the face carries its OWN S:Mode$ CastWithFlash static
	// (faceHasCastWithFlash): the face half of castWithFlash's fast path
	// (legal_walk_flash.go). A pure function of f.Statics, so it is read
	// only while the facts are fullyCurrent.
	flash bool
	// quiet holds the quiet-seat proof's per-face facts
	// (rules/quiet_facts.go), a pure function of the face's own lists like
	// every other member.
	quiet quietFaceFacts
}

// verifyFresh panics when a field of ff a reader may use differs from a
// recompute over f: the keyword, trigger and label halves are compared only
// while their own guards (keywordsCurrent, triggersCurrent, the name test)
// pass, since every reader of a guarded half checks its guard first, and
// the list-identity guards themselves are not facts.
//
// It runs on EVERY facts read in the rules test binary (tens of millions of
// reads per suite run), so it compares without allocating: the recompute
// leaves the three option labels unbuilt and they are checked against f.Name
// in place (labelIs), and walkFaceFactsEqual is a typed field-by-field
// compare rather than reflect.DeepEqual over two boxed copies, which cost a
// third of the whole suite's allocation and a third of its CPU.
func (ff *walkFaceFacts) verifyFresh(f *cards.Face) {
	fresh := computeWalkFaceFactsForVerify(f)
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
	labelsOK := true
	if ff.name != f.Name {
		got.name = fresh.name
	} else {
		labelsOK = labelIs(ff.castLabel, "Cast ", f.Name, "") && labelIs(ff.playLabel, "Play ", f.Name, "") &&
			labelIs(ff.manaLabel, "Activate ", f.Name, " for mana")
	}
	got.castLabel, got.playLabel, got.manaLabel = fresh.castLabel, fresh.playLabel, fresh.manaLabel
	if !ff.fullyCurrent(f) {
		got.staticOn, got.staticOff, got.mayPlay = fresh.staticOn, fresh.staticOff, fresh.mayPlay
	}
	if !ff.staticsCurrent(f) {
		got.flash = fresh.flash
	}
	// Scan verdicts have their own verifier in faceScanHas. Keep the cached
	// value on both sides here rather than recomputing it in this whole-facts
	// check; this verifier still compares every other field.
	got.statFirst, got.statLen, got.replFirst, got.replLen = fresh.statFirst, fresh.statLen, fresh.replFirst, fresh.replLen
	got.svars, got.svarsLen = fresh.svars, fresh.svarsLen
	fresh.scan = ff.scan
	if !labelsOK || !walkFaceFactsEqual(&got, &fresh) {
		panic(fmt.Sprintf("rules: walk face facts for %q are stale (%+v vs %+v)", f.Name, *ff, fresh))
	}
}

// labelIs reports whether l == prefix+name+suffix without building the
// concatenation.
func labelIs(l, prefix, name, suffix string) bool {
	return len(l) == len(prefix)+len(name)+len(suffix) && l[:len(prefix)] == prefix &&
		l[len(prefix):len(prefix)+len(name)] == name && l[len(prefix)+len(name):] == suffix
}

// walkFaceFactsEqual is reflect.DeepEqual(a, b) for walkFaceFacts, typed:
// every field but altCosts is compared with ==, and altCosts row by row
// (Cost.Equal on each frozen Cost, through pointers so nothing is boxed). The
// pointer-valued fields compare by identity, which is DeepEqual's answer
// here: each is a list-identity guard or the face itself, and verifyFresh
// either checked it current (so both sides hold the same address) or copied
// fresh's value over it. TestWalkFaceFactsEqualCoversEveryField holds this
// field list to the struct's.
func walkFaceFactsEqual(a, b *walkFaceFacts) bool {
	if a.face != b.face || a.abFirst != b.abFirst || a.abLen != b.abLen ||
		a.abZones != b.abZones || a.abZonesActivator != b.abZonesActivator ||
		a.mana != b.mana || a.manaReflected != b.manaReflected || a.manaAllTap != b.manaAllTap ||
		a.manaControllerOnly != b.manaControllerOnly ||
		a.kwGranted != b.kwGranted || a.kwFirst != b.kwFirst || a.kwLen != b.kwLen || a.ph != b.ph ||
		a.altCostMask != b.altCostMask || a.impendingCount != b.impendingCount ||
		a.name != b.name || a.castLabel != b.castLabel || a.playLabel != b.playLabel || a.manaLabel != b.manaLabel ||
		a.scan != b.scan || a.trigZones != b.trigZones || a.trigSig != b.trigSig || a.trigSigOther != b.trigSigOther ||
		a.trigLookBack != b.trigLookBack || a.grantsTrig != b.grantsTrig || a.trigFirst != b.trigFirst || a.trigLen != b.trigLen ||
		a.statFirst != b.statFirst || a.statLen != b.statLen || a.replFirst != b.replFirst || a.replLen != b.replLen ||
		a.svars != b.svars || a.svarsLen != b.svarsLen ||
		a.staticOn != b.staticOn || a.staticOff != b.staticOff ||
		a.mayPlay != b.mayPlay ||
		a.flash != b.flash || a.quiet != b.quiet {
		return false
	}
	if (a.altCosts == nil) != (b.altCosts == nil) || len(a.altCosts) != len(b.altCosts) {
		return false
	}
	for i := range a.altCosts {
		if a.altCosts[i].ok != b.altCosts[i].ok || !a.altCosts[i].cost.Equal(&b.altCosts[i].cost) {
			return false
		}
	}
	return true
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

// staticsCurrent reports whether the facts were computed over f's current
// static list. flash (and staticOn/staticOff) depend only on that list, so a
// reader of just those bits checks this strictly weaker guard than
// fullyCurrent.
func (ff *walkFaceFacts) staticsCurrent(f *cards.Face) bool {
	return ff.statLen == len(f.Statics) && (ff.statLen == 0 || ff.statFirst == &f.Statics[0])
}

// walkSkipVerify: see derivedMemoVerify. Set by the rules test binary.
var walkSkipVerify = derivedMemoVerifyFlag != ""

func computeWalkFaceFacts(f *cards.Face) walkFaceFacts {
	return computeWalkFaceFactsMode(f, true)
}

// computeWalkFaceFactsForVerify omits scan facts because faceScanHas owns
// their independent verification on every scan read, and the option labels,
// which verifyFresh checks against the face's name without building them.
func computeWalkFaceFactsForVerify(f *cards.Face) walkFaceFacts {
	return computeWalkFaceFactsMode(f, false)
}

func computeWalkFaceFactsMode(f *cards.Face, includeScan bool) walkFaceFacts {
	ff := walkFaceFacts{face: f, abLen: len(f.Abilities), kwLen: len(f.Keywords), name: f.Name}
	if len(f.Abilities) > 0 {
		ff.abFirst = &f.Abilities[0]
	}
	if includeScan {
		// The verify recompute leaves the labels unbuilt: verifyFresh
		// checks the cached ones against f.Name in place.
		ff.castLabel, ff.playLabel, ff.manaLabel = "Cast "+f.Name, "Play "+f.Name, "Activate "+f.Name+" for mana"
		ff.scan = computeFaceScan(f)
	}
	ff.trigZones, ff.trigLen = computeFaceTriggerZones(f, phaseSpecValid), len(f.Triggers)
	ff.trigSig, ff.trigSigOther = computeFaceTrigSigs(f, phaseSpecValid)
	ff.trigLookBack = computeFaceLookBackZones(f)
	ff.grantsTrig = faceGrantsTriggers(f)
	if len(f.Triggers) > 0 {
		ff.trigFirst = &f.Triggers[0]
	}
	ff.statLen, ff.replLen, ff.svarsLen, ff.svars = len(f.Statics), len(f.Repls), len(f.SVars), svarsIdentity(f)
	ff.staticOn, ff.staticOff, ff.mayPlay = faceStaticHotOn(f), faceStaticHotOff(f), faceMayPlayHot(f)
	ff.flash = faceHasCastWithFlash(f)
	if len(f.Statics) > 0 {
		ff.statFirst = &f.Statics[0]
	}
	if len(f.Repls) > 0 {
		ff.replFirst = &f.Repls[0]
	}
	ff.ph = printedHeadsOf(f)
	ff.impendingCount = impendingCount(f)
	for i := range altCastModes {
		// Every row keys on its K: head (faceCostRaw answers false without
		// it); testing presence first skips building and copying an
		// 824-byte zero Cost per absent row, i.e. per row on almost every
		// face.
		if _, ok := f.KeywordParam(altCastModes[i].head); !ok {
			continue
		}
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
	// The quiet facts depend on ff.altCosts (a compiled alternative-cost
	// keyword entry is castOpen), so they are built after the alt-cost loop.
	ff.quiet = computeQuietFaceFacts(f, len(ff.altCosts) > 0)
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
			// A fully current entry another configuration already published
			// on the face IS this compute (a pure function of the face's
			// lists, which fullyCurrent pins), and is what walkFaceFactsOf
			// serves for f anyway: copy it rather than recompute. Every
			// configuration lists the whole token corpus, so the compliance
			// generator's one-configuration-per-scenario builds recomputed
			// ~a thousand faces per scenario without this.
			if p := f.ExtSlot().Load(); p != nil {
				if ff := (*walkFaceFacts)(p); ff.fullyCurrent(f) {
					t.slots[i] = *ff
					continue
				}
			}
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
	return e.compiledText.faceFacts(f)
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
