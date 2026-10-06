package v2agent

import (
	"fmt"
	"sort"
)

// UnknownFields lists, as JSON paths, every object key in raw that the Go
// type of target does not model. Keys under json.RawMessage fields and maps
// (seat_decision.extensions, counters) are not inspected. Mistyped values
// are not reported here; the lenient decode reports them.
//
// The type's shape comes from a generated schema table (unknown_gen.go),
// not reflection, so target must be one of the request types the schema
// was generated for (unknownRoot); any other is a programming error and
// panics. TestUnknownSchemaIsCurrent regenerates the table from the Go
// types and fails when it is stale, and TestUnknownFieldsIsReflective holds
// the result to the reflective walk on the recorded transcripts.
func UnknownFields(raw []byte, target any) []string {
	root := unknownRoot(target)
	v, err := decodeAny(raw)
	if err != nil {
		return nil
	}
	var out []string
	walkUnknown(v, root, "$", &out)
	return out
}

// unknownRoot is the schema node of target's type.
func unknownRoot(target any) int32 {
	switch target.(type) {
	case *GameStart:
		return unknownRootGameStart
	case *gameStartRequest:
		return unknownRootGameStartRequest
	case *Semantic:
		return unknownRootSemantic
	case *SeatDecision:
		return unknownRootSeatDecision
	}
	panic(fmt.Sprintf("v2agent: UnknownFields has no schema for %T", target))
}

// gameStartRequest is the game_start line as the strict check reads it:
// the envelope fields beside GameStart's own.
type gameStartRequest struct {
	envelope
	GameStart
}

// unknownNode is one Go type's JSON shape: an opaque value (a scalar, an
// interface, a json.RawMessage) whose contents are not inspected, an object
// with known keys, or a list or map of elem.
type unknownNode struct {
	kind   unknownKind
	elem   int32          // list, map: the element's node
	fields []unknownField // object: sorted by name
}

type unknownKind uint8

const (
	unknownOpaque unknownKind = iota
	unknownObject
	unknownList
	unknownMap
)

type unknownField struct {
	name string
	node int32
}

func (n *unknownNode) field(name string) (int32, bool) {
	i := sort.Search(len(n.fields), func(i int) bool { return n.fields[i].name >= name })
	if i < len(n.fields) && n.fields[i].name == name {
		return n.fields[i].node, true
	}
	return 0, false
}

func walkUnknown(v any, node int32, path string, out *[]string) {
	n := &unknownNodes[node]
	switch n.kind {
	case unknownObject:
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ft, known := n.field(k)
			if !known {
				*out = append(*out, path+"."+k)
				continue
			}
			walkUnknown(obj[k], ft, path+"."+k, out)
		}
	case unknownList:
		arr, ok := v.([]any)
		if !ok {
			return
		}
		for i, e := range arr {
			walkUnknown(e, n.elem, path+"["+itoa(i)+"]", out)
		}
	case unknownMap:
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkUnknown(obj[k], n.elem, path+"."+k, out)
		}
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
