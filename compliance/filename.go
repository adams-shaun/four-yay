package compliance

import "strings"

// ManifestFileName is the file a set's manifest is committed under:
// "<CODE>.json", or "<CODE>_.json" when the code is a Windows reserved device
// name. Go's module zip refuses such a path element (golang.org/x/mod/module
// badWindowsNames), so Conflux's CON.json would make the whole module
// un-fetchable.
func ManifestFileName(code string) string {
	if reservedWindowsName(code) {
		return code + "_.json"
	}
	return code + ".json"
}

// reservedWindowsName reports whether a path element (up to its first dot)
// is a Windows reserved device name, case-insensitively.
func reservedWindowsName(elem string) bool {
	if i := strings.IndexByte(elem, '.'); i >= 0 {
		elem = elem[:i]
	}
	switch u := strings.ToUpper(elem); u {
	case "CON", "PRN", "AUX", "NUL":
		return true
	default:
		return len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '1' && u[3] <= '9'
	}
}
