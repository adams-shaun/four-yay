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
	// name and the option labels the walk offers for this face, built once
	// so a walk shares them instead of concatenating per option (Go strings
	// are immutable; a shared label is the same value the concatenation
	// produces). A label is served only while f.Name still equals name.
	name                             string
	castLabel, playLabel, manaLabel string
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
	if len(f.Keywords) > 0 {
		ff.kwFirst = &f.Keywords[0]
		for _, h := range grantedKWHeads {
			if f.KeywordLinesHaveHead(h.s, h.id) {
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
		if isManaAbilityAPI(ab.API) && !loyaltyAbilityText(ab) {
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
		if !ParseCost(ma.Params["Cost"]).Tap {
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
	raw := ab.Params["Cost"]
	if !containsLoyaltyFold(raw) {
		return isLoyaltyMarked(ab)
	}
	c := ParseCost(raw)
	return isLoyaltyAbilityRef(ab, &c)
}

func blankActivator(ab *cards.SA) bool {
	v, ok := ab.Params["Activator"]
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
	return t
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
				if fresh := computeWalkFaceFacts(f); fresh != *s {
					panic(fmt.Sprintf("rules: walk face facts for %q are stale (%+v vs %+v)", f.Name, *s, fresh))
				}
			}
			return s
		}
		if s.face == nil {
			return nil
		}
		i = (i + 1) & t.mask
	}
}

// walkFaceFactsOf is the engine's table lookup (nil without a compiledText).
func (e *Engine) walkFaceFactsOf(f *cards.Face) *walkFaceFacts {
	if e == nil || e.compiledText == nil {
		return nil
	}
	return e.compiledText.faces.lookup(f)
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
