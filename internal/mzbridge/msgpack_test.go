package mzbridge

import (
	"bytes"
	"encoding/binary"
	"math"
	"slices"
	"testing"
)

func TestEvaluateRequestBytes(t *testing.T) {
	got := AppendEvaluateRequest(nil, [][]int32{{1, 127, 128}, {}, {65535, 65536, 2147483646}})
	want := []byte{
		0x82,
		0xa7, 'i', 'n', 'd', 'i', 'c', 'e', 's',
		0x96, 0x01, 0x7f, 0xcc, 0x80, 0xcd, 0xff, 0xff, 0xce, 0x00, 0x01, 0x00, 0x00, 0xce, 0x7f, 0xff, 0xff, 0xfe,
		0xa7, 'o', 'f', 'f', 's', 'e', 't', 's',
		0x93, 0x00, 0x03, 0x03,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("request bytes\n got % x\nwant % x", got, want)
	}
	bags, err := DecodeEvaluateRequest(got)
	if err != nil || len(bags) != 3 || !slices.Equal(bags[0], []int32{1, 127, 128}) || len(bags[1]) != 0 ||
		!slices.Equal(bags[2], []int32{65535, 65536, 2147483646}) {
		t.Fatalf("round trip: %v %v", bags, err)
	}
	// a long bag takes the array16 / array32 headers
	long := make([]int32, 70000)
	for i := range long {
		long[i] = int32(i)
	}
	bags, err = DecodeEvaluateRequest(AppendEvaluateRequest(nil, [][]int32{long[:20], long}))
	if err != nil || len(bags) != 2 || !slices.Equal(bags[1], long) || !slices.Equal(bags[0], long[:20]) {
		t.Fatalf("long round trip: %d bags, %v", len(bags), err)
	}
}

func TestIntEncodings(t *testing.T) {
	for _, v := range []int64{0, 1, 127, 128, 255, 256, 65535, 65536, math.MaxUint32, math.MaxUint32 + 1, math.MaxInt64,
		-1, -32, -33, -128, -129, -32768, -32769, math.MinInt32, math.MinInt32 - 1, math.MinInt64} {
		r := &msgReader{b: appendInt(nil, v)}
		f, got, isInt, err := r.number()
		if err != nil || !isInt || got != v || f != float64(v) || r.pos != len(r.b) {
			t.Errorf("int %d -> % x -> %d (%v, int %v, %v)", v, r.b, got, f, isInt, err)
		}
	}
}

// packEval encodes one result the way server.py does: a map of float64
// lists and a float64 value (plus a key the client must skip).
func packEval(b []byte, e Evaluation) []byte {
	f64s := func(b []byte, v []float32) []byte {
		b = appendArrayHeader(b, len(v))
		for _, x := range v {
			b = binary.BigEndian.AppendUint64(append(b, 0xcb), math.Float64bits(float64(x)))
		}
		return b
	}
	b = appendMapHeader(b, 6)
	b = f64s(appendString(b, "policy_player"), e.PolicyPlayer)
	b = f64s(appendString(b, "policy_opponent"), e.PolicyOpponent)
	b = appendString(b, "debug")
	b = appendMapHeader(b, 1)
	b = appendString(b, "nested")
	b = appendArrayHeader(b, 3)
	b = append(b, 0xc0, 0xc3)
	b = appendString(b, "x")
	b = f64s(appendString(b, "policy_target"), e.PolicyTarget)
	b = f64s(appendString(b, "policy_binary"), e.PolicyBinary)
	b = appendString(b, "value")
	return binary.BigEndian.AppendUint64(append(b, 0xcb), math.Float64bits(float64(e.Value)))
}

func TestDecodeEvaluateResponse(t *testing.T) {
	e1 := Evaluation{PolicyPlayer: []float32{1, -2.5}, PolicyOpponent: []float32{0}, PolicyTarget: make([]float32, 1024), PolicyBinary: []float32{0.25, 0.75}, Value: -0.5}
	e1.PolicyTarget[1023] = 7
	e2 := Evaluation{PolicyPlayer: []float32{3}, PolicyOpponent: []float32{}, PolicyTarget: []float32{}, PolicyBinary: []float32{}, Value: 1}
	eq := func(a, b Evaluation) bool {
		return slices.Equal(a.PolicyPlayer, b.PolicyPlayer) && slices.Equal(a.PolicyOpponent, b.PolicyOpponent) &&
			slices.Equal(a.PolicyTarget, b.PolicyTarget) && slices.Equal(a.PolicyBinary, b.PolicyBinary) && a.Value == b.Value
	}
	// one bag: a bare map
	got, err := DecodeEvaluateResponse(packEval(nil, e1), 1)
	if err != nil || len(got) != 1 || !eq(got[0], e1) {
		t.Fatalf("single: %v %+v", err, got)
	}
	// several: an array of maps
	arr := packEval(packEval(appendArrayHeader(nil, 2), e1), e2)
	got, err = DecodeEvaluateResponse(arr, 2)
	if err != nil || len(got) != 2 || !eq(got[0], e1) || !eq(got[1], e2) {
		t.Fatalf("array: %v %+v", err, got)
	}
	// float32 and integer numbers are accepted too
	b := appendMapHeader(nil, 2)
	b = appendString(b, "policy_binary")
	b = appendArrayHeader(b, 2)
	b = binary.BigEndian.AppendUint32(append(b, 0xca), math.Float32bits(1.5))
	b = appendInt(b, -3)
	b = appendString(b, "value")
	b = appendInt(b, 1)
	got, err = DecodeEvaluateResponse(b, 1)
	if err != nil || !slices.Equal(got[0].PolicyBinary, []float32{1.5, -3}) || got[0].Value != 1 {
		t.Fatalf("mixed numbers: %v %+v", err, got)
	}

	if _, err := DecodeEvaluateResponse(arr, 3); err == nil {
		t.Error("bag count mismatch accepted")
	}
	if _, err := DecodeEvaluateResponse(packEval(nil, e1), 2); err == nil {
		t.Error("a bare map accepted for two bags")
	}
	if _, err := DecodeEvaluateResponse(append(packEval(nil, e1), 0), 1); err == nil {
		t.Error("trailing bytes accepted")
	}
	if _, err := DecodeEvaluateResponse(appendString(appendMapHeader(nil, 1), "policy_binary"), 1); err == nil {
		t.Error("truncated map accepted")
	}
	if _, err := DecodeEvaluateResponse(f64Array(appendString(appendMapHeader(nil, 1), "policy_binary")), 1); err == nil {
		t.Error("a response with no value accepted")
	}
	full := packEval(nil, e1)
	for n := 0; n < len(full); n += 97 {
		if _, err := DecodeEvaluateResponse(full[:n], 1); err == nil {
			t.Fatalf("truncation at %d accepted", n)
		}
	}
}

func f64Array(b []byte) []byte { return appendArrayHeader(b, 0) }

// TestEvaluateResponseRoundTrip: the stub-server encoder and the client
// decoder agree, in both response forms.
func TestEvaluateResponseRoundTrip(t *testing.T) {
	evals := []Evaluation{
		{PolicyPlayer: []float32{1, 2}, PolicyOpponent: []float32{3}, PolicyTarget: []float32{4}, PolicyBinary: []float32{0.5, -0.5}, Value: 0.25},
		{PolicyPlayer: []float32{}, PolicyOpponent: []float32{}, PolicyTarget: []float32{}, PolicyBinary: []float32{}, Value: -1},
	}
	for n := 1; n <= 2; n++ {
		b := AppendEvaluateResponse(nil, evals[:n])
		if isMap := b[0]&0xf0 == 0x80; isMap != (n == 1) {
			t.Fatalf("%d bags: first byte %#x", n, b[0])
		}
		got, err := DecodeEvaluateResponse(b, n)
		if err != nil || len(got) != n {
			t.Fatalf("%d bags: %v", n, err)
		}
		for i := range got {
			if !slices.Equal(got[i].PolicyPlayer, evals[i].PolicyPlayer) || !slices.Equal(got[i].PolicyBinary, evals[i].PolicyBinary) || got[i].Value != evals[i].Value {
				t.Fatalf("bag %d: %+v", i, got[i])
			}
		}
	}
}
