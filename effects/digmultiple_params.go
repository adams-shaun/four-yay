package effects

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/state"
)

// DigMultipleParams is the single read home for DigMultiple's independently
// selectable ChangeValid alternatives and its two destinations.
type DigMultipleParams struct {
	paramBinding
	Num                                                              ParamText
	Specs                                                            []string
	Dest, Rest                                                       state.Zone
	Reveal, Optional, RandomRest, Remember, ImprintRest, ChangeLater bool
	ChooseAmount                                                     int
	ChosenZone                                                       state.Zone
}

func digMultipleParam(sa *cards.SA, key cards.ParamKey) ParamText {
	v, ok := sa.Param(key)
	return ParamText{Text: v, Present: ok}
}

// DigMultipleOf returns the bound compiled record, or compiles a copy whose
// Params map differs from the catalog entry (the DigOf front-cache contract).
func DigMultipleOf(sa *cards.SA) *DigMultipleParams {
	if f := LoadSAFacts(sa); f != nil {
		if p := f.DigMultiple; p != nil && p.boundTo(sa.Params) {
			return p
		}
	}
	slot := &digMultipleFront[paramMapSlot(sa.Params)]
	if p := slot.Load(); p != nil && p.boundTo(sa.Params) {
		return p
	}
	p := compileDigMultiple(sa)
	if sa.Params != nil {
		slot.Store(p)
	}
	return p
}

var digMultipleFront [1 << 10]atomic.Pointer[DigMultipleParams]

func compileDigMultiple(sa *cards.SA) *DigMultipleParams {
	p := &DigMultipleParams{paramBinding: bindParams(sa), Num: digMultipleParam(sa, cards.PKDigNum), Dest: state.ZHand, Rest: state.ZLibrary, ChosenZone: state.ZBattlefield}
	for _, spec := range strings.Split(digMultipleParam(sa, cards.PKChangeValid).Text, ",") {
		if spec = strings.TrimSpace(spec); spec != "" {
			p.Specs = append(p.Specs, permanentCardSpec(spec))
		}
	}
	if v := digMultipleParam(sa, cards.PKDestinationZone).Text; v != "" {
		p.Dest = ParseZone(v)
	}
	if v := digMultipleParam(sa, cards.PKDestinationZone2).Text; v != "" {
		p.Rest = ParseZone(v)
	}
	if v := digMultipleParam(sa, cards.PKChosenZone).Text; v != "" {
		p.ChosenZone = ParseZone(v)
	}
	p.Reveal = isTrue(digMultipleParam(sa, cards.PKReveal).Text)
	p.Optional = isTrue(digMultipleParam(sa, cards.PKOptional).Text)
	p.RandomRest = isTrue(digMultipleParam(sa, cards.PKRestRandomOrder).Text)
	p.Remember = isTrue(digMultipleParam(sa, cards.PKRememberChanged).Text)
	p.ImprintRest = isTrue(digMultipleParam(sa, cards.PKImprintRest).Text)
	p.ChangeLater = isTrue(digMultipleParam(sa, cards.PKChangeLater).Text)
	p.ChooseAmount, _ = strconv.Atoi(digMultipleParam(sa, cards.PKChooseAmount).Text)
	return p
}
