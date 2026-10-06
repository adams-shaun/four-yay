//go:build manabrew

package manabrew

import (
	"reflect"
	"strings"
	"testing"
)

// TestGeneratedSchemaMatchesReflectiveWalk holds the generated schema graph
// to the reflective field walk it replaced: from every Decode root and union
// member, each struct's JSON key set and each key's container shape must
// agree (tests may still use reflect; production code may not).
func TestGeneratedSchemaMatchesReflectiveWalk(t *testing.T) {
	roots := []any{&ClientMessage{}, &EngineMessage{}, &AgentPrompt{}, &CardView{},
		&ClientResponse{}, &ClientDirective{}, &StateUpdate{}, &StateDelta{}, &DisplayMessage{},
		&PromptMessage{}, &ErrorMessage{}, &HiddenCard{}, &VisibleCard{}, &PromptInput{}, &PromptOutputData{}}
	for _, newCase := range promptInputCases {
		roots = append(roots, newCase())
	}
	for _, newCase := range promptOutputCases {
		roots = append(roots, newCase())
	}
	seen := map[reflect.Type]bool{}
	for _, r := range roots {
		s, ok := schemaOfTarget(r)
		if !ok {
			t.Fatalf("schemaOfTarget(%T) not ok", r)
		}
		compareSchema(t, reflect.TypeOf(r).Elem().Name(), reflect.TypeOf(r), s, seen)
	}
	if _, ok := schemaOfTarget((*ClientMessage)(nil)); ok {
		t.Error("schemaOfTarget accepted a nil pointer")
	}
}

// reflectFields is the deleted reflective structFields, verbatim in effect.
func reflectFields(t reflect.Type) map[string]reflect.Type {
	fields := make(map[string]reflect.Type)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
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
					for k, v := range reflectFields(ft) {
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

// leaf reports whether the reflective walker would report nothing below t.
func leaf(t reflect.Type, depth int) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return false
	case reflect.Slice, reflect.Array, reflect.Map:
		if depth > 8 {
			return false
		}
		return leaf(t.Elem(), depth+1)
	}
	return true
}

func compareSchema(t *testing.T, path string, rt reflect.Type, s *schema, seen map[reflect.Type]bool) {
	t.Helper()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	if leaf(rt, 0) {
		if s != nil {
			t.Errorf("%s: reflect leaf %v, generated schema kind %d", path, rt, s.kind)
		}
		return
	}
	if s == nil {
		t.Errorf("%s: generated leaf, reflect %v", path, rt)
		return
	}
	switch rt.Kind() {
	case reflect.Struct:
		if s.kind != schemaStruct {
			t.Errorf("%s: %v is a struct, schema kind %d", path, rt, s.kind)
			return
		}
		if seen[rt] {
			return
		}
		seen[rt] = true
		want := reflectFields(rt)
		if len(want) != len(s.fields) {
			t.Errorf("%s: %v has %d json keys, schema %d", path, rt, len(want), len(s.fields))
		}
		for k, ft := range want {
			fs, ok := s.fields[k]
			if !ok {
				t.Errorf("%s.%s: missing from generated schema", path, k)
				continue
			}
			compareSchema(t, path+"."+k, ft, fs, seen)
		}
	case reflect.Slice, reflect.Array:
		if s.kind != schemaList {
			t.Errorf("%s: %v is a list, schema kind %d", path, rt, s.kind)
			return
		}
		compareSchema(t, path+"[]", rt.Elem(), s.elem, seen)
	case reflect.Map:
		if s.kind != schemaMap {
			t.Errorf("%s: %v is a map, schema kind %d", path, rt, s.kind)
			return
		}
		compareSchema(t, path+"{}", rt.Elem(), s.elem, seen)
	}
}
