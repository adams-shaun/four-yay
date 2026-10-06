package effects

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/testutil"
	"github.com/adams-shaun/gorge/state"
)

// TestCopyPermanentDefinedNameRealCorpus pins ECL Mutable Explorer's real
// ChangesZone -> TrigToken -> CopyPermanent DefinedName$ Mutavault script.
// Fix 30c3341fe makes that named-card source produce the tapped Mutavault
// token the card promises, rather than rejecting DefinedName$.
func TestCopyPermanentDefinedNameRealCorpus(t *testing.T) {
	reg := testutil.CorpusRegistry(t)
	explorer, ok := reg.Lookup("Mutable Explorer")
	if !ok {
		t.Fatal("corpus pin moved: Mutable Explorer is missing")
	}
	mutavault, ok := reg.Lookup("Mutavault")
	if !ok {
		t.Fatal("corpus pin moved: Mutavault is missing")
	}
	if len(explorer.Faces) == 0 || len(mutavault.Faces) == 0 {
		t.Fatal("corpus pin moved: Mutable Explorer or Mutavault has no face")
	}
	face := explorer.Faces[0]
	var effectFound bool
	for _, tr := range face.Triggers {
		if tr.Mode != "ChangesZone" || tr.Params["Execute"] != "TrigToken" {
			continue
		}
		if tr.Effect == nil {
			t.Fatal("corpus pin moved: Mutable Explorer TrigToken trigger is not linked")
		}
		if tr.Effect.API != "CopyPermanent" {
			t.Fatalf("corpus pin moved: TrigToken API = %q, want CopyPermanent", tr.Effect.API)
		}
		cp := CopyPermanentOf(tr.Effect)
		if cp.DefinedName != "Mutavault" {
			t.Fatalf("corpus pin moved: TrigToken DefinedName$ = %q, want Mutavault", cp.DefinedName)
		}
		if cp.Blocked {
			t.Fatalf("real CopyPermanent SA is blocked: %+v", cp)
		}
		effectFound = true

		h := newHost(t, 2)
		h.g.NameUniverse = reg.AllCards()
		src := h.g.AddObject(explorer, 0)
		src.Zone = state.ZBattlefield
		h.g.SetZone(state.ZBattlefield, 0, []state.ObjID{src.ID})
		if src.Zone != state.ZBattlefield || len(h.g.Zone(state.ZBattlefield, 0)) != 1 || h.g.Zone(state.ZBattlefield, 0)[0] != src.ID {
			t.Fatal("precondition: Mutable Explorer source must be on seat 0's battlefield")
		}
		if h.g.NamedCard("Mutavault") != mutavault {
			t.Fatal("precondition: production-style NameUniverse must resolve the real Mutavault card")
		}
		Resolve(h, &Ctx{Source: src.ID, Controller: 0, SVars: face.SVars}, tr.Effect)

		var token *state.Object
		for i := range h.g.Objs {
			o := &h.g.Objs[i]
			if o.IsToken && o.Face() != nil && strings.EqualFold(o.Face().Name, "Mutavault") {
				token = o
				break
			}
		}
		if token == nil {
			t.Fatalf("real Mutable Explorer effect minted no Mutavault token; notes %v", ufNotes(h))
		}
		if token.Face() == nil || token.Face().Name != mutavault.Faces[0].Name {
			t.Fatalf("token face = %+v, want real Mutavault face %q", token.Face(), mutavault.Faces[0].Name)
		}
		if !containsFold(token.Face().Types, "Land") {
			t.Fatalf("token types = %v, want Land", token.Face().Types)
		}
		if !token.IsCopy {
			t.Fatal("real Mutavault token is not marked IsCopy")
		}
		if !token.Tapped {
			t.Fatal("real Mutavault token is not tapped")
		}
		if token.Zone != state.ZBattlefield {
			t.Fatalf("real Mutavault token zone = %v, want battlefield", token.Zone)
		}
		for _, note := range ufNotes(h) {
			if strings.Contains(note, "DefinedName") {
				t.Fatalf("real DefinedName$ effect emitted a note: %q", note)
			}
		}
		break
	}
	if !effectFound {
		t.Fatal("corpus pin moved: Mutable Explorer has no ChangesZone Execute$ TrigToken trigger")
	}
}
