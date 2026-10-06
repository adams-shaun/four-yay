package manabrew

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

//go:generate go run ./internal/schemagen -dir .

// schema is one node of the typed JSON schema graph Decode walks to report
// unknown fields. schema_gen.go builds it from this package's source
// (internal/schemagen), so no runtime reflection is involved. A nil *schema
// is a leaf: the walker never descends into it.
type schema struct {
	kind   schemaKind
	fields map[string]*schema // schemaStruct: JSON key -> field schema
	elem   *schema            // schemaList, schemaMap: element schema
}

type schemaKind uint8

const (
	schemaStruct schemaKind = iota + 1
	schemaList
	schemaMap
)

// Encode emits canonical ManaBrew JSON: compact (no insignificant whitespace),
// struct field order preserved, and object keys of maps sorted by
// encoding/json. Callers that need a byte-stable wire form use Encode; Decode
// is its lenient counterpart.
func Encode(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Decode parses JSON leniently into v and returns the sorted JSON paths of
// every field the target's schema does not model. Unknown fields never reach
// the decoded value (encoding/json ignores them), so the returned paths are
// the only trace of them; the ManaBrew doc's own examples are inconsistent,
// which is why unknown fields are reported, not rejected.
//
// The paths are dotted ("action.output.extra"), with "[n]" indices for array
// elements. v must be a non-nil pointer.
func Decode(data []byte, v any) ([]string, error) {
	if v == nil {
		return nil, fmt.Errorf("manabrew: Decode target is nil")
	}
	root, known := schemaOfTarget(v)
	var raw any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(&raw); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, fmt.Errorf("manabrew: multiple JSON values in one document")
	}
	if err := json.Unmarshal(data, v); err != nil {
		var inv *json.InvalidUnmarshalError
		if errors.As(err, &inv) {
			return nil, fmt.Errorf("manabrew: Decode target must be a non-nil pointer")
		}
		return nil, err
	}
	if !known {
		// A target outside this package's schema graph (a non-struct or
		// foreign type) decodes, but has no schema to report unknowns by.
		return nil, nil
	}
	paths := unknownPaths(raw, root, "")
	sort.Strings(paths)
	return paths, nil
}

// unknownPaths walks the parsed JSON against the schema graph and collects
// the paths of fields the schema does not model. A union carrier is resolved
// from its discriminator before the walk descends.
func unknownPaths(value any, s *schema, path string) []string {
	if s == nil {
		return nil
	}
	if s.kind == schemaStruct {
		if m, ok := value.(map[string]any); ok {
			if key, concrete := resolveUnionType(s, m); concrete != nil {
				// Walk the union's concrete schema with the discriminator key
				// itself removed: the concrete struct does not re-declare it.
				filtered := make(map[string]any, len(m))
				for k, v := range m {
					if k != key {
						filtered[k] = v
					}
				}
				return unknownPaths(filtered, concrete, path)
			}
		}
	}
	switch s.kind {
	case schemaStruct:
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		var out []string
		for k, v := range m { // collected into one slice, then sorted by Decode
			p := k
			if path != "" {
				p = path + "." + k
			}
			ft, ok := s.fields[k]
			if !ok {
				out = append(out, p)
				continue
			}
			out = append(out, unknownPaths(v, ft, p)...)
		}
		return out
	case schemaList:
		a, ok := value.([]any)
		if !ok {
			return nil
		}
		var out []string
		for i, v := range a {
			out = append(out, unknownPaths(v, s.elem, fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	case schemaMap:
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		var out []string
		for k, v := range m { // ditto: sorted by Decode before return
			p := k
			if path != "" {
				p = path + "." + k
			}
			out = append(out, unknownPaths(v, s.elem, p)...)
		}
		return out
	default:
		return nil
	}
}

// resolveUnionType maps a union carrier schema plus its raw JSON object to
// the discriminator key it resolved on and the concrete schema that key
// selects. It returns nil when the discriminator is absent or unknown (the
// strict codecs report that; the walker stays silent).
func resolveUnionType(s *schema, m map[string]any) (string, *schema) {
	switch s {
	case schemaPromptInput:
		if t, ok := m["type"].(string); ok {
			if newCase, ok := promptInputCases[t]; ok {
				if cs, ok := schemaOfTarget(newCase()); ok {
					return "type", cs
				}
			}
		}
	case schemaPromptOutputData:
		if t, ok := m["type"].(string); ok {
			if newCase, ok := promptOutputCases[t]; ok {
				if cs, ok := schemaOfTarget(newCase()); ok {
					return "type", cs
				}
			}
		}
	case schemaClientMessage:
		switch m["kind"] {
		case "response":
			return "kind", schemaClientResponse
		case "directive":
			return "kind", schemaClientDirective
		}
	case schemaEngineMessage:
		switch m["kind"] {
		case "state":
			return "kind", schemaStateUpdate
		case "stateDelta":
			return "kind", schemaStateDelta
		case "display":
			return "kind", schemaDisplayMessage
		case "prompt":
			return "kind", schemaPromptMessage
		case "error":
			return "kind", schemaErrorMessage
		}
	case schemaCardView:
		if m["visibility"] == "hidden" {
			return "visibility", schemaHiddenCard
		}
		return "visibility", schemaVisibleCard
	}
	return "", nil
}
