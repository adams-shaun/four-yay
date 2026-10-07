package oraclegen

import (
	"strings"
	"testing"

	"github.com/adams-shaun/gorge/rules"
)

// answerMatches reports whether an emitted answer names one offered ref under
// the XMage driver's own matching: an exact "@ref"/ref alias, or a bare or
// copy-marked name with the isCopy() filter. It mirrors
// TestPlayer.hasObjectTargetNameOrAlias and makeChoose's "[no copy]"/
// "[only copy]" handling, so a test can prove the answer leaves exactly one
// candidate.
func answerMatches(answer, ref string) bool {
	if strings.HasPrefix(answer, "@") {
		return answer == "@"+ref
	}
	if strings.Contains(answer, ":") && strings.HasPrefix(answer, "p") {
		return answer == ref
	}
	base := answer
	copyFilter := ""
	switch {
	case strings.HasSuffix(base, "[no copy]"):
		copyFilter, base = "origin", strings.TrimSuffix(base, "[no copy]")
	case strings.HasSuffix(base, "[only copy]"):
		copyFilter, base = "copy", strings.TrimSuffix(base, "[only copy]")
	}
	if !strings.EqualFold(base, refNameForTest(ref)) {
		return false
	}
	token := strings.Contains(ref, ":token:")
	switch copyFilter {
	case "origin":
		return !token
	case "copy":
		return token
	}
	return true
}

func refNameForTest(ref string) string {
	n := ref
	if i := strings.IndexByte(n, ':'); i >= 0 && strings.HasPrefix(n, "p") {
		n = n[i+1:]
	}
	n = strings.TrimPrefix(n, "token:")
	if j := strings.LastIndexByte(n, '#'); j >= 0 && strings.Trim(n[j+1:], "0123456789") == "" {
		n = n[:j]
	}
	return n
}

// sameNameSelectsExactlyOne is the property the ticket fixes: an ambiguous
// pick's emitted answer must leave exactly one distinct offered object for
// XMage's name match. A bare name (the pre-fix output) leaves more than one.
func sameNameSelectsExactlyOne(t *testing.T, d rules.OracleDecision, answer string) {
	t.Helper()
	if !ClassifySameName(d, 0).Ambiguous {
		t.Fatalf("fixture must be an ambiguous same-name pick: %+v", d)
	}
	seen := map[string]bool{}
	for _, o := range d.OptionRefs {
		if answerMatches(answer, o) {
			seen[o] = true
		}
	}
	if len(seen) != 1 {
		t.Fatalf("answer %q leaves %d distinct same-name candidates, want exactly 1 (opts=%v)", answer, len(seen), d.OptionRefs)
	}
	if !answerMatches(answer, d.PickRefs[0]) {
		t.Fatalf("answer %q does not select the picked ref %q", answer, d.PickRefs[0])
	}
}

// The three shapes the reviewer proved still emitted a bare name: two cards,
// two tokens, a card among two cards plus a token, and an opponent's object
// sharing an own object's name. Each must carry the pick's exact scenario ref,
// which the driver resolves to that one object.
func TestXAnswersSameNameDistinctObjectsUseExactRef(t *testing.T) {
	for _, tc := range []struct {
		name, pick, want string
		refs             []string
	}{
		{"two_cards_second", "p0:Forest#2", "@p0:Forest#2", []string{"p0:Forest", "p0:Forest#2"}},
		{"two_tokens_second", "p0:token:Forest#2", "@p0:token:Forest#2", []string{"p0:token:Forest", "p0:token:Forest#2"}},
		{"two_cards_plus_token", "p0:Forest#2", "@p0:Forest#2", []string{"p0:Forest", "p0:Forest#2", "p0:token:Forest"}},
		{"opponent_same_name", "p1:Forest", "@p1:Forest", []string{"p0:Forest", "p1:Forest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := rules.OracleDecision{
				Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: len(tc.refs), Max: 1,
				Picks: []string{"Forest"}, PickRefs: []string{tc.pick},
				PickIdx: []int{1}, PickKinds: []string{"card"}, OptionRefs: tc.refs,
			}
			got := XAnswers([]rules.OracleDecision{d}, 1, nil)
			if len(got) != 1 || len(got[0]) != 1 {
				t.Fatalf("answers = %#v, want one", got)
			}
			if got[0][0].Value != tc.want {
				t.Fatalf("answer = %q, want %q", got[0][0].Value, tc.want)
			}
			sameNameSelectsExactlyOne(t, d, got[0][0].Value)
		})
	}
}

// The copy marker is used only when it leaves EXACTLY one candidate: one card
// among same-named tokens, or one token among same-named cards. With an extra
// same-kind sibling the marker is not enough and the ref is used instead.
func TestXAnswersCopyMarkerOnlyWhenUnique(t *testing.T) {
	unique := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Grizzly Bears"}, PickRefs: []string{"p0:Grizzly Bears"},
		PickIdx: []int{0}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Grizzly Bears", "p0:token:Grizzly Bears"},
	}
	got := XAnswers([]rules.OracleDecision{unique}, 1, nil)
	if got[0][0].Value != "Grizzly Bears[no copy]" {
		t.Fatalf("unique copy filter answer = %q, want %q", got[0][0].Value, "Grizzly Bears[no copy]")
	}
	sameNameSelectsExactlyOne(t, unique, got[0][0].Value)

	// Two tokens and one card, picking a token: the token filter leaves two
	// tokens, so the marker cannot be used; the ref must be.
	notUnique := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 3, Max: 1,
		Picks: []string{"Grizzly Bears"}, PickRefs: []string{"p0:token:Grizzly Bears#2"},
		PickIdx: []int{2}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Grizzly Bears", "p0:token:Grizzly Bears", "p0:token:Grizzly Bears#2"},
	}
	if p := ClassifySameName(notUnique, 0); p.CopyMarker != "" {
		t.Fatalf("two tokens must not use a copy marker, got %q", p.CopyMarker)
	}
	got = XAnswers([]rules.OracleDecision{notUnique}, 1, nil)
	if got[0][0].Value != "@p0:token:Grizzly Bears#2" {
		t.Fatalf("ambiguous copy filter answer = %q, want the exact ref", got[0][0].Value)
	}
	sameNameSelectsExactlyOne(t, notUnique, got[0][0].Value)
}

// Two same-name options that are the SAME object (a repeated trigger source)
// are not an ambiguity: a bare name still selects that one object.
func TestXAnswersRepeatedSameRefIsNotAmbiguous(t *testing.T) {
	d := rules.OracleDecision{
		Step: 0, Seat: 0, Kind: "choose_n", GorgeKind: "choose", Options: 2, Max: 1,
		Picks: []string{"Alania, Divergent Storm"}, PickRefs: []string{"p0:Alania, Divergent Storm"},
		PickIdx: []int{0}, PickKinds: []string{"card"},
		OptionRefs: []string{"p0:Alania, Divergent Storm", "p0:Alania, Divergent Storm"},
	}
	if ClassifySameName(d, 0).Ambiguous {
		t.Fatalf("two options naming one object must not be ambiguous")
	}
	got := XAnswers([]rules.OracleDecision{d}, 1, nil)
	if got[0][0].Value != "Alania, Divergent Storm" {
		t.Fatalf("repeated same-ref answer = %q, want the bare label", got[0][0].Value)
	}
}
