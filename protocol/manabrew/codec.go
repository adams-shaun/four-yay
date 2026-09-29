package manabrew

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
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
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, fmt.Errorf("manabrew: Decode target must be a non-nil pointer")
	}
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
		return nil, err
	}
	paths := unknownPaths(raw, rv.Elem().Type(), "")
	sort.Strings(paths)
	return paths, nil
}

// unknownPaths walks the parsed JSON against the Go schema and collects the
// paths of fields the schema does not model. A union carrier is resolved from
// its discriminator before the walk descends.
func unknownPaths(value any, t reflect.Type, path string) []string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		if m, ok := value.(map[string]any); ok {
			if key, concrete := resolveUnionType(t, m); concrete != nil {
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
	switch t.Kind() {
	case reflect.Struct:
		m, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := structFields(t)
		var out []string
		for k, v := range m { // collected into one slice, then sorted by Decode
			p := k
			if path != "" {
				p = path + "." + k
			}
			ft, ok := fields[k]
			if !ok {
				out = append(out, p)
				continue
			}
			out = append(out, unknownPaths(v, ft, p)...)
		}
		return out
	case reflect.Slice, reflect.Array:
		a, ok := value.([]any)
		if !ok {
			return nil
		}
		var out []string
		for i, v := range a {
			out = append(out, unknownPaths(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	case reflect.Map:
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
			out = append(out, unknownPaths(v, t.Elem(), p)...)
		}
		return out
	default:
		return nil
	}
}

// resolveUnionType maps a union carrier type plus its raw JSON object to the
// discriminator key it resolved on and the concrete schema that key selects.
// It returns nil when the discriminator is absent or unknown (the strict
// codecs report that; the walker stays silent).
func resolveUnionType(t reflect.Type, m map[string]any) (string, reflect.Type) {
	switch t {
	case reflect.TypeOf(PromptInput{}):
		if s, ok := m["type"].(string); ok {
			if newCase, ok := promptInputCases[s]; ok {
				return "type", reflect.TypeOf(newCase())
			}
		}
	case reflect.TypeOf(PromptOutputData{}):
		if s, ok := m["type"].(string); ok {
			if newCase, ok := promptOutputCases[s]; ok {
				return "type", reflect.TypeOf(newCase())
			}
		}
	case reflect.TypeOf(ClientMessage{}):
		switch m["kind"] {
		case "response":
			return "kind", reflect.TypeOf(ClientResponse{})
		case "directive":
			return "kind", reflect.TypeOf(ClientDirective{})
		}
	case reflect.TypeOf(EngineMessage{}):
		switch m["kind"] {
		case "state":
			return "kind", reflect.TypeOf(StateUpdate{})
		case "stateDelta":
			return "kind", reflect.TypeOf(StateDelta{})
		case "display":
			return "kind", reflect.TypeOf(DisplayMessage{})
		case "prompt":
			return "kind", reflect.TypeOf(PromptMessage{})
		case "error":
			return "kind", reflect.TypeOf(ErrorMessage{})
		}
	case reflect.TypeOf(CardView{}):
		if m["visibility"] == "hidden" {
			return "visibility", reflect.TypeOf(HiddenCard{})
		}
		return "visibility", reflect.TypeOf(VisibleCard{})
	}
	return "", nil
}

// structFields maps each JSON key of a struct type to its field type,
// flattening anonymous embedded structs the way encoding/json does.
func structFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			if f.Anonymous {
				ft := f.Type
				for ft.Kind() == reflect.Pointer {
					ft = ft.Elem()
				}
				if ft.Kind() == reflect.Struct {
					for k, v := range structFields(ft) {
						fields[k] = v
					}
				}
			}
			continue
		}
		fields[name] = f.Type
	}
	return fields
}
