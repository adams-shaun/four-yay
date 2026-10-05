#!/usr/bin/env python3
"""ratchets-only-fall: a branch may lower a shrink-only ratchet, never raise one.

The ratchets (internal/codeshape, internal/archtest, internal/testutil's
agentsdoc_test.go, rules/testdata/known-unsupported/) are tests in the
branch's own tree, so a branch that raises a constant passes its own tests.
This gate compares the branch against merge-base(<base>, HEAD) and fails when
the branch:

  (a) raises an integer constant in internal/codeshape/*.go,
      internal/testutil/agentsdoc_test.go or internal/archtest/**/*.go;
  (b) deletes such a constant;
  (c) adds an entry to an allow-list / exemption collection in those files
      (and, the mirror image, removes an entry from a deny-list collection);
  (d) removes or rewires a ratchet row of TestCodeShapeOnlyShrinks, or drops
      the comparison that makes a ratchet fail;
  (e) adds a file under rules/testdata/known-unsupported/;
  (f) edits scripts/ratchet_gate.py itself;
  (g) adds a key to, or raises a value of, longFuncCeilings.

It runs from the BASE copy so a branch cannot weaken it:

    git show <base>:scripts/ratchet_gate.py | python3 - <base>

run in the branch worktree. There is no escape trailer: the operator raises a
ratchet by landing it by hand. `python3 scripts/ratchet_gate.py --selftest`
exercises every clause in both directions against temporary git repos.

Stdlib only. The Go sources are read with a small tokenizer (comments and
string contents never match), not regexes over raw text.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import tempfile

GATE_PATH = "scripts/ratchet_gate.py"
KNOWN_UNSUPPORTED = "rules/testdata/known-unsupported/"
RATCHET_TEST = "internal/codeshape/ratchet_test.go"


def watched(path: str) -> bool:
    """The Go files whose constants and collections are ratchets."""
    if not path.endswith(".go"):
        return False
    if path == "internal/testutil/agentsdoc_test.go":
        return True
    if path.startswith("internal/codeshape/") and path.count("/") == 2:
        return True
    return path.startswith("internal/archtest/")


# ---------------------------------------------------------------------------
# Collection classification. The shapes were read from the tree at the time
# the gate landed (main 00363b185):
#
#   allow-lists (an added entry loosens a guard):
#     internal/archtest/arch_test.go       allowed := map[string]bool{ module + "/host": true, ... }
#     internal/archtest/tape_owner_test.go allowed := map[string]string{ "(*Engine).entryPreview": "...", ... }
#     internal/archtest/filename_test.go   var sizeOnlyFileNamesAllowed = map[string]bool{ "effects/misc.go": true, ... }
#     internal/archtest/layering*_test.go  var charsImports / payImports / paramsImports / resolveImports /
#                                          costVocabularyImports / trigmatchImports / combatImports = map[string]bool{ module + "/cards": true, ... }
#     internal/codeshape/codeshape.go      var CtxConstructorFiles = []string{"effects/ctx_new.go", ...}
#
#   deny-lists (a removed entry loosens a guard):
#     internal/archtest/arch_test.go       forbidden := []struct{ from, to string }{ {module + "/effects", module + "/rules"}, ... }
#                                          forbiddenManaBrewDirect := []struct{ from, to string }{ ... }
#     internal/archtest/layering_pay_test.go var payForbiddenStd = []string{"time", ...}
#     internal/codeshape/codeshape.go      var ScannedDirs = []string{"rules", "effects"}
#                                          var <API>OnlyKeys = []string{...}  (keys only that compiler may read)
#
#   ceilings: internal/codeshape/ratchet_test.go var longFuncCeilings = map[string]int{ "rules (*Engine).x": 412, ... }
#
# The <API>Files lists (an API's own resolution files) are neither: they move
# with ordinary file renames. A future collection is classified by its name.
# ---------------------------------------------------------------------------

ALLOW_NAMES = {"allowed", "sizeOnlyFileNamesAllowed", "CtxConstructorFiles"}
ALLOW_RE = re.compile(r"(?i)(allow|exempt|grandfather|whitelist|permit|tolerat|skip|ignore|known)|Imports$")
DENY_NAMES = {"ScannedDirs", "forbidden", "forbiddenManaBrewDirect", "payForbiddenStd"}
DENY_RE = re.compile(r"(?i)forbid|OnlyKeys$")
CEILING_MAPS = {"longFuncCeilings"}


def classify(name: str) -> str:
    if name in CEILING_MAPS:
        return "ceiling"
    if name in ALLOW_NAMES:
        return "allow"
    if name in DENY_NAMES or DENY_RE.search(name):
        return "deny"
    if ALLOW_RE.search(name):
        return "allow"
    return ""


# Comparisons whose removal turns a ratchet off while every row stays.
GUARD_SNIPPETS = {
    RATCHET_TEST: ["case r . got > r . limit :", "case f . Lines > limit :"],
}

# ---------------------------------------------------------------------------
# A minimal Go tokenizer.
# ---------------------------------------------------------------------------

_TOKEN_RE = re.compile(
    r"""
    (?P<nl>\n)
  | (?P<ws>[ \t\r\f]+)
  | (?P<lc>//[^\n]*)
  | (?P<bc>/\*.*?\*/)
  | (?P<raw>`[^`]*`)
  | (?P<str>"(?:\\.|[^"\\\n])*")
  | (?P<rune>'(?:\\.|[^'\\\n])+')
  | (?P<num>(?:0[xXoObB])?[0-9][0-9a-fA-F_]*(?:\.[0-9_]*)?(?:[eEpP][+-]?[0-9_]+)?i?)
  | (?P<ident>[A-Za-z_][A-Za-z0-9_]*)
  | (?P<op>\.\.\.|:=|==|!=|<=|>=|&&|\|\||<-|<<|>>|&\^|\+\+|--|[-+*/%&|^<>=!:;,.(){}\[\]~])
    """,
    re.VERBOSE | re.DOTALL,
)


def tokenize(src: str) -> list[tuple[str, str]]:
    """(kind, text) tokens; comments and spaces dropped, newlines kept as NL."""
    out: list[tuple[str, str]] = []
    pos = 0
    while pos < len(src):
        m = _TOKEN_RE.match(src, pos)
        if m is None:
            pos += 1  # an unknown byte (non-ASCII in an identifier, say): skip it
            continue
        pos = m.end()
        kind = m.lastgroup
        text = m.group()
        if kind in ("ws", "lc"):
            continue
        if kind == "bc":
            if "\n" in text:
                out.append(("nl", "\n"))
            continue
        if kind == "raw":
            kind = "str"
        out.append((kind, text))
    return out


def _int(text: str) -> int | None:
    t = text.replace("_", "")
    try:
        if len(t) > 1 and t[0] == "0" and t[1].isdigit():
            return int(t, 8)
        return int(t, 0)
    except ValueError:
        return None


def _match_close(toks, i):
    """Index of the bracket closing toks[i]."""
    pairs = {"(": ")", "[": "]", "{": "}"}
    depth = 0
    for j in range(i, len(toks)):
        t = toks[j][1]
        if toks[j][0] != "op":
            continue
        if t in pairs:
            depth += 1
        elif t in (")", "]", "}"):
            depth -= 1
            if depth == 0:
                return j
    return len(toks) - 1


def _split_top(toks, seps):
    """Split a token list at depth-0 separators."""
    parts, cur, depth = [], [], 0
    for kind, text in toks:
        if kind == "op" and text in "([{":
            depth += 1
        elif kind == "op" and text in ")]}":
            depth -= 1
        if depth == 0 and ((kind == "op" and text in seps) or (kind == "nl" and "\n" in seps)):
            parts.append(cur)
            cur = []
            continue
        cur.append((kind, text))
    parts.append(cur)
    return parts


def _text(toks) -> str:
    return " ".join(t for k, t in toks if k != "nl")


def int_consts(toks) -> dict[str, int]:
    """name -> value for every `const NAME [T] = <int literal>` spec."""
    out: dict[str, int] = {}

    def spec(st):
        st = [t for t in st if t[0] != "nl"]
        if len(st) < 3 or st[0][0] != "ident":
            return
        try:
            eq = [t[1] for t in st].index("=")
        except ValueError:
            return
        if eq not in (1, 2) or len(st) != eq + 2 or st[eq + 1][0] != "num":
            return
        v = _int(st[eq + 1][1])
        if v is not None:
            out[st[0][1]] = max(v, out.get(st[0][1], v))

    i = 0
    while i < len(toks):
        if toks[i] == ("ident", "const"):
            if i + 1 < len(toks) and toks[i + 1] == ("op", "("):
                j = _match_close(toks, i + 1)
                for st in _split_top(toks[i + 2:j], {";", "\n"}):
                    spec(st)
                i = j
            else:
                j = i + 1
                while j < len(toks) and toks[j][0] != "nl" and toks[j] != ("op", ";"):
                    j += 1
                spec(toks[i + 1:j])
                i = j
        i += 1
    return out


def collections(toks) -> dict[str, dict[str, str]]:
    """name -> {entry key text: value text} for every `NAME = / := T{...}`
    whose T is a map[string]... or a slice type. A slice entry's key is the
    whole element and its value is empty."""
    out: dict[str, dict[str, str]] = {}
    n = len(toks)
    for i in range(n - 4):
        if toks[i][0] != "ident" or toks[i + 1] not in (("op", "="), ("op", ":=")):
            continue
        t2 = [toks[k][1] for k in range(i + 2, min(i + 6, n))]
        is_map = t2[:4] == ["map", "[", "string", "]"]
        is_slice = t2[:2] == ["[", "]"]
        if not (is_map or is_slice):
            continue
        # The literal's '{' is the first depth-0 brace not opening a
        # struct/interface type.
        j = i + 2
        depth = 0
        while j < n:
            kind, text = toks[j]
            if kind == "op" and text in "([":
                depth += 1
            elif kind == "op" and text in ")]":
                depth -= 1
            elif kind == "op" and text == "{" and depth == 0:
                if toks[j - 1][1] in ("struct", "interface"):
                    j = _match_close(toks, j)
                else:
                    break
            elif kind == "nl" and depth == 0:
                j = n
                break
            j += 1
        if j >= n:
            continue
        end = _match_close(toks, j)
        entries = out.setdefault(toks[i][1], {})
        for el in _split_top(toks[j + 1:end], {","}):
            el = [t for t in el if t[0] != "nl"]
            if not el:
                continue
            if is_map:
                parts = _split_top(el, {":"})
                entries[_text(parts[0])] = _text([t for p in parts[1:] for t in p])
            else:
                entries[_text(el)] = ""
    return out


def ratchet_rows(toks) -> dict[str, tuple[str, str]]:
    """TestCodeShapeOnlyShrinks' rows: name -> (measured expr, limit expr)."""
    n = len(toks)
    for i in range(n - 2):
        if toks[i] == ("ident", "func") and toks[i + 1] == ("ident", "TestCodeShapeOnlyShrinks"):
            j = i + 2
            while j < n and toks[j] != ("op", "{"):
                j += 1
            end = _match_close(toks, j)
            body = toks[j:end]
            rows = {}
            for k in range(len(body) - 2):
                if body[k] == ("op", "{") and body[k + 1][0] == "str" and body[k + 2] == ("op", ","):
                    close = _match_close(body, k)
                    parts = _split_top([t for t in body[k + 1:close] if t[0] != "nl"], {","})
                    if len(parts) >= 3:
                        rows[parts[0][0][1]] = (_text(parts[1]), _text(parts[2]))
            return rows
    return {}


# ---------------------------------------------------------------------------
# The check.
# ---------------------------------------------------------------------------


def git(repo: str, *args: str, check: bool = True) -> str:
    p = subprocess.run(["git", "-C", repo, *args], capture_output=True, text=True)
    if check and p.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)}: {p.stderr.strip()}")
    return p.stdout


def show(repo: str, rev: str, path: str) -> str | None:
    p = subprocess.run(["git", "-C", repo, "show", f"{rev}:{path}"], capture_output=True, text=True)
    return p.stdout if p.returncode == 0 else None


def check(repo: str, base: str) -> list[str]:
    mb = git(repo, "merge-base", base, "HEAD").strip()
    changes = []
    for line in git(repo, "diff", "--name-status", "--no-renames", mb, "HEAD").splitlines():
        status, _, path = line.partition("\t")
        changes.append((status[:1], path))
    bad: list[str] = []

    for status, path in changes:
        if path == GATE_PATH:
            bad.append(f"(f) {path}: the branch edits the ratchet gate itself; the operator lands gate changes by hand")
        if status == "A" and path.startswith(KNOWN_UNSUPPORTED):
            bad.append(f"(e) {path}: a new known-unsupported entry; support the card instead of recording the gap")

    files = sorted({p for _, p in changes if watched(p)})
    base_consts: dict[str, int] = {}
    head_consts: dict[str, int] = {}
    for path in files:
        old, new = show(repo, mb, path), show(repo, "HEAD", path)
        ot, nt = tokenize(old or ""), tokenize(new or "")
        for name, v in int_consts(ot).items():
            base_consts[name] = max(v, base_consts.get(name, v))
        for name, v in int_consts(nt).items():
            head_consts[name] = max(v, head_consts.get(name, v))
        oc, nc = collections(ot), collections(nt)
        # A collection renamed in the same file is compared with what it
        # replaced, so a rename cannot launder an added entry.
        gone = {cls: {} for cls in ("allow", "deny", "ceiling")}
        for name, entries in oc.items():
            cls = classify(name)
            if cls and name not in nc:
                gone[cls].update(entries)
        for name, entries in nc.items():
            cls = classify(name)
            if not cls:
                continue
            before = oc.get(name)
            if before is None:
                if not gone[cls]:
                    continue  # a new guard brings its own census
                before = gone[cls]
            if cls == "allow":
                for k in sorted(set(entries) - set(before)):
                    bad.append(f"(c) {path}: {name} gains allow-list entry {k}; fix the code instead of exempting it")
            elif cls == "deny":
                for k in sorted(set(before) - set(entries)):
                    bad.append(f"(c) {path}: {name} loses deny-list entry {k}; that loosens the guard")
            else:
                for k in sorted(entries):
                    nv, ov = _int(entries[k]), _int(before.get(k, ""))
                    if k not in before:
                        bad.append(f"(g) {path}: {name} gains key {k}; extract a named helper or a descriptor entry instead")
                    elif nv is not None and ov is not None and nv > ov:
                        bad.append(f"(g) {path}: {name}[{k}] raised {ov} -> {nv}; extract instead of growing the function")
        if path == RATCHET_TEST:
            orows, nrows = ratchet_rows(ot), ratchet_rows(nt)
            for name in sorted(orows):
                if name not in nrows:
                    bad.append(f"(d) {path}: ratchet row {name} removed from TestCodeShapeOnlyShrinks")
                elif nrows[name] != orows[name]:
                    bad.append(f"(d) {path}: ratchet row {name} rewired {orows[name]} -> {nrows[name]}")
        otext, ntext = _text(ot), _text(nt)
        for snip in GUARD_SNIPPETS.get(path, []):
            if snip in otext and snip not in ntext:
                bad.append(f"(d) {path}: the ratchet comparison `{snip}` is gone")

    for name in sorted(base_consts):
        if name not in head_consts:
            bad.append(f"(b) integer constant {name} deleted (was {base_consts[name]})")
        elif head_consts[name] > base_consts[name]:
            bad.append(f"(a) integer constant {name} raised {base_consts[name]} -> {head_consts[name]}")
    return bad


def main(argv: list[str]) -> int:
    if argv[:1] == ["--selftest"]:
        return selftest()
    if len(argv) != 1:
        print("usage: ratchet_gate.py <base> | --selftest", file=sys.stderr)
        return 2
    try:
        bad = check(os.getcwd(), argv[0])
    except RuntimeError as e:
        print(f"ratchet gate: {e}", file=sys.stderr)
        return 2
    if bad:
        print("ratchets-only-fall: the branch loosens a ratchet. A ratchet is never raised by a branch;")
        print("the operator lands a deliberate raise by hand. Undo these:")
        for b in bad:
            print("  " + b)
        return 1
    print(f"ratchets-only-fall: OK (against merge-base of {argv[0]})")
    return 0


# ---------------------------------------------------------------------------
# Selftest.
# ---------------------------------------------------------------------------

BASE_TREE = {
    "internal/codeshape/ratchet_test.go": """package codeshape

// maxFuncLinesOver300 = 99 in a comment never counts.
const (
	maxFuncLinesOver300 = 44
	engineMethodCount   = 1822
	label               = "x = 5"
)

var longFuncCeilings = map[string]int{
	"rules (*Engine).big": 400,
	"effects effBig":      350,
}

func checkRatchets(rs []ratchet) {
	for _, r := range rs {
		switch {
		case r.got > r.limit:
			fail()
		}
	}
}

func TestCodeShapeOnlyShrinks(t *testing.T) {
	m := measureRepo(t)
	checkRatchets(t, []ratchet{
		{"maxFuncLinesOver300", m.FuncsOver300, maxFuncLinesOver300,
			"advice"},
		{"engineMethodCount", m.EngineMethods, engineMethodCount, "advice"},
	})
}

func TestLongFunctionsOnlyShrink(t *testing.T) {
	switch {
	case f.Lines > limit:
	}
}
""",
    "internal/codeshape/codeshape.go": """package codeshape

const LongFuncLines = 300

var ScannedDirs = []string{"rules", "effects"}

var CtxConstructorFiles = []string{"effects/ctx_new.go", "rules/ctx_new.go"}

var DrawFiles = []string{"effects/draw.go"}
""",
    "internal/archtest/filename_test.go": """package archtest

var sizeOnlyFileNamesAllowed = map[string]bool{
	"effects/misc.go": true,
}

func TestTime(t *testing.T) {
	allowed := map[string]bool{
		module + "/host": true,
	}
	forbidden := []struct{ from, to string }{
		{module + "/effects", module + "/rules"},
	}
	_, _ = allowed, forbidden
}

const charsBoardMethods = 8
""",
    "internal/testutil/agentsdoc_test.go": """package testutil

const (
	knownApproximationRows = 8
	standInCellLimit       = 600
)
""",
    "rules/testdata/known-unsupported/incinerate.txt": "Incinerate\n",
    GATE_PATH: "# gate\n",
}


def _sub(path, old, new):
    def f(tree):
        assert old in tree[path], (path, old)
        tree[path] = tree[path].replace(old, new, 1)
    return f


def _add(path, text):
    def f(tree):
        tree[path] = text
    return f


CASES = [
    # (name, mutation, expected clause or None for a pass)
    ("no change", lambda t: None, None),
    ("(a) raise a codeshape constant", _sub(RATCHET_TEST, "maxFuncLinesOver300 = 44", "maxFuncLinesOver300 = 45"), "(a)"),
    ("(a) lower a codeshape constant", _sub(RATCHET_TEST, "maxFuncLinesOver300 = 44", "maxFuncLinesOver300 = 43"), None),
    ("(a) raise LongFuncLines", _sub("internal/codeshape/codeshape.go", "LongFuncLines = 300", "LongFuncLines = 400"), "(a)"),
    ("(a) raise an archtest constant", _sub("internal/archtest/filename_test.go", "charsBoardMethods = 8", "charsBoardMethods = 9"), "(a)"),
    ("(a) lower an archtest constant", _sub("internal/archtest/filename_test.go", "charsBoardMethods = 8", "charsBoardMethods = 7"), None),
    ("(a) raise agentsdoc constant", _sub("internal/testutil/agentsdoc_test.go", "knownApproximationRows = 8", "knownApproximationRows = 9"), "(a)"),
    ("(a) lower agentsdoc constant", _sub("internal/testutil/agentsdoc_test.go", "knownApproximationRows = 8", "knownApproximationRows = 7"), None),
    ("(a) a comment edit is not a constant", _sub(RATCHET_TEST, "= 99 in a comment", "= 999 in a comment"), None),
    ("(a) a new constant is fine", _sub(RATCHET_TEST, "engineMethodCount   = 1822", "engineMethodCount   = 1822\n\tnewRatchet = 7"), None),
    ("(b) delete a constant", _sub(RATCHET_TEST, "\tengineMethodCount   = 1822\n", ""), "(b)"),
    ("(b) a constant moved to another watched file is kept", lambda t: (
        _sub("internal/archtest/filename_test.go", "const charsBoardMethods = 8\n", "")(t),
        _sub("internal/testutil/agentsdoc_test.go", "standInCellLimit       = 600", "standInCellLimit       = 600\n\tcharsBoardMethods = 8")(t)), None),
    ("(c) add to a package-level allow-list", _sub("internal/archtest/filename_test.go", '"effects/misc.go": true,', '"effects/misc.go": true,\n\t"rules/more.go": true,'), "(c)"),
    ("(c) remove from a package-level allow-list", _sub("internal/archtest/filename_test.go", '\t"effects/misc.go": true,\n', ""), None),
    ("(c) add to a local allow-list", _sub("internal/archtest/filename_test.go", 'module + "/host": true,', 'module + "/host": true,\n\t\tmodule + "/rules": true,'), "(c)"),
    ("(c) remove from a local allow-list", _sub("internal/archtest/filename_test.go", '\t\tmodule + "/host": true,\n', ""), None),
    ("(c) add a codeshape exemption file", _sub("internal/codeshape/codeshape.go", '"rules/ctx_new.go"}', '"rules/ctx_new.go", "rules/more.go"}'), "(c)"),
    ("(c) drop a codeshape exemption file", _sub("internal/codeshape/codeshape.go", ', "rules/ctx_new.go"}', "}"), None),
    ("(c) drop a scanned dir (deny-list)", _sub("internal/codeshape/codeshape.go", '{"rules", "effects"}', '{"rules"}'), "(c)"),
    ("(c) add a scanned dir (deny-list)", _sub("internal/codeshape/codeshape.go", '{"rules", "effects"}', '{"rules", "effects", "cards"}'), None),
    ("(c) drop a forbidden edge", _sub("internal/archtest/filename_test.go", '\t\t{module + "/effects", module + "/rules"},\n', ""), "(c)"),
    ("(c) add a forbidden edge", _sub("internal/archtest/filename_test.go", '{module + "/effects", module + "/rules"},', '{module + "/effects", module + "/rules"},\n\t\t{module + "/view", module + "/rules"},'), None),
    ("(c) rename an allow-list to launder an entry", _sub("internal/archtest/filename_test.go",
        'var sizeOnlyFileNamesAllowed = map[string]bool{\n\t"effects/misc.go": true,',
        'var sizeOnlyNamesAllowed = map[string]bool{\n\t"effects/misc.go": true,\n\t"rules/more.go": true,'), "(c)"),
    ("(c) a brand-new allow-list in a new file is fine", _add("internal/archtest/layering_new_test.go",
        'package archtest\n\nvar newImports = map[string]bool{\n\tmodule + "/cards": true,\n}\n'), None),
    ("(c) an API's own Files list may move", _sub("internal/codeshape/codeshape.go", '"effects/draw.go"', '"effects/draw_resolve.go"'), None),
    ("(d) remove a ratchet row", _sub(RATCHET_TEST, '\t\t{"engineMethodCount", m.EngineMethods, engineMethodCount, "advice"},\n', ""), "(d)"),
    ("(d) rewire a ratchet row's limit", _sub(RATCHET_TEST, "m.EngineMethods, engineMethodCount,", "m.EngineMethods, engineMethodCount + 50,"), "(d)"),
    ("(d) add a ratchet row", _sub(RATCHET_TEST, '"engineMethodCount", m.EngineMethods, engineMethodCount, "advice"},',
        '"engineMethodCount", m.EngineMethods, engineMethodCount, "advice"},\n\t\t{"newRatchet", m.New, newRatchet, "advice"},'), None),
    ("(d) edit a row's advice", _sub(RATCHET_TEST, '"advice"},\n\t\t{"engineMethodCount"', '"better advice"},\n\t\t{"engineMethodCount"'), None),
    ("(d) neuter the comparison", _sub(RATCHET_TEST, "case r.got > r.limit:", "case r.got > r.limit+100:"), "(d)"),
    ("(e) add a known-unsupported file", _add("rules/testdata/known-unsupported/shock.txt", "Shock\n"), "(e)"),
    ("(e) delete a known-unsupported file", lambda t: t.pop("rules/testdata/known-unsupported/incinerate.txt"), None),
    ("(f) edit the gate", _sub(GATE_PATH, "# gate", "# gate, weakened"), "(f)"),
    ("(g) raise a long-function ceiling", _sub(RATCHET_TEST, '"effects effBig":      350', '"effects effBig":      351'), "(g)"),
    ("(g) lower a long-function ceiling", _sub(RATCHET_TEST, '"effects effBig":      350', '"effects effBig":      349'), None),
    ("(g) add a long-function key", _sub(RATCHET_TEST, '"effects effBig":      350,', '"effects effBig":      350,\n\t"rules newBig": 301,'), "(g)"),
    ("(g) delete a long-function key", _sub(RATCHET_TEST, '\t"effects effBig":      350,\n', ""), None),
]


def _write(root, tree):
    for rel, text in tree.items():
        p = os.path.join(root, rel)
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w") as fh:
            fh.write(text)


def selftest() -> int:
    env_git = ["-c", "user.name=selftest", "-c", "user.email=selftest@example.invalid", "-c", "commit.gpgsign=false"]
    failures = 0
    with tempfile.TemporaryDirectory() as tmp:
        for idx, (name, mutate, want) in enumerate(CASES):
            repo = os.path.join(tmp, f"r{idx}")
            os.makedirs(repo)
            git(repo, "init", "-q", "-b", "main")
            _write(repo, BASE_TREE)
            git(repo, "add", "-A")
            git(repo, *env_git, "commit", "-q", "-m", "base")
            git(repo, "checkout", "-q", "-b", "wt/case")
            tree = dict(BASE_TREE)
            mutate(tree)
            for rel in BASE_TREE:
                if rel not in tree:
                    os.remove(os.path.join(repo, rel))
            _write(repo, tree)
            git(repo, "add", "-A")
            git(repo, *env_git, "commit", "-q", "--allow-empty", "-m", "change")
            # Main moves on after the branch: the gate must measure from the
            # merge-base, so a base-side raise is never charged to the branch.
            git(repo, "checkout", "-q", "main")
            moved = dict(BASE_TREE)
            _sub("internal/testutil/agentsdoc_test.go", "standInCellLimit       = 600", "standInCellLimit       = 700")(moved)
            _write(repo, moved)
            git(repo, *env_git, "commit", "-q", "-am", "main moves")
            git(repo, "checkout", "-q", "wt/case")
            bad = check(repo, "main")
            got = None
            if bad:
                clauses = sorted({b.split(" ", 1)[0] for b in bad})
                got = clauses[0] if len(clauses) == 1 else ",".join(clauses)
            ok = got == want
            failures += not ok
            print(f"{'PASS' if ok else 'FAIL'}  {name}: want {want or 'pass'}, got {got or 'pass'}")
            if not ok:
                for b in bad:
                    print("        " + b)
    print(f"selftest: {len(CASES) - failures}/{len(CASES)} cases pass")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
