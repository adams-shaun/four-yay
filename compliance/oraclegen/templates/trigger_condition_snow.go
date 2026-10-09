// The snow-covered basic presence a trigger's own enter probe needs. A
// ChangesZone trigger's etb cause plays the basic its ValidCard filter names
// from p0's hand; a presence setup carrying the same name would leave the
// play step's card ref pointing at a battlefield copy (the first object of
// that name), so the probe never plays. The snow-covered twin is the same
// land type with a distinct name: Roiling Canopy's "if you control at least
// five other Forests" is set up with those, and the played Forest stays
// unambiguous.
package templates

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
	"github.com/adams-shaun/gorge/compliance/levelb"
)

// snowPresenceSwap renames the plain basic-land presence preludes whose land
// type the row trigger's own zone-change filter names and plays.
func snowPresenceSwap(t *cards.Trigger, preludes []conditionPrelude) {
	filter := strings.ToLower(levelb.ZoneChangeFilter(t))
	if filter == "" {
		return
	}
	for i := range preludes {
		for j, n := range preludes[i].battlefield {
			twin, ok := snowBasicTwin(n)
			if !ok || !strings.Contains(filter, strings.ToLower(n)) {
				continue
			}
			preludes[i].battlefield[j] = twin
		}
	}
}

// snowBasicTwin is the snow-covered twin of a basic land name.
func snowBasicTwin(name string) (string, bool) {
	switch name {
	case "Plains":
		return "Snow-Covered Plains", true
	case "Island":
		return "Snow-Covered Island", true
	case "Swamp":
		return "Snow-Covered Swamp", true
	case "Mountain":
		return "Snow-Covered Mountain", true
	case "Forest":
		return "Snow-Covered Forest", true
	}
	return "", false
}
