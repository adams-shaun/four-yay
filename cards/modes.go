package cards

import "strings"

// SplitModeNames is the ONE interpretation of a modal ability's Choices$
// text as a mode list: split on commas, each name trimmed, in script order
// (an empty text is the one-name list [""], as strings.Split gives). The
// parameter compiler (effects' compileCharm, whose CharmParams.Modes every
// engine path reads) and the resolution kernel's ask-free text predicate
// (SAChainMayAsk, which cannot see effects) both split through it, so the
// mode list the predicate walks is the list the resolution runs.
func SplitModeNames(choices string) []string {
	names := strings.Split(choices, ",")
	for i := range names {
		names[i] = strings.TrimSpace(names[i])
	}
	return names
}
