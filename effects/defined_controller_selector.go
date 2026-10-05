package effects

// DefinedControllerParentReferent reports whether ref -- a
// TargetsWithDefinedController$ value -- names the parent link's targets
// rather than an event role. The parent-target vocabulary ("ParentTarget",
// "ParentTargeted", "ParentTargetedController") is owned by the one table
// that resolves it, definedSpecCodes (context.go's definedSpec), so a caller
// restricts its offer from that classification instead of re-naming the words
// in a second table. The two exporter-relevant spellings, "ParentTarget" and
// "ParentTargetedController", agree on the controller set a target restriction
// reads: the parent targets' players, an object target contributing its
// controller.
func DefinedControllerParentReferent(ref string) bool {
	switch definedSpecCodes.Code(ref) {
	case definedSpecParentTarget, definedSpecParentTargetedController:
		return true
	}
	return false
}
