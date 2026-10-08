package scriptfacts

import (
	"strings"

	"github.com/adams-shaun/gorge/cards"
)

// TriggerOptionalSpec is the OptionalDecider$ spec a printed trigger's CR 603.5
// yes/no is posed under, or "" when the trigger is not optional. It is the one
// read the placement path (triggerOptional), the resolution gate (resolveTop)
// and the view's StackOptional share, so the three cannot disagree.
//
// One script shape carries OptionalDecider$ although the Oracle ability has
// no optional action: a trigger whose ONLY "may" is a play/cast permission it
// grants for a duration ("Until the end of your next turn, you may play that
// card" -- Strongbox Raider; "you may cast a creature spell from that
// player's graveyard this turn" -- Whispersteel Dagger). CR 603.5 makes an
// ability optional only when its text says its controller "may" take an
// action on resolution; a "may play ... this turn" grants a permission the
// player exercises later, so the exile (Strongbox Raider's top two cards)
// happens regardless. A corpus census (every T: line with OptionalDecider$
// whose every may-sentence is a play/cast permission with a this-turn/until/
// for-as-long duration) finds exactly those two cards; every other carrier
// keeps its election.
func TriggerOptionalSpec(t cards.Trigger) string {
	spec := t.ParamStr(cards.PKOptionalDecider)
	if spec == "" {
		return ""
	}
	if mayIsOnlyPlayPermission(t.ParamStr(cards.PKTriggerDescription)) {
		return ""
	}
	return spec
}

// mayIsOnlyPlayPermission reports whether every sentence of a trigger
// description that says "may" is a duration-bounded play/cast permission, and
// at least one sentence says "may".
func mayIsOnlyPlayPermission(desc string) bool {
	desc = strings.ToLower(desc)
	if !strings.Contains(desc, "may") {
		return false
	}
	saw := false
	for s := range strings.SplitSeq(desc, ". ") {
		if !descHasWord(s, "may") {
			continue
		}
		saw = true
		if !strings.Contains(s, "may play") && !strings.Contains(s, "may cast") {
			return false
		}
		if !strings.Contains(s, "this turn") && !strings.Contains(s, "until") && !strings.Contains(s, "for as long") {
			return false
		}
	}
	return saw
}

// descHasWord reports whether w occurs in s as a whole word.
func descHasWord(s, w string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], w)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || !descWordByte(s[j-1])
		after := j+len(w) == len(s) || !descWordByte(s[j+len(w)])
		if before && after {
			return true
		}
		i = j + len(w)
	}
}

func descWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
