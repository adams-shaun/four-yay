package v2agent

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// UnknownFields lists, as JSON paths, every object key in raw that the Go
// type of target (a struct, or a pointer to one) does not model. Keys under
// json.RawMessage fields and maps (seat_decision.extensions, counters) are
// not inspected. Mistyped values are not reported here; the lenient decode
// reports them.
func UnknownFields(raw []byte, target any) []string {
	v, err := decodeAny(raw)
	if err != nil {
		return nil
	}
	var out []string
	walkUnknown(v, reflect.TypeOf(target), "$", &out)
	return out
}

var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

func walkUnknown(v any, t reflect.Type, path string, out *[]string) {
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
			walkUnknown(obj[k], ft, path+"."+k, out)
		}
	case reflect.Slice, reflect.Array:
		arr, ok := v.([]any)
		if !ok {
			return
		}
		for i, e := range arr {
			walkUnknown(e, t.Elem(), path+"["+itoa(i)+"]", out)
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
			walkUnknown(obj[k], t.Elem(), path+"."+k, out)
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

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
