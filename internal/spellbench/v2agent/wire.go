package v2agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Protocol is the protocol value every v2 message carries (spec 4.1).
const Protocol = "spellbench/v2"

// MaxLineBytes is the 8 MiB line limit of spec 2. A line whose content
// (terminator excluded) is longer is rejected; this reader accepts content of
// exactly MaxLineBytes, one byte more than the python reference reader, which
// counts the "\n" against the limit (see the package report on spec 2).
const MaxLineBytes = 8 << 20

// ErrLineTooLong is returned by LineReader.ReadLine for a line over
// MaxLineBytes. The rest of that line has been discarded, so the next
// ReadLine starts at the next line (one response per request line, spec 2).
var ErrLineTooLong = errors.New("line exceeds 8 MiB")

// ErrUnterminated is returned for bytes that end at EOF without a "\n".
var ErrUnterminated = errors.New(`line missing "\n" terminator before EOF`)

// LineReader frames NDJSON lines (spec 2): "\n" terminated, "\r\n"
// tolerated, at most MaxLineBytes of content. It never buffers more than the
// limit, whatever a peer sends.
type LineReader struct {
	r     *bufio.Reader
	limit int
	buf   []byte
}

// NewLineReader reads lines of at most MaxLineBytes from r.
func NewLineReader(r io.Reader) *LineReader {
	return &LineReader{r: bufio.NewReaderSize(r, 64<<10), limit: MaxLineBytes}
}

// ReadLine returns the next line without its terminator. At a clean EOF it
// returns (nil, io.EOF). The returned slice is valid until the next call.
func (lr *LineReader) ReadLine() ([]byte, error) {
	lr.buf = lr.buf[:0]
	tooLong := false
	for {
		chunk, err := lr.r.ReadSlice('\n')
		if !tooLong {
			if len(lr.buf)+len(chunk) > lr.limit+2 { // content + "\r\n" at most
				tooLong = true
				lr.buf = lr.buf[:0]
			} else {
				lr.buf = append(lr.buf, chunk...)
			}
		}
		switch {
		case err == nil:
			if tooLong {
				return nil, ErrLineTooLong
			}
			line := lr.buf[:len(lr.buf)-1]
			if n := len(line); n > 0 && line[n-1] == '\r' {
				line = line[:n-1]
			}
			if len(line) > lr.limit {
				return nil, ErrLineTooLong
			}
			return line, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLong {
				return nil, ErrLineTooLong
			}
			if len(lr.buf) == 0 {
				return nil, io.EOF
			}
			return nil, ErrUnterminated
		default:
			return nil, err
		}
	}
}

// Canonical encodes v as canonical JSON (RFC 8785 as spec 4.3 profiles it):
// object keys sorted by code point, no whitespace, integers in shortest
// form, strings escaping only '"', '\\' and control characters. v may hold
// the values encoding/json produces with UseNumber (map[string]any, []any,
// string, json.Number, bool, nil), json.RawMessage (re-read and
// canonicalized), and Go integers; anything else is first marshalled with
// encoding/json and then canonicalized.
func Canonical(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := writeCanonical(&b, v, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// CanonicalLine is Canonical followed by "\n".
func CanonicalLine(v any) ([]byte, error) {
	out, err := Canonical(v)
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// decodeAny decodes raw JSON with json.Number for numbers.
func decodeAny(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after JSON value")
	}
	return v, nil
}

const maxCanonicalDepth = 128

func writeCanonical(b *bytes.Buffer, v any, depth int) error {
	if depth > maxCanonicalDepth {
		return errors.New("canonical: nesting too deep")
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeCanonicalString(b, x)
	case json.Number:
		writeCanonicalNumber(b, string(x))
	case int:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case uint32:
		b.WriteString(strconv.FormatUint(uint64(x), 10))
	case uint64:
		b.WriteString(strconv.FormatUint(x, 10))
	case json.RawMessage:
		inner, err := decodeAny(x)
		if err != nil {
			return fmt.Errorf("canonical: raw message: %w", err)
		}
		return writeCanonical(b, inner, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // byte order of UTF-8 is code point order
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonicalString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, x[k], depth+1); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, e, depth+1); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonicalString(b, e)
		}
		b.WriteByte(']')
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return fmt.Errorf("canonical: %w", err)
		}
		inner, err := decodeAny(raw)
		if err != nil {
			return fmt.Errorf("canonical: %w", err)
		}
		return writeCanonical(b, inner, depth)
	}
	return nil
}

// writeCanonicalNumber writes an integer literal in shortest decimal form.
// A non-integer literal (only a lenient reader could have produced one) is
// written as given.
func writeCanonicalNumber(b *bytes.Buffer, text string) {
	if n, ok := new(big.Int).SetString(text, 10); ok {
		b.WriteString(n.String())
		return
	}
	b.WriteString(text)
}

func writeCanonicalString(b *bytes.Buffer, s string) {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
	}
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}
