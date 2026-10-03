package compliance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// Manifest is one set's card list, taken from an XMage set class at a pinned
// XMage commit.
type Manifest struct {
	Code     string         `json:"code"`
	Name     string         `json:"name"`
	Released string         `json:"released"` // YYYY-MM-DD
	SetType  string         `json:"set_type"` // XMage SetType, e.g. EXPANSION
	XMageRef string         `json:"xmage_ref"`
	Cards    []ManifestCard `json:"cards"`
}

// ManifestCard is one distinct card name in a set, with every collector
// number it is printed under.
type ManifestCard struct {
	Name    string   `json:"name"`
	Numbers []string `json:"numbers"`
	Rarity  string   `json:"rarity"`
}

// The shapes every XMage set class uses, measured over all 589 classes at
// XMAGE_REF 6b602a1c (2026-10-02), after comments are stripped: every
// SetCardInfo entry is a literal name followed by an int or quoted collector
// number, and HasCon2017 alone qualifies the constructor as
// ExpansionSet.SetCardInfo. anyCardRe counts every spelling of the
// constructor, so one the row shape misses refuses the file.
var (
	setHeaderRe = regexp.MustCompile(`super\(\s*"((?:[^"\\]|\\.)*)"\s*,\s*"([^"]+)"\s*,\s*(?:ExpansionSet\.)?buildDate\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)\s*\)\s*,\s*SetType\.([A-Z_]+)`)
	setCardRe   = regexp.MustCompile(`new\s+(?:ExpansionSet\.)?SetCardInfo\(\s*"((?:[^"\\]|\\.)*)"\s*,\s*("(?:[^"\\]|\\.)*"|\d+)\s*,\s*Rarity\.([A-Z_]+)`)
	anyCardRe   = regexp.MustCompile(`new\s+(?:[\w.]+\.)?SetCardInfo\s*\(`)
)

// stripJavaComments blanks // and /* */ comments to spaces, keeping newlines
// and leaving string and char literals intact: XMage comments out the cards
// it has not implemented (Bloomburrow's Heirloom Epic), and a split card's
// name ("Fire // Ice") holds a "//" that is not a comment.
func stripJavaComments(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank := func(i int) {
		if out[i] != '\n' {
			out[i] = ' '
		}
	}
	for i := 0; i < len(out); i++ {
		switch c := out[i]; {
		case c == '"' || c == '\'':
			for i++; i < len(out) && out[i] != c && out[i] != '\n'; i++ {
				if out[i] == '\\' {
					i++
				}
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				blank(i)
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			blank(i)
			blank(i + 1)
			for i += 2; i < len(out); i++ {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					blank(i)
					blank(i + 1)
					i++
					break
				}
				blank(i)
			}
		}
	}
	return out
}

// javaString decodes the body of a Java string literal. Java's escapes in
// set classes (\" \\ \uXXXX) are a subset of Go's.
func javaString(body string) (string, error) {
	return strconv.Unquote(`"` + body + `"`)
}

// ParseSetClass reads one XMage set class. It refuses a file in which any
// SetCardInfo entry does not match the literal shape, or that lists no card
// at all, so a card is never silently dropped from a manifest.
func ParseSetClass(src []byte, xmageRef string) (Manifest, error) {
	src = stripJavaComments(src)
	h := setHeaderRe.FindSubmatch(src)
	if h == nil {
		return Manifest{}, fmt.Errorf("compliance: no ExpansionSet super(...) header")
	}
	name, err := javaString(string(h[1]))
	if err != nil {
		return Manifest{}, fmt.Errorf("compliance: set name %q: %v", h[1], err)
	}
	y, _ := strconv.Atoi(string(h[3]))
	mo, _ := strconv.Atoi(string(h[4]))
	d, _ := strconv.Atoi(string(h[5]))
	m := Manifest{
		Code: string(h[2]), Name: name, Released: fmt.Sprintf("%04d-%02d-%02d", y, mo, d),
		SetType: string(h[6]), XMageRef: xmageRef,
	}
	all := anyCardRe.FindAllIndex(src, -1)
	rows := setCardRe.FindAllSubmatch(src, -1)
	if len(all) != len(rows) {
		return Manifest{}, fmt.Errorf("compliance: %s: %d SetCardInfo entries but %d parsed", m.Code, len(all), len(rows))
	}
	if len(rows) == 0 {
		return Manifest{}, fmt.Errorf("compliance: %s: no SetCardInfo entries", m.Code)
	}
	byName := map[string]*ManifestCard{}
	for _, r := range rows {
		cn, err := javaString(string(r[1]))
		if err != nil {
			return Manifest{}, fmt.Errorf("compliance: %s: card name %q: %v", m.Code, r[1], err)
		}
		num := string(r[2])
		if num[0] == '"' {
			if num, err = strconv.Unquote(num); err != nil {
				return Manifest{}, fmt.Errorf("compliance: %s: %s: collector number %s: %v", m.Code, cn, r[2], err)
			}
		}
		c := byName[cn]
		if c == nil {
			c = &ManifestCard{Name: cn, Rarity: string(r[3])}
			byName[cn] = c
		}
		c.Numbers = append(c.Numbers, num)
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := byName[n]
		sort.Strings(c.Numbers)
		c.Numbers = dedupSorted(c.Numbers)
		m.Cards = append(m.Cards, *c)
	}
	return m, nil
}

func dedupSorted(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// MarshalLines renders the manifest as JSON with one card per line, so a
// manifest diff after an XMAGE_REF bump reads card by card.
func (m Manifest) MarshalLines() []byte {
	var b bytes.Buffer
	head := struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Released string `json:"released"`
		SetType  string `json:"set_type"`
		XMageRef string `json:"xmage_ref"`
	}{m.Code, m.Name, m.Released, m.SetType, m.XMageRef}
	hj, _ := json.Marshal(head)
	b.Write(hj[:len(hj)-1]) // drop the closing brace
	b.WriteString(",\n\"cards\": [\n")
	for i, c := range m.Cards {
		cj, _ := json.Marshal(c)
		b.Write(cj)
		if i < len(m.Cards)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("]}\n")
	return b.Bytes()
}
