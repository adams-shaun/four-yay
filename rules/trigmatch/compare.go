package trigmatch

import (
	"strconv"
	"strings"

	"github.com/adams-shaun/gorge/state"
)

// CompareLife compares a life total against a Forge comparison literal such as
// GE40, EQ0, LE3. Any shape this build cannot fold (a non-numeric rhs, a
// missing operator) is false, so an unreadable condition never fires a
// trigger.
func CompareLife(have int32, cmp string) bool {
	op, n, ok := SplitCompare(strings.TrimSpace(cmp))
	if !ok {
		return false
	}
	return ApplyCompare(int(have), op, n)
}

// ComparePresent compares a present-count against the same comparison literal
// grammar. A non-numeric rhs (PresentCompare$ EQX) fails closed; callers
// that hold a source fold an SVar rhs first (presentCompareFor).
func ComparePresent(have int, cmp string) bool {
	op, n, ok := SplitCompare(strings.TrimSpace(cmp))
	if !ok {
		return false
	}
	return ApplyCompare(have, op, n)
}

// SplitCompare separates a Forge comparison literal ("GE40", "EQ0") into its
// two-character operator and its numeric rhs. ok is false for anything that is
// not a recognised operator followed by an integer.
func SplitCompare(cmp string) (op string, n int, ok bool) {
	if len(cmp) < 3 {
		return "", 0, false
	}
	op = cmp[:2]
	num, err := strconv.Atoi(cmp[2:])
	if err != nil {
		return "", 0, false
	}
	return op, num, true
}

func ApplyCompare(have int, op string, n int) bool {
	switch applyCompareff81Codes.Code(string(op)) {
	case applyCompareff81GE:
		return have >= n
	case applyCompareff81LE:
		return have <= n
	case applyCompareff81EQ:
		return have == n
	case applyCompareff81GT:
		return have > n
	case applyCompareff81LT:
		return have < n
	case applyCompareff81NE:
		return have != n
	}
	return false
}

const (
	applyCompareff81GE uint16 = 1 // "GE"
	applyCompareff81LE uint16 = 2 // "LE"
	applyCompareff81EQ uint16 = 3 // "EQ"
	applyCompareff81GT uint16 = 4 // "GT"
	applyCompareff81LT uint16 = 5 // "LT"
	applyCompareff81NE uint16 = 6 // "NE"
)

var applyCompareff81Codes = state.NewStrCodes(
	state.StrEntry[uint16]{Key: "GE", Val: applyCompareff81GE},
	state.StrEntry[uint16]{Key: "LE", Val: applyCompareff81LE},
	state.StrEntry[uint16]{Key: "EQ", Val: applyCompareff81EQ},
	state.StrEntry[uint16]{Key: "GT", Val: applyCompareff81GT},
	state.StrEntry[uint16]{Key: "LT", Val: applyCompareff81LT},
	state.StrEntry[uint16]{Key: "NE", Val: applyCompareff81NE},
)
