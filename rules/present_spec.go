package rules

import (
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// presentSpecForZone adapts the Permanent base at a PresentZone count boundary.
// The ordinary matcher intentionally keeps Permanent battlefield-only.
func presentSpecForZone(spec string, zone state.Zone) string {
	if zone == state.ZBattlefield || zone == state.ZStack {
		return spec
	}
	parts := strings.Split(spec, ",")
	for i, part := range parts {
		trimmed := strings.TrimLeft(part, " \t")
		prefix := part[:len(part)-len(trimmed)]
		if trimmed == "Permanent" {
			parts[i] = prefix + "PermanentCard"
		} else if strings.HasPrefix(trimmed, "Permanent.") || strings.HasPrefix(trimmed, "Permanent+") {
			parts[i] = prefix + "PermanentCard" + trimmed[len("Permanent"):]
		}
	}
	return strings.Join(parts, ",")
}
