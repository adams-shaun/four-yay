package effects

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/effects/params"
)

// The generic targeting tier's compiler lives in the leaf package
// effects/params (params/targets.go, W5 step E7): the payment planner reads
// the compiled TargetParams without importing effects. These are the effects
// spellings, plus the bound evaluations that need the resolution's Host.

// TargetFlag is one compiled boolean fact of an ability's targeting
// (params.TargetFlag).
type TargetFlag = params.TargetFlag

// The TargetFlag facts (params).
const (
	TgtTargeted               = params.TgtTargeted
	TgtValidPresent           = params.TgtValidPresent
	TgtUnique                 = params.TgtUnique
	TgtUniqueSet              = params.TgtUniqueSet
	TgtSameController         = params.TgtSameController
	TgtDifferentControllers   = params.TgtDifferentControllers
	TgtForEachPlayer          = params.TgtForEachPlayer
	TgtPlayerControls         = params.TgtPlayerControls
	TgtPlayerControlsSet      = params.TgtPlayerControlsSet
	TgtMinOneEach             = params.TgtMinOneEach
	TgtMaxOneEach             = params.TgtMaxOneEach
	TgtBoundX                 = params.TgtBoundX
	TgtBoundPromisedGift      = params.TgtBoundPromisedGift
	TgtBoundsDynamic          = params.TgtBoundsDynamic
	TgtNonTriggeredController = params.TgtNonTriggeredController
	TgtTypeStack              = params.TgtTypeStack
	TgtValidStack             = params.TgtValidStack
	TgtValidPlayers           = params.TgtValidPlayers
	TgtValidXBound            = params.TgtValidXBound
	TgtDeclares               = params.TgtDeclares
	TgtAtRandom               = params.TgtAtRandom
	TgtRandomNum              = params.TgtRandomNum
)

// TargetParams is one ability's targeting parameters (params.TargetParams).
type TargetParams = params.TargetParams

// TargetsOf returns sa's compiled targeting parameters (params.TargetsOf).
func TargetsOf(sa *cards.SA) *TargetParams { return params.TargetsOf(sa) }

// dividedParam is DividedAsYouChoose$ as written (params.DividedParam).
func dividedParam(sa *cards.SA) (ParamText, bool) { return params.DividedParam(sa) }

// SpecTargetsStack reports whether a TargetType$/ValidTgts$ value names a
// stack object (params.SpecTargetsStack).
func SpecTargetsStack(spec string) bool { return params.SpecTargetsStack(spec) }

// SpecTargetsPlayers reports whether a spec can name a player as a target
// (params.SpecTargetsPlayers).
func SpecTargetsPlayers(spec string) bool { return params.SpecTargetsPlayers(spec) }

// SpecTargetsOnlyPlayers reports whether every ValidTgts$ alternative names
// only players (params.SpecTargetsOnlyPlayers).
func SpecTargetsOnlyPlayers(spec string) bool { return params.SpecTargetsOnlyPlayers(spec) }

// SpecNamesXBound reports whether spec carries an X-bounded numeric predicate
// (params.SpecNamesXBound).
func SpecNamesXBound(spec string) bool { return params.SpecNamesXBound(spec) }

// NumTextResolved is NumResolved over a compiled parameter (a typed
// parameter struct's ParamText) instead of a key read.
func NumTextResolved(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	return numResolvedText(h, c, p, def)
}

// NumTextResolvedStrict is NumResolvedStrict over a compiled parameter.
func NumTextResolvedStrict(h Host, c *Ctx, p ParamText, def int32) (int32, bool) {
	return numResolvedStrictText(h, c, p, def)
}

func literalInt(p ParamText) (int, bool) {
	if !p.Present {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.Text))
	return n, err == nil
}
