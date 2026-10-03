package rules

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

// tokenNamedOnBattlefield returns the first battlefield permanent seat p
// controls whose face name is name, failing loudly when absent.
func tokenNamedOnBattlefield(t *testing.T, e *Engine, p state.PlayerID, name string) state.ObjID {
	t.Helper()
	for _, id := range e.G.Zone(state.ZBattlefield, p) {
		if o := e.G.Obj(id); o != nil && o.Face() != nil && o.Face().Name == name {
			return id
		}
	}
	t.Fatalf("no %q permanent on seat %d's battlefield", name, p)
	return 0
}
