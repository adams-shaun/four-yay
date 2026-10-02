package mzbridge

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// The subset of MessagePack the inference protocol needs. A request is
//
//	{"indices": [raw feature ids of every bag, concatenated], "offsets": [start of each bag]}
//
// packed the way RemoteModelEvaluator.java:196-204 packs it (smallest integer
// encodings). The response (server.py:139-156) is one map for a single bag
// and an array of maps for several, each map holding policy_player,
// policy_opponent, policy_target (A logits), policy_binary (2 logits) and
// value; the server sends every number as a float64, and the decoder also
// accepts float32 and integers.

func appendMapHeader(b []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(b, 0x80|byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xde), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, 0xdf), uint32(n))
}

func appendArrayHeader(b []byte, n int) []byte {
	switch {
	case n <= 15:
		return append(b, 0x90|byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xdc), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, 0xdd), uint32(n))
}

func appendString(b []byte, s string) []byte {
	switch n := len(s); {
	case n <= 31:
		b = append(b, 0xa0|byte(n))
	case n <= math.MaxUint8:
		b = append(b, 0xd9, byte(n))
	case n <= math.MaxUint16:
		b = binary.BigEndian.AppendUint16(append(b, 0xda), uint16(n))
	default:
		b = binary.BigEndian.AppendUint32(append(b, 0xdb), uint32(n))
	}
	return append(b, s...)
}

func appendInt(b []byte, v int64) []byte {
	switch {
	case v >= 0 && v <= 127:
		return append(b, byte(v))
	case v >= 0 && v <= math.MaxUint8:
		return append(b, 0xcc, byte(v))
	case v >= 0 && v <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xcd), uint16(v))
	case v >= 0 && v <= math.MaxUint32:
		return binary.BigEndian.AppendUint32(append(b, 0xce), uint32(v))
	case v >= 0:
		return binary.BigEndian.AppendUint64(append(b, 0xcf), uint64(v))
	case v >= -32:
		return append(b, byte(v))
	case v >= math.MinInt8:
		return append(b, 0xd0, byte(v))
	case v >= math.MinInt16:
		return binary.BigEndian.AppendUint16(append(b, 0xd1), uint16(v))
	case v >= math.MinInt32:
		return binary.BigEndian.AppendUint32(append(b, 0xd2), uint32(v))
	}
	return binary.BigEndian.AppendUint64(append(b, 0xd3), uint64(v))
}

// AppendEvaluateRequest appends the /evaluate request body for bags, each a
// state's feature ids, to dst.
func AppendEvaluateRequest(dst []byte, bags [][]int32) []byte {
	total := 0
	for _, bag := range bags {
		total += len(bag)
	}
	dst = appendMapHeader(dst, 2)
	dst = appendString(dst, "indices")
	dst = appendArrayHeader(dst, total)
	for _, bag := range bags {
		for _, id := range bag {
			dst = appendInt(dst, int64(id))
		}
	}
	dst = appendString(dst, "offsets")
	dst = appendArrayHeader(dst, len(bags))
	pos := 0
	for _, bag := range bags {
		dst = appendInt(dst, int64(pos))
		pos += len(bag)
	}
	return dst
}

// Evaluation is the network's output for one state.
type Evaluation struct {
	// PolicyPlayer, PolicyOpponent and PolicyTarget are A raw logits,
	// indexed by Vocab.ActionIndex / TargetIndex. PolicyBinary is the two
	// CHOOSE_USE logits (no, yes).
	PolicyPlayer   []float32
	PolicyOpponent []float32
	PolicyTarget   []float32
	PolicyBinary   []float32
	// Value is the tanh value head, in [-1, 1], from the encoded
	// perspective ("Player" in the feature tree).
	Value float32
}

var errTruncated = errors.New("mzbridge: msgpack: truncated")

// msgReader decodes MessagePack from a byte slice.
type msgReader struct {
	b   []byte
	pos int
}

func (r *msgReader) need(n int) ([]byte, error) {
	if n < 0 || len(r.b)-r.pos < n {
		return nil, errTruncated
	}
	p := r.b[r.pos : r.pos+n]
	r.pos += n
	return p, nil
}

func (r *msgReader) byte() (byte, error) {
	p, err := r.need(1)
	if err != nil {
		return 0, err
	}
	return p[0], nil
}

func (r *msgReader) peek() (byte, error) {
	if r.pos >= len(r.b) {
		return 0, errTruncated
	}
	return r.b[r.pos], nil
}

func (r *msgReader) uintN(n int) (uint64, error) {
	p, err := r.need(n)
	if err != nil {
		return 0, err
	}
	var v uint64
	for _, c := range p {
		v = v<<8 | uint64(c)
	}
	return v, nil
}

// length reads a container or string header from one of three encodings:
// a fix form (mask/bits), a 16-bit form and a 32-bit form; str and bin also
// have an 8-bit form.
func (r *msgReader) arrayLen() (int, error) {
	c, err := r.byte()
	if err != nil {
		return 0, err
	}
	switch {
	case c&0xf0 == 0x90:
		return int(c & 0x0f), nil
	case c == 0xdc:
		v, err := r.uintN(2)
		return int(v), err
	case c == 0xdd:
		v, err := r.uintN(4)
		return int(v), err
	}
	return 0, fmt.Errorf("mzbridge: msgpack: want array, got type byte %#x", c)
}

func (r *msgReader) mapLen() (int, error) {
	c, err := r.byte()
	if err != nil {
		return 0, err
	}
	switch {
	case c&0xf0 == 0x80:
		return int(c & 0x0f), nil
	case c == 0xde:
		v, err := r.uintN(2)
		return int(v), err
	case c == 0xdf:
		v, err := r.uintN(4)
		return int(v), err
	}
	return 0, fmt.Errorf("mzbridge: msgpack: want map, got type byte %#x", c)
}

func (r *msgReader) str() (string, error) {
	c, err := r.byte()
	if err != nil {
		return "", err
	}
	var n uint64
	switch {
	case c&0xe0 == 0xa0:
		n = uint64(c & 0x1f)
	case c == 0xd9 || c == 0xc4:
		n, err = r.uintN(1)
	case c == 0xda || c == 0xc5:
		n, err = r.uintN(2)
	case c == 0xdb || c == 0xc6:
		n, err = r.uintN(4)
	default:
		return "", fmt.Errorf("mzbridge: msgpack: want string, got type byte %#x", c)
	}
	if err != nil {
		return "", err
	}
	p, err := r.need(int(n))
	return string(p), err
}

// number reads any integer or float as a float64, and any integer as an
// int64 (ok reports whether the value was an integer).
func (r *msgReader) number() (f float64, i int64, isInt bool, err error) {
	c, err := r.byte()
	if err != nil {
		return 0, 0, false, err
	}
	var u uint64
	switch {
	case c <= 0x7f:
		return float64(c), int64(c), true, nil
	case c >= 0xe0:
		return float64(int8(c)), int64(int8(c)), true, nil
	case c == 0xca:
		u, err = r.uintN(4)
		return float64(math.Float32frombits(uint32(u))), 0, false, err
	case c == 0xcb:
		u, err = r.uintN(8)
		return math.Float64frombits(u), 0, false, err
	case c >= 0xcc && c <= 0xcf:
		u, err = r.uintN(1 << (c - 0xcc))
		return float64(u), int64(u), true, err
	case c == 0xd0:
		u, err = r.uintN(1)
		return float64(int8(u)), int64(int8(u)), true, err
	case c == 0xd1:
		u, err = r.uintN(2)
		return float64(int16(u)), int64(int16(u)), true, err
	case c == 0xd2:
		u, err = r.uintN(4)
		return float64(int32(u)), int64(int32(u)), true, err
	case c == 0xd3:
		u, err = r.uintN(8)
		return float64(int64(u)), int64(u), true, err
	}
	return 0, 0, false, fmt.Errorf("mzbridge: msgpack: want number, got type byte %#x", c)
}

func (r *msgReader) float32s() ([]float32, error) {
	n, err := r.arrayLen()
	if err != nil {
		return nil, err
	}
	if n > len(r.b)-r.pos { // every element takes at least a byte
		return nil, errTruncated
	}
	out := make([]float32, n)
	for i := range out {
		f, _, _, err := r.number()
		if err != nil {
			return nil, err
		}
		out[i] = float32(f)
	}
	return out, nil
}

func (r *msgReader) ints() ([]int64, error) {
	n, err := r.arrayLen()
	if err != nil {
		return nil, err
	}
	if n > len(r.b)-r.pos {
		return nil, errTruncated
	}
	out := make([]int64, n)
	for i := range out {
		_, v, isInt, err := r.number()
		if err != nil {
			return nil, err
		}
		if !isInt {
			return nil, errors.New("mzbridge: msgpack: want integer, got float")
		}
		out[i] = v
	}
	return out, nil
}

// skip consumes one value of any type.
func (r *msgReader) skip(depth int) error {
	if depth > 32 {
		return errors.New("mzbridge: msgpack: nested too deep")
	}
	c, err := r.peek()
	if err != nil {
		return err
	}
	switch {
	case c == 0xc0 || c == 0xc2 || c == 0xc3:
		r.pos++
		return nil
	case c <= 0x7f || c >= 0xe0 || (c >= 0xca && c <= 0xd3):
		_, _, _, err = r.number()
		return err
	case c&0xe0 == 0xa0 || (c >= 0xd9 && c <= 0xdb) || (c >= 0xc4 && c <= 0xc6):
		_, err = r.str()
		return err
	case c&0xf0 == 0x90 || c == 0xdc || c == 0xdd:
		n, err := r.arrayLen()
		for i := 0; i < n && err == nil; i++ {
			err = r.skip(depth + 1)
		}
		return err
	case c&0xf0 == 0x80 || c == 0xde || c == 0xdf:
		n, err := r.mapLen()
		for i := 0; i < 2*n && err == nil; i++ {
			err = r.skip(depth + 1)
		}
		return err
	}
	return fmt.Errorf("mzbridge: msgpack: unsupported type byte %#x", c)
}

func (r *msgReader) evaluation() (Evaluation, error) {
	var e Evaluation
	n, err := r.mapLen()
	if err != nil {
		return e, err
	}
	haveValue := false
	for i := 0; i < n; i++ {
		key, err := r.str()
		if err != nil {
			return e, err
		}
		switch key {
		case "policy_player":
			e.PolicyPlayer, err = r.float32s()
		case "policy_opponent":
			e.PolicyOpponent, err = r.float32s()
		case "policy_target":
			e.PolicyTarget, err = r.float32s()
		case "policy_binary":
			e.PolicyBinary, err = r.float32s()
		case "value":
			var f float64
			f, _, _, err = r.number()
			e.Value, haveValue = float32(f), true
		default:
			err = r.skip(0)
		}
		if err != nil {
			return e, fmt.Errorf("%w (key %q)", err, key)
		}
	}
	if !haveValue {
		return e, errors.New("mzbridge: evaluate response has no value")
	}
	return e, nil
}

// DecodeEvaluateResponse decodes the response to a request of the given
// number of bags: an array of that many maps, or a bare map for one bag.
func DecodeEvaluateResponse(b []byte, bags int) ([]Evaluation, error) {
	r := &msgReader{b: b}
	c, err := r.peek()
	if err != nil {
		return nil, err
	}
	n := 1
	if isMap := c&0xf0 == 0x80 || c == 0xde || c == 0xdf; !isMap {
		if n, err = r.arrayLen(); err != nil {
			return nil, err
		}
	}
	if n != bags {
		return nil, fmt.Errorf("mzbridge: sent %d bags, the server answered %d", bags, n)
	}
	out := make([]Evaluation, n)
	for i := range out {
		if out[i], err = r.evaluation(); err != nil {
			return nil, err
		}
	}
	if r.pos != len(b) {
		return nil, fmt.Errorf("mzbridge: msgpack: %d trailing bytes", len(b)-r.pos)
	}
	return out, nil
}

// DecodeEvaluateRequest is the server half of AppendEvaluateRequest, for
// stub servers: it returns the request's bags.
func DecodeEvaluateRequest(b []byte) ([][]int32, error) {
	r := &msgReader{b: b}
	n, err := r.mapLen()
	if err != nil {
		return nil, err
	}
	var indices, offsets []int64
	for i := 0; i < n; i++ {
		key, err := r.str()
		if err != nil {
			return nil, err
		}
		switch key {
		case "indices":
			indices, err = r.ints()
		case "offsets":
			offsets, err = r.ints()
		default:
			err = r.skip(0)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(offsets) == 0 { // server.py: no offsets means one bag
		offsets = []int64{0}
	}
	bags := make([][]int32, len(offsets))
	for i, start := range offsets {
		end := int64(len(indices))
		if i+1 < len(offsets) {
			end = offsets[i+1]
		}
		if start < 0 || end < start || end > int64(len(indices)) {
			return nil, fmt.Errorf("mzbridge: bad bag offsets %v for %d indices", offsets, len(indices))
		}
		bags[i] = make([]int32, 0, end-start)
		for _, v := range indices[start:end] {
			bags[i] = append(bags[i], int32(v))
		}
	}
	return bags, nil
}

func appendFloat64s(b []byte, v []float32) []byte {
	b = appendArrayHeader(b, len(v))
	for _, x := range v {
		b = binary.BigEndian.AppendUint64(append(b, 0xcb), math.Float64bits(float64(x)))
	}
	return b
}

// AppendEvaluateResponse is the server half of DecodeEvaluateResponse, for
// stub servers: it encodes evals the way server.py does, every number a
// float64, one bare map for a single bag and an array of maps otherwise.
func AppendEvaluateResponse(dst []byte, evals []Evaluation) []byte {
	if len(evals) != 1 {
		dst = appendArrayHeader(dst, len(evals))
	}
	for _, e := range evals {
		dst = appendMapHeader(dst, 5)
		dst = appendFloat64s(appendString(dst, "policy_player"), e.PolicyPlayer)
		dst = appendFloat64s(appendString(dst, "policy_opponent"), e.PolicyOpponent)
		dst = appendFloat64s(appendString(dst, "policy_target"), e.PolicyTarget)
		dst = appendFloat64s(appendString(dst, "policy_binary"), e.PolicyBinary)
		dst = appendString(dst, "value")
		dst = binary.BigEndian.AppendUint64(append(dst, 0xcb), math.Float64bits(float64(e.Value)))
	}
	return dst
}
