package effects

import (
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
)

// MeldParams is the immutable, per-ability description of a Forge AB$/DB$
// Meld instruction. The condition and payment gates belong to the generic
// ActivationParams tier; these are the meld-specific operands.
type MeldParams struct {
	paramBinding
	Name, Primary, Secondary, SecondaryType string
	Tapped, Attacking                       bool
}

var meldFront [1 << 10]atomic.Pointer[MeldParams]

// MeldOf uses the configured compiled record, then the parameter-map front
// cache for SVar-resolved copies, and only recompiles when the map changed.
func MeldOf(sa *cards.SA) *MeldParams {
	if sa == nil {
		return nil
	}
	if f := LoadSAFacts(sa); f != nil {
		if p := f.Meld; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &meldFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileMeld(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

// compileMeld is the only reader of Meld-specific parameters.
func compileMeld(sa *cards.SA) *MeldParams {
	return &MeldParams{
		paramBinding:  bindParams(sa),
		Name:          strings.TrimSpace(sa.ParamStr(cards.PKName)),
		Primary:       strings.TrimSpace(sa.ParamStr(cards.PKPrimary)),
		Secondary:     strings.TrimSpace(sa.ParamStr(cards.PKSecondary)),
		SecondaryType: strings.TrimSpace(sa.ParamStr(cards.PKSecondaryType)),
		Tapped:        isTrue(sa.ParamStr(cards.PKTapped)),
		Attacking:     isTrue(sa.ParamStr(cards.PKAttacking)),
	}
}
