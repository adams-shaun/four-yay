package v2agent

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The reflective unknown-field walk UnknownFields used to be: kept in a test
// file as the oracle the generated schema (unknown_gen.go) is held to, and
// as the type walk unknown_gen_test.go generates that schema from.

// reflectUnknownFields is the reflective UnknownFields.
func reflectUnknownFields(raw []byte, target any) []string {
	v, err := decodeAny(raw)
	if err != nil {
		return nil
	}
	var out []string
	reflectWalkUnknown(v, reflect.TypeOf(target), "$", &out)
	return out
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

func reflectWalkUnknown(v any, t reflect.Type, path string, out *[]string) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t == rawMessageType {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		fields := jsonFields(t)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ft, known := fields[k]
			if !known {
				*out = append(*out, path+"."+k)
				continue
			}
			reflectWalkUnknown(obj[k], ft, path+"."+k, out)
		}
	case reflect.Slice, reflect.Array:
		arr, ok := v.([]any)
		if !ok {
			return
		}
		for i, e := range arr {
			reflectWalkUnknown(e, t.Elem(), path+"["+itoa(i)+"]", out)
		}
	case reflect.Map:
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
			reflectWalkUnknown(obj[k], t.Elem(), path+"."+k, out)
		}
	}
}

// jsonFields maps a struct's JSON keys to field types, flattening embedded
// structs the way encoding/json does.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			et := f.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				for k, v := range jsonFields(et) {
					if _, dup := fields[k]; !dup {
						fields[k] = v
					}
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields
}

var _ = testing.Short
