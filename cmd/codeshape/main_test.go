package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/internal/codeshape"
)

func TestRunPrintsJSONAndTable(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var js bytes.Buffer
	if err := run(&js, root, false, 0); err != nil {
		t.Fatal(err)
	}
	var m codeshape.Metrics
	if err := json.Unmarshal(js.Bytes(), &m); err != nil {
		t.Fatalf("JSON output does not parse: %v", err)
	}
	if m.Files == 0 || m.FuncsOver300 != len(m.LongFuncs) {
		t.Errorf("implausible measurement: files=%d funcs_over_300=%d long_funcs=%d", m.Files, m.FuncsOver300, len(m.LongFuncs))
	}
	var tab bytes.Buffer
	if err := run(&tab, root, true, 3); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"funcs over 300 lines", "effects.Host methods", "effects.Ctx literals", "functions over 300 lines:"} {
		if !strings.Contains(tab.String(), want) {
			t.Errorf("table output lacks %q:\n%s", want, tab.String())
		}
	}
}
