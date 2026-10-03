package rules

import "github.com/adams-shaun/gorge/state"

// The resolution-time target set (spike S3, legacy defect 2).
//
// resolveTop does not hand a resolving chain the stack object's recorded
// Targets as-is. An overloaded spell's "each" set is a census taken as it
// resolves (CR 702.96b), CR 608.2b drops the targets that became illegal, and
// a modal object resolves against its per-mode recheck's flat list. That
// derived list is fixed for the whole resolution: a chain that suspends at a
// mid-resolution ask must re-enter against the SAME list, or every per-target
// cursor (effDiscard's DiscardTarget, the Defined$ Targeted walks) indexes a
// different set. Re-deriving it at resume is wrong too: the census would see
// objects the resolution itself moved, and the legality recheck would drop a
// target the resolution's own earlier effect made illegal.
//
// resolveTop records the derived lists on Engine.resolutionTargets, keyed by
// the stack object, only when they differ from what a resume would otherwise
// rebuild (the common case stores nothing), and resumeResolution reads them
// back (flatFor / modesFor).
// The entry is replaced at every resolveTop resolution start and removed when
// the object leaves the stack (emit.go), the castSubTargets discipline.

// resolutionTargetSet is one stack object's derived lists: the flat list
// (when it differs from o.Targets) and the CR 608.2b-rechecked per-mode
// groups of a modal object (when they differ from Engine.charmTargets).
type resolutionTargetSet struct {
	flat     []state.Target
	flatSet  bool
	modes    [][]state.Target
	modesSet bool
}

// resolutionTargetMap is Engine.resolutionTargets: the derived sets keyed by
// stack object, holding an entry only where one differs.
type resolutionTargetMap map[state.ObjID]resolutionTargetSet

// record stores the lists resolveTop is about to resolve stack object id
// against: the flat list, and for a modal object whose per-mode recheck ran
// (charmHandled) the rechecked groups, each only when it differs from what a
// resume would otherwise rebuild (recorded / announced).
func (m *resolutionTargetMap) record(id state.ObjID, recorded, resolving []state.Target,
	announced, modes [][]state.Target, charmHandled bool) {
	var set resolutionTargetSet
	if !sameTargetList(recorded, resolving) {
		set.flat, set.flatSet = append([]state.Target(nil), resolving...), true
	}
	if charmHandled && !sameTargetGroups(announced, modes) {
		set.modes, set.modesSet = cloneCharmTargetGroups(modes), true
	}
	if !set.flatSet && !set.modesSet {
		delete(*m, id)
		return
	}
	if *m == nil {
		*m = make(resolutionTargetMap)
	}
	(*m)[id] = set
}

// flatFor is the flat target list a resumed frame of stack object id
// re-enters against: the one resolveTop resolved against, which is recorded
// (o.Targets) unless record kept a derived list.
func (m resolutionTargetMap) flatFor(id state.ObjID, recorded []state.Target) []state.Target {
	if set, ok := m[id]; ok && set.flatSet {
		return set.flat
	}
	return recorded
}

// modesFor is the per-mode target groups a resumed frame of a modal stack
// object re-enters against, as a fresh copy the Ctx owns: the rechecked
// groups resolveTop bound, else the announced ones.
func (m resolutionTargetMap) modesFor(id state.ObjID, announced [][]state.Target) [][]state.Target {
	if set, ok := m[id]; ok && set.modesSet {
		return cloneCharmTargetGroups(set.modes)
	}
	return cloneCharmTargetGroups(announced)
}

func sameTargetList(a, b []state.Target) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameTargetGroups(a, b [][]state.Target) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !sameTargetList(a[i], b[i]) {
			return false
		}
	}
	return true
}
