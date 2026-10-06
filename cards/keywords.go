package cards

import (
	"strings"
)

// expandKeywords turns each keyword the engine implements through ordinary
// machinery into the triggered ability, replacement effect or activated
// ability Forge itself expands it to (CardFactoryUtil, in spirit), tagged
// Params["Keyword"] so nothing downstream needs to know the difference. The
// SVars it adds start with "__kw" and cannot collide with a script's own.
// Idempotent per keyword LINE INSTANCE, not per line text: a second Link()
// adds nothing, but a face that prints the same K: line twice expands it
// twice (CR 702.108b for prowess, and the Oracle text of Scurry of Squirrels,
// "Myriad, myriad", or Evercoat Ursine, "Hideaway 3, hideaway 3"). Forge
// expands every K: line independently, so matching it means each instance is
// its own expansion. Two distinct K:Equip: lines (different costs or
// restrictions) still expand both -- ruling FL-13. Keywords whose meaning is
// a casting option (Kicker, Surge, Flashback, Delve, Flash, Miracle) or a
// static property (Protection, Indestructible, Devoid) are not expanded:
// rules reads them directly.
func (f *Face) expandKeywords() {
	// The idempotence key is (kind, keyword line text). Lines expanded by a
	// PRIOR Link() pass are recorded in pre before this pass starts; a line
	// expanded earlier in THIS pass is deliberately NOT recorded, so two
	// equal lines at different indices each expand. Within one pass every
	// has() therefore answers false for a line this pass has not yet added,
	// which is what lets an expander that checks has() and then delegates
	// to addKeywordTrigger (which checks has() again) append exactly once.
	pre := f.expandedKeywordKeys()
	// has reports whether a PREVIOUS pass already produced a T:/R:/A:/S:
	// entry for tag. It does not consult this pass' own additions, so a
	// second identical line in the same pass is a second instance (CR
	// 702.108b) while a repeat Link() adds nothing.
	has := func(kind, tag string) bool {
		return pre[kind+"\x00"+tag]
	}
	for i, k := range f.Keywords {
		head := KeywordHead(k)
		param := ""
		if j := strings.IndexByte(k, ':'); j >= 0 {
			param = strings.TrimSpace(k[j+1:])
		}
		// Per-keyword dispatch: see kwExpanders. A head with no registered
		// expander is not expanded, which is what the switch did by having no
		// default arm -- rules reads those keywords directly.
		if fn := kwExpanders[head]; fn != nil {
			fn(f, i, k, head, param, has)
		}
	}
}

// expandedKeywordKeys collects the (kind, KeywordLine) tags of every T:/R:/
// A:/S: entry already on the face, so expandKeywords can treat a prior Link()
// pass' expansions as already present while a current pass' own additions do
// not suppress a second identical line. KeywordLine is the tag every expander
// sets alongside Keyword; it is never set on printed (hand-written) abilities,
// so a printed T: line cannot be mistaken for a prior expansion.
func (f *Face) expandedKeywordKeys() map[string]bool {
	keys := map[string]bool{}
	add := func(kind, line string) {
		if line != "" {
			keys[kind+"\x00"+line] = true
		}
	}
	for _, t := range f.Triggers {
		add("T", t.Params["KeywordLine"])
	}
	for _, r := range f.Repls {
		add("R", r.Params["KeywordLine"])
	}
	for _, a := range f.Abilities {
		add("A", a.Params["KeywordLine"])
	}
	for _, s := range f.Statics {
		add("S", s.Params["KeywordLine"])
	}
	return keys
}

// kwExpander expands one keyword line onto the face.
//
// The parameters are what the switch arms this replaced closed over: f is the
// face, i the keyword's index in f.Keywords (mint __kw SVar names from it so
// two lines of the same keyword cannot collide), k the FULL keyword line
// verbatim, head and param its split halves, and has the idempotence check --
// whether this exact LINE already produced a T:/R:/A: entry, which is what
// lets a face with two distinct K:Equip: lines expand both (ruling FL-13)
// while a second Link() call adds nothing.
type kwExpander func(f *Face, i int, k, head, param string, has func(kind, line string) bool)

// kwExpanders maps a keyword head to its expansion. A head with no entry is
// not expanded -- the switch had no default arm either, because keywords whose
// meaning is a casting option or a static property are read directly by rules.
var kwExpanders = map[string]kwExpander{}

// registerKeyword installs fn for each named head. Called from init() in the
// per-keyword kw_*.go files.
//
// A duplicate registration panics rather than silently replacing: the point of
// the split is that many tickets edit different files at once, so two files
// claiming one head must be loud at startup, not an expansion that quietly
// stopped being reached.
func registerKeyword(fn kwExpander, heads ...string) {
	for _, head := range heads {
		if _, dup := kwExpanders[head]; dup {
			panic("cards: duplicate keyword expander registered for " + head)
		}
		kwExpanders[head] = fn
	}
}

// addKeywordTrigger appends one tagged T: line whose Execute$ is an SVar
// this function creates, unless the exact keyword line was already
// expanded (kw is the head, used only for the Keyword$ tag; line is the full
// keyword text, used both for idempotency and -- since it, unlike kw, is
// unique per call -- for the __kw SVar name. Soulbond calls this twice with
// the same kw ("Soulbond") but two different lines ("Soulbond#self" and
// "Soulbond#other"): keying the SVar name on kw alone would collide the two
// calls onto one shared SVar, silently letting the second call's effect body
// overwrite the first's).
func (f *Face) addKeywordTrigger(kw, line, trigger, effect string, has func(kind, line string) bool) {
	if has("T", line) {
		return
	}
	sv := "__kw" + strings.ReplaceAll(line, " ", "")
	f.setSVar(sv, effect)
	p := parseParams(trigger + " | Execute$ " + sv + " | Keyword$ " + kw)
	p["KeywordLine"] = line
	f.Triggers = append(f.Triggers, Trigger{Mode: p["Mode"], Params: p})
}

// setSVar lazily initializes f.SVars before writing name/body. Most compiled
// faces never call this -- only ones with an SVar-based keyword expansion
// do -- so allocating unconditionally in expandKeywords would put a throwaway
// empty map into every face the IR cache stores for nothing.
func (f *Face) setSVar(name, body string) {
	if f.SVars == nil {
		f.SVars = map[string]string{}
	}
	f.SVars[name] = body
}
