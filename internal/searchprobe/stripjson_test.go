package searchprobe

import "bytes"

// potentialActionsKey is the member stripPotentialActions removes.
var potentialActionsKey = []byte(`,"potential_actions":`)

// stripPotentialActions removes every `"potential_actions"` member from a
// marshalled view.View, yielding exactly the bytes the same view marshals to
// when no seat carries the field (it is tagged omitempty, so a nil slice is an
// absent key). The scan is string-aware, so a card name spelling the key is
// not a member. TestStripPotentialActionsMatchesASkippedCapture pins the
// equality against a real capture on every frame of the bench fixture.
func stripPotentialActions(board []byte) []byte {
	if !bytes.Contains(board, potentialActionsKey) {
		return board
	}
	return appendStrippedPotentialActions(make([]byte, 0, len(board)), board)
}

// appendStrippedPotentialActions appends board, stripped as
// stripPotentialActions documents, to out.
func appendStrippedPotentialActions(out, board []byte) []byte {
	key := potentialActionsKey
	for i := 0; i < len(board); {
		switch c := board[i]; {
		case c == '"':
			end := skipJSONString(board, i)
			out = append(out, board[i:end]...)
			i = end
		case c == ',' && bytes.HasPrefix(board[i:], key):
			i = skipJSONValue(board, i+len(key))
		default:
			out = append(out, c)
			i++
		}
	}
	return out
}

// skipJSONString returns the index just past the string literal opening at i.
func skipJSONString(b []byte, i int) int {
	for i++; i < len(b); i++ {
		switch b[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return i
}

// skipJSONValue returns the index just past the value starting at i.
func skipJSONValue(b []byte, i int) int {
	depth := 0
	for i < len(b) {
		switch c := b[i]; c {
		case '"':
			i = skipJSONString(b, i)
			if depth == 0 {
				return i
			}
			continue
		case '[', '{':
			depth++
		case ']', '}':
			if depth--; depth <= 0 {
				if depth < 0 {
					return i
				}
				return i + 1
			}
		case ',':
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return i
}
