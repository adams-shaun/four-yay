package rules


// renameGateVerifyFlag / renameGateVerify: when set, every refresh the
// no-SetName gate below skips is recomputed in full and the engine panics on
// any difference. Enabled for the rules test binary by
// setname_gate_verify_test.go; for a built binary, pass
// go build -ldflags "-X github.com/adams-shaun/gorge/rules.renameGateVerifyFlag=1".
var renameGateVerifyFlag string

var renameGateVerify = renameGateVerifyFlag != ""

// setNameInLists is a cheap SUPERSET of anySetNameActive that does not build
// active(). active() is the live registry entries (a subset of the registry,
// filtered by continuousLive) plus the static memo, then
// pruneLostSourceAbilities (which only drops entries). So if neither source
// list carries a layer-3 SetName effect, no active effect sets a name, the
// rename table is necessarily empty, and refreshRenames can skip the full
// active() rebuild it would otherwise force after every board-changing event.
// Granted or copied SetName statics are covered: they reach active() only
// through these two lists, not through a printed-text probe.
//
// A plain function over the two slices, not an Engine method, so it adds
// nothing to the codeshape engine-surface metrics.
func setNameInLists(registry, statics []ContinuousEffect) bool {
	for i := range registry {
		if ce := &registry[i]; ce.Layer == LText && ce.SetName != "" {
			return true
		}
	}
	for i := range statics {
		if ce := &statics[i]; ce.Layer == LText && ce.SetName != "" {
			return true
		}
	}
	return false
}
