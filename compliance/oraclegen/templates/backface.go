// backface.go is the shared helper that lets a face-0 level-B template serve a
// requirement on a face after 0. The runner's setup places the named
// battlefield card on its back face (rules/oracle_run.go, the "back_face"
// seat list), so a template that would otherwise put the card on the
// battlefield in face 0 marks it here instead. The setup shortcut is the whole
// point: a face-1 requirement is served without casting the front face and
// transforming.
package templates

import (
	"github.com/adams-shaun/gorge/compliance/levelb"
	"github.com/adams-shaun/gorge/compliance/oraclegen"
)

// setupBackFace marks name as a back-face (face index 1) battlefield setup
// when the requirement lives on a face after 0. A face-0 requirement is left
// untouched, so every level-A and face-0 level-B scenario keeps its exact
// bytes. The card must already be in the seat's Battlefield list; the runner
// flips it there.
func setupBackFace(p0 *oraclegen.Seat, name string, req levelb.Requirement) {
	if req.Face > 0 {
		p0.BackFace = appendFixtureUnique(p0.BackFace, name)
	}
}
