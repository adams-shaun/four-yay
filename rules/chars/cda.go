package chars

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects"
	"github.com/adams-shaun/gorge/state"
)

// StaticZoneAdmits is the source-zone admission a Continuous static's
// ExcludeZone$ and EffectZone$ parameters jointly express, the ONE read
// rules' staticEffects gate and CDAPTStatic's layer-7a CDA claim both make. With
// no ExcludeZone$ the ordinary EffectZone$ gate stands unchanged (empty =
// battlefield). With one, the named zones are excluded and -- absent an
// explicit EffectZone$ -- every OTHER zone admits, which is what lets a
// zone-conditional CDA (Grist) live exactly off the battlefield. An
// unrecognised word excludes NOTHING (the mirror-image direction of
// affectedZoneOK's fail-closed deny): the unparseable exclusion degrades to
// the ordinary gate, today's applies-as-gated behaviour, rather than going
// silent.
func StaticZoneAdmits(exclude, effectZone string, z state.Zone) bool {
	exclude = strings.TrimSpace(exclude)
	if exclude == "" {
		return EffectZoneOK(effectZone, z)
	}
	zones, all, ok := effects.ParseZones(exclude)
	if !ok {
		return EffectZoneOK(effectZone, z)
	}
	if all || slices.Contains(zones, z) {
		return false
	}
	return effectZone == "" || EffectZoneOK(effectZone, z)
}

// EffectZoneOK reports whether a static whose EffectZone$ reads v applies
// while its source sits in zone z. Forge's default is the battlefield, and
// the corpus names zones with Forge's comma-separated All/Battlefield/Stack/
// Graveyard/Hand/Library/Exile/Command words; anything unrecognised denies
// (the same fail-closed direction every unparseable static qualifier takes),
// because wrongly applying a hand-or-library static is exactly the class of
// over-reach the zone gate exists to prevent.
func EffectZoneOK(v string, z state.Zone) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return z == state.ZBattlefield
	}
	for name := range strings.SplitSeq(v, ",") {
		switch effectZoneOKCodes.Code(string(strings.TrimSpace(name))) {
		case effectZoneOKAll:
			return true
		case effectZoneOKBattlefield:
			if z == state.ZBattlefield {
				return true
			}
		case effectZoneOKStack:
			if z == state.ZStack {
				return true
			}
		case effectZoneOKGraveyard:
			if z == state.ZGraveyard {
				return true
			}
		case effectZoneOKHand:
			if z == state.ZHand {
				return true
			}
		case effectZoneOKLibrary:
			if z == state.ZLibrary {
				return true
			}
		case effectZoneOKExile:
			if z == state.ZExile {
				return true
			}
		case effectZoneOKCommand:
			if z == state.ZCommand {
				return true
			}
		}
	}
	return false
}

// EffectZones is an EffectZone$ value compiled once: bit z is
// EffectZoneOK(v, z), and effectZonesBlank marks the text exactly "" (the
// ExcludeZone$ form's "no explicit EffectZone$"). It is the PKEffectZone
// ParamCoder, so a printed static's code is stored at load and the per-object
// static walks read it through ParamCode instead of re-splitting the word
// list.
type EffectZones uint16

const effectZonesBlank EffectZones = 1 << 15

// EffectZonesOf compiles v.
func EffectZonesOf(v string) EffectZones {
	var m EffectZones
	for z := state.Zone(0); z < 15; z++ {
		if EffectZoneOK(v, z) {
			m |= 1 << z
		}
	}
	if v == "" {
		m |= effectZonesBlank
	}
	return m
}

// Admits is EffectZoneOK for the compiled value.
func (m EffectZones) Admits(z state.Zone) bool { return z < 15 && m&(1<<z) != 0 }

// staticEffectZones is st's compiled EffectZone$; an absent key reads as the
// empty text, exactly as its ParamStr did.
func staticEffectZones(st cards.Static) EffectZones {
	c, ok := st.ParamCode(cards.PKEffectZone)
	if !ok {
		return EffectZonesOf("")
	}
	return EffectZones(c)
}

// StaticEffectZoneOK is EffectZoneOK(st.ParamStr(EffectZone$), z) through
// the code stored at load.
func StaticEffectZoneOK(st cards.Static, z state.Zone) bool { return staticEffectZones(st).Admits(z) }

// StaticZoneAdmitsStatic is StaticZoneAdmits over st's own ExcludeZone$ and
// EffectZone$, through the codes stored at load.
func StaticZoneAdmitsStatic(st cards.Static, z state.Zone) bool {
	ez := staticEffectZones(st)
	c, ok := st.ParamCode(cards.PKExcludeZone)
	excl := effects.ZoneList(c)
	if !ok || !excl.OK() {
		return ez.Admits(z)
	}
	if excl.All() || excl.Has(z) {
		return false
	}
	return ez&effectZonesBlank != 0 || ez.Admits(z)
}

func init() {
	cards.RegisterParamCoder(cards.PKEffectZone, "chars.EffectZones", func(v string) uint16 { return uint16(EffectZonesOf(v)) })
	cards.RegisterParamCoder(cards.PKExcludeZone, "effects.ZoneList", func(v string) uint16 { return uint16(effects.ZoneListOf(v)) })
}

// CDAPTStatic resolves ONE static's characteristic-defining P/T claim
// (CharacteristicDefining$ True), the layer-7a base CDASetPT applies in
// every zone (CR 613.4a, CR 604.3/208.2). A static carrying any parameter
// beyond the implemented shape's whitelist fails closed (no claim -- the
// explicit-whitelist rule rules' adjustLandPlaysGrant documents), as does one
// scoped to anything but the card itself, and one whose SetPower$/
// SetToughness$ value is neither a literal nor an SVar/inline Count$
// expression the evaluator resolves (EvalCountOK's verdict -- e.g.
// LifePaidOnETB's paid-life shape). Iterating st.Params only yields the
// whitelist boolean, so map order never reaches a value -- determinism is
// preserved.
func CDAPTStatic(h effects.Host, st cards.Static, ctx *effects.Ctx) (p, t int32, hasP, hasT bool) {
	for key := range st.Params {
		// The keys the implemented CDA shape (and only it) carries.
		if !cdaPTStaticKeys.Has(key) {
			return 0, 0, false, false
		}
	}
	if aff := strings.TrimSpace(st.ParamStr(cards.PKAffected)); aff != "" && aff != "Card.Self" {
		return 0, 0, false, false
	}
	// ExcludeZone$ narrows the claim's zones (Grist, the Hunger Tide): the CDA
	// read is every zone by CR 604.3/208.2, minus the ones the static names --
	// and beside any explicit EffectZone$, exactly as the emission gate reads
	// the pair. The same StaticZoneAdmits helper, so the layer-7a claim and
	// any emitted fallback ce cannot disagree about where the static is live.
	// A source object already gone carries no zone to admit.
	if raw := strings.TrimSpace(st.ParamStr(cards.PKExcludeZone)); raw != "" {
		if oz := h.Game().Obj(ctx.Source); oz == nil || !StaticZoneAdmits(raw, st.ParamStr(cards.PKEffectZone), oz.Zone) {
			return 0, 0, false, false
		}
	}
	if raw, ok := st.Param(cards.PKSetPower); ok {
		if n, ok := cdaValue(h, ctx, raw); ok {
			p, hasP = n, true
		}
	}
	if raw, ok := st.Param(cards.PKSetToughness); ok {
		if n, ok := cdaValue(h, ctx, raw); ok {
			t, hasT = n, true
		}
	}
	return p, t, hasP, hasT
}

// cdaValue resolves one CDA P/T value: a literal integer, else the SVar
// named on the card's own face, else an inline Count$ expression -- each
// through effects.EvalCountOK's resolvability verdict, so an unmodelled
// count body fails closed (no claim) instead of degrading to a silent zero.
func cdaValue(h effects.Host, ctx *effects.Ctx, raw string) (int32, bool) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil {
		return int32(n), true
	}
	// A runtime SVar write (api:StoreSVar) shadows the printed body of the
	// same name: Minion of the Wastes / Nameless Race store the life paid as
	// they entered under LifePaidOnETB, whose printed default is Number$0, so
	// the stored value must be checked BEFORE the face table is consulted.
	if o := h.Game().Obj(ctx.Source); o != nil {
		if v, ok := o.RuntimeSVars[raw]; ok {
			return v, true
		}
	}
	if strings.HasPrefix(raw, "Count$") {
		return effects.EvalCountOK(h, ctx, raw)
	}
	if body, ok := ctx.SVars[raw]; ok {
		return effects.EvalCountOK(h, ctx, body)
	}
	return 0, false
}

// CDASetPT is the object's own layer-7a characteristic-defining P/T
// (CR 613.4a): the first usable CDA static on the current face (script
// order) resolves the base power and toughness CDAPTStatic's whitelist
// admits. CR 604.3/208.2 put the ability in EVERY zone, which is exactly
// why it is read directly off the face in PT rather than emitted
// from the battlefield-only static scan. A face with no usable CDA degrades
// to no claim (the printed P/T stands).
func CDASetPT(b Board, o *state.Object) (p, t int32, hasP, hasT bool) {
	f := o.Face()
	if f == nil {
		return 0, 0, false, false
	}
	var ctx *effects.Ctx // built only for a face that has a CDA static
	for _, st := range f.Statics {
		if st.Mode != "Continuous" || strings.TrimSpace(st.ParamStr(cards.PKCharacteristicDefining)) == "" {
			continue
		}
		if ctx == nil {
			ctx = b.CDAContext(o, f)
		}
		if pp, tt, hp, ht := CDAPTStatic(b.Host(), st, ctx); hp || ht {
			return pp, tt, hp, ht
		}
	}
	return 0, 0, false, false
}

// addPT saturates instead of allowing a large static expression to wrap a
// characteristic through zero. Forge's calculateAmount is int-bounded too;
// keeping the clamp at this boundary makes all P/T additions deterministic.
func addPT(a, b int32) int32 {
	n := int64(a) + int64(b)
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

type effectZoneOKCode uint16

const (
	effectZoneOKAll effectZoneOKCode = iota + 1
	effectZoneOKBattlefield
	effectZoneOKStack
	effectZoneOKGraveyard
	effectZoneOKHand
	effectZoneOKLibrary
	effectZoneOKExile
	effectZoneOKCommand
)

var effectZoneOKCodes = state.NewStrCodes(
	state.StrEntry[effectZoneOKCode]{Key: "All", Val: effectZoneOKAll},
	state.StrEntry[effectZoneOKCode]{Key: "Battlefield", Val: effectZoneOKBattlefield},
	state.StrEntry[effectZoneOKCode]{Key: "Stack", Val: effectZoneOKStack},
	state.StrEntry[effectZoneOKCode]{Key: "Graveyard", Val: effectZoneOKGraveyard},
	state.StrEntry[effectZoneOKCode]{Key: "Hand", Val: effectZoneOKHand},
	state.StrEntry[effectZoneOKCode]{Key: "Library", Val: effectZoneOKLibrary},
	state.StrEntry[effectZoneOKCode]{Key: "Exile", Val: effectZoneOKExile},
	state.StrEntry[effectZoneOKCode]{Key: "Command", Val: effectZoneOKCommand},
)

// cdaPTStaticKeys are the keys the implemented P/T CDA static shape carries.
var cdaPTStaticKeys = state.NewNameSet(
	"Mode", "CharacteristicDefining", "SetPower", "SetToughness", "Affected",
	"Description", "ExcludeZone",
)
