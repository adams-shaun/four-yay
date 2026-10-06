package templates

import "github.com/adams-shaun/gorge/cards"

// adventure.go lets a scenario be generated for an Adventure spell face
// (CR 715) requested by name ("Burglar's Plot"). The registry resolves the
// face name to its physical card, whose first face is the creature, so the
// requested face is selected explicitly. The scenario deals the PHYSICAL
// card under its parent (front) name -- the name both engines deal it by --
// and the cast step names the Adventure face; gorge's runner binds that ref
// to the dealt card and casts it through the Adventure offer
// (rules/oracle_run_faces.go).

// requestedFace is the face Generate builds name's scenario from: the
// Adventure spell face when name names one, else the card's first face.
func requestedFace(c *cards.Card, name string) *cards.Face {
	if c.AlternateMode == "Adventure" {
		for _, f := range c.Faces[1:] {
			if f.Name == name {
				return f
			}
		}
	}
	return c.Faces[0]
}

// physicalName is the name setup deals name's card under: the parent
// (front) name for an Adventure spell face, else name itself.
func physicalName(reg *cards.Registry, name string) string {
	if c, ok := reg.Lookup(name); ok && len(c.Faces) > 0 && requestedFace(c, name) != c.Faces[0] {
		return c.Faces[0].Name
	}
	return name
}
