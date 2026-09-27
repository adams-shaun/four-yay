package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestHelloAutoManaWireFlag pins the hello frame's TableInfo.auto_mana field
// on the wire. AutoMana is not `omitempty` (protocol/protocol.go): the zero
// value false is a real value -- "auto-mana off" -- so the key must be
// emitted even when false, or a client cannot tell "off" from "field not
// present". The checked-in hello golden is the committed wire shape; a
// regression that drops the field (the post-payment-plan main red this task
// pins) must fail here.
func TestHelloAutoManaWireFlag(t *testing.T) {
	// Locate the hello fixture the goldens use, so this test reads exactly
	// the frame TestGoldens pins.
	var hello Frame
	found := false
	for _, f := range fixtures(t) {
		if f.T == THello {
			hello = f
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixtures(t) produced no THello frame; this test reads the hello wire shape")
	}

	// Decode the hello body to reach the table. Marshal the body alone: the
	// body is where auto_mana lives.
	marshalTableAutoMana := func(f Frame) (present bool, value bool, raw []byte, tables map[string]any) {
		t.Helper()
		var h Hello
		if err := f.Decode(&h); err != nil {
			t.Fatalf("decode hello body: %v", err)
		}
		if len(h.Tables) != 1 {
			t.Fatalf("hello fixture has %d tables; this test pins the one table's auto_mana", len(h.Tables))
		}
		raw, err := json.Marshal(h.Tables[0])
		if err != nil {
			t.Fatalf("marshal table: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("unmarshal table map: %v", err)
		}
		v, ok := m["auto_mana"]
		b, isBool := v.(bool)
		if ok && !isBool {
			t.Fatalf("auto_mana present but not a JSON boolean: %T %v", v, v)
		}
		return ok, b, raw, m
	}

	// (1) The false case: the fixture's table has the zero value, and the
	// serialized object MUST carry auto_mana:false (not merely omit it).
	var h0 Hello
	if err := hello.Decode(&h0); err != nil {
		t.Fatalf("decode hello body: %v", err)
	}
	if h0.Tables[0].AutoMana {
		t.Fatal("precondition: hello fixture table AutoMana is true; want the default false to pin false-present")
	}
	present, value, rawFalse, _ := marshalTableAutoMana(hello)
	if !present {
		t.Fatalf("hello table JSON omits auto_mana (raw=%s); false is a real value and must serialize", rawFalse)
	}
	if value {
		t.Fatalf("hello table JSON auto_mana = true; want false")
	}

	// (2) The true case: flipping the flag must change the wire value.
	h1 := h0
	h1.Tables[0].AutoMana = true
	if !h1.Tables[0].AutoMana {
		t.Fatal("precondition: failed to set AutoMana true on the source table")
	}
	tTrue, _ := NewFrame(THello, hello.Table, hello.Match, hello.Seq, h1)
	presentTrue, valueTrue, rawTrue, _ := marshalTableAutoMana(tTrue)
	if !presentTrue {
		t.Fatalf("hello table JSON with AutoMana=true omits auto_mana (raw=%s)", rawTrue)
	}
	if !valueTrue {
		t.Fatalf("hello table JSON with AutoMana=true auto_mana = false; want true")
	}
	// Precondition: the two wire values actually differ, so the assertions
	// above cannot both pass on a constant.
	if bytes.Equal(rawFalse, rawTrue) {
		t.Fatalf("auto_mana=false and auto_mana=true serialized identically (%s); assertion is vacuous", rawFalse)
	}

	// (3) The checked-in golden carries auto_mana:false. Parse the committed
	// bytes (not the fixture) so a golden that dropped the key fails here.
	gold, err := os.ReadFile(filepath.Join("testdata", "hello.json"))
	if err != nil {
		t.Fatalf("read hello golden: %v", err)
	}
	var doc struct {
		Body struct {
			Tables []map[string]any `json:"tables"`
		} `json:"body"`
	}
	if err := json.Unmarshal(gold, &doc); err != nil {
		t.Fatalf("parse hello golden: %v", err)
	}
	if len(doc.Body.Tables) != 1 {
		t.Fatalf("hello golden has %d tables; want 1", len(doc.Body.Tables))
	}
	gv, ok := doc.Body.Tables[0]["auto_mana"]
	if !ok {
		t.Fatal("committed hello golden omits auto_mana; it must serialize the false zero value")
	}
	gb, isBool := gv.(bool)
	if !isBool || gb {
		t.Fatalf("committed hello golden auto_mana = %v (%T); want boolean false", gv, gv)
	}
}
