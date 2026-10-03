package events

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestEveryKindHasADescriptor is the one gate a new Kind meets. It replaces
// the scatter of lockstep failures a new Kind used to cause one at a time
// (TestEveryKindHasAName here, rules' TestTriggerEventInterestMapping and
// view's TestDescribeCoversEveryKind): every per-Kind fact those checked now
// comes from the kindInfo table, so a Kind appended without its entry fails
// here, once, saying where the entry goes.
func TestEveryKindHasADescriptor(t *testing.T) {
	seen := make(map[string]Kind, NumKinds)
	for k := Kind(0); int(k) < NumKinds; k++ {
		info, ok := Info(k)
		var problems []string
		if !ok {
			problems = append(problems, "Info reports no entry")
		}
		if info.Name == "" {
			problems = append(problems, "Name is empty")
		} else if prev, dup := seen[info.Name]; dup {
			problems = append(problems, fmt.Sprintf("Name %q is already Kind(%d)'s", info.Name, int(prev)))
		} else {
			seen[info.Name] = k
		}
		if info.Name != "" && !snakeCase(info.Name) {
			problems = append(problems, fmt.Sprintf("Name %q is not snake_case", info.Name))
		}
		if info.Trigger == TriggerUnset || int(info.Trigger) >= NumTriggerClasses {
			problems = append(problems, "Trigger is unset (choose TriggerNone for bookkeeping no trigger mode observes, "+
				"TriggerFullMatch for a trigger-relevant kind without a dedicated class, or a specific class)")
		}
		if err := checkDescribeTemplate(info.Describe); err != nil {
			problems = append(problems, "Describe "+err.Error())
		}
		if len(problems) > 0 {
			t.Errorf("Kind(%d) %q has an incomplete descriptor: %s.\n"+
				"Fix it in ONE place: its entry in events/kindinfo.go's kindInfo table "+
				"(Name, Trigger, and Describe unless view/describe.go renders the kind with its own case).",
				int(k), info.Name, strings.Join(problems, "; "))
		}
	}
	if got, want := Kind(NumKinds).String(), "unknown"; got != want {
		t.Fatalf("Kind(NumKinds).String() = %q, want %q", got, want)
	}
	if _, ok := Info(Kind(NumKinds)); ok {
		t.Fatal("Info(Kind(NumKinds)) reports an entry past the enum")
	}
	if got := Kind(NumKinds).Trigger(); got != TriggerUnset {
		t.Fatalf("Kind(NumKinds).Trigger() = %d, want TriggerUnset", got)
	}
}

func snakeCase(s string) bool {
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r == '_' && i > 0 && i < len(s)-1:
		default:
			return false
		}
	}
	return true
}

// checkDescribeTemplate accepts "" and any text whose {...} placeholders all
// name DescribeFields, with no stray brace.
func checkDescribeTemplate(tmpl string) error {
	for rest := tmpl; rest != ""; {
		open := strings.IndexAny(rest, "{}")
		if open < 0 {
			return nil
		}
		if rest[open] == '}' {
			return fmt.Errorf("template %q has an unmatched '}'", tmpl)
		}
		end := strings.IndexByte(rest[open:], '}')
		if end < 0 {
			return fmt.Errorf("template %q has an unterminated placeholder", tmpl)
		}
		name := rest[open+1 : open+end]
		if !slices.Contains(DescribeFields[:], name) {
			return fmt.Errorf("template %q uses {%s}; the fields are %v", tmpl, name, DescribeFields)
		}
		rest = rest[open+end+1:]
	}
	return nil
}

func TestCheckDescribeTemplateRejectsMalformedTemplates(t *testing.T) {
	for _, ok := range []string{"", "plain", "{player} taps {obj} for {amount}: {text}"} {
		if err := checkDescribeTemplate(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"{who} wins", "{player", "player}", "{}"} {
		if checkDescribeTemplate(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
