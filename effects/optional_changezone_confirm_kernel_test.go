package effects

// The no-host (R-9) leaves of the deleted Optional$ confirm-before-pick
// tests: with no decision channel the confirmation gate and the Min-0 pick
// are both built and answered deterministically in the player's place --
// "may" played as "do" -- so the first eligible card moves. (The answered
// leaves are re-hosted on a real engine in
// rules/optional_changezone_confirm_kernel_test.go.)

import (
	"testing"

	"github.com/adams-shaun/gorge/state"
)

func TestOptionalChangeZoneHandConfirmNoHost(t *testing.T) {
	h, ids := handNoHostFixture(t)
	Resolve(h, &Ctx{Controller: 0}, sa(t, ocHandOptional))
	if h.askCount != 2 {
		t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", h.askCount)
	}
	if o := h.g.Obj(ids[0]); o.Zone != state.ZBattlefield {
		t.Fatalf("no-host fetch left the first land on %s, want battlefield", o.Zone)
	}
}

func TestOptionalChangeZoneHiddenPickConfirmNoHost(t *testing.T) {
	fh, bearID, _ := ocHiddenFixture(t)
	h := &fakeHost{g: fh.g}
	Resolve(h, &Ctx{Controller: 0}, sa(t, ocHiddenOptional))
	if h.askCount != 2 {
		t.Fatalf("no-host ask count = %d, want 2 (the confirmation gate and the pick)", h.askCount)
	}
	if o := h.g.Obj(bearID); o.Zone != state.ZBattlefield {
		t.Fatalf("no-host fetch left the creature on %s, want battlefield", o.Zone)
	}
}
