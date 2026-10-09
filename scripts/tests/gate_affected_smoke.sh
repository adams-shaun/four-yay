#!/usr/bin/env bash
# gate_affected_smoke.sh — the affected gate must never name a package the
# default build cannot compile.
#
# A candidate directory whose every .go file is build-tagged (cmd/autopayaudit)
# is not a package under the default tags: passing it to `go vet`/`go test`
# fails the whole gate with "build constraints exclude all Go files"
# (9b9bd27a, 2026-10-06). The gate filters its touched-directory list through
# scripts/gate_affected.sh's gate_affected_default_build_pkgs, which asks
# `go list -e` for at least one default-build file.
#
# This test pins the CLASS, not the one instance: it builds a throwaway module
# with a freshly-minted build-constrained-only package, a normal package and a
# package whose only test file is build-tagged, and asserts the helper drops
# exactly the unbuildable one. It then asserts the real script still routes its
# candidate list through that helper, so deleting the filter fails here.
#
#   scripts/tests/gate_affected_smoke.sh
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
GATE=$ROOT/scripts/gate_affected.sh
TMP=$(mktemp -d /tmp/gate-affected.XXXXXX)
fails=0
check() {
	if [ "$2" = 0 ]; then
		printf 'ok   %s\n' "$1"
	else
		printf 'FAIL %s %s\n' "$1" "${3:-}"
		fails=$((fails + 1))
	fi
}
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT

# --- precondition: the script defines the helper and is sourceable without
# running the gate. A syntax error or a removed definition fails loudly here.
[ -f "$GATE" ]
check "gate script exists at scripts/gate_affected.sh" $?
( bash -c "source '$GATE'" >/dev/null 2>&1 )
check "gate script sources cleanly without running the gate (precondition)" $? \
	"bash -c source '$GATE' exited $?"
# Sourcing returns at the guard near the top, so a syntax error BELOW that
# guard never executes during a source and hides from the check above
# (cli-20261009T114433Z-45f2f307 mrg1: a stray fi survived a green smoke).
# bash -n parses the whole file instead.
( bash -n "$GATE" )
check "gate script parses in full (bash -n catches late syntax errors)" $? \
	"bash -n '$GATE' exited $?"
grep -q 'gate_affected_default_build_pkgs' "$GATE"
check "gate_affected.sh defines gate_affected_default_build_pkgs" $?
# A caller-controlled environment variable must not skip normal gate work.
GATE_AFFECTED_SOURCE_LIB=1 bash "$GATE" definitely-not-a-valid-base >/dev/null 2>&1
[ "$?" -ne 0 ]
check "environment variable cannot bypass base validation" $?

# --- the helper itself, against the real repo --------------------------------
filter() ( bash -c "source '$GATE'; gate_affected_default_build_pkgs" )

# cmd/autopayaudit was the instance that motivated the filter, but it now has a
# default-build test file (c2feefbd9), so it is no longer a probe for the class:
# the throwaway module below is.
out=$(printf './view\n' | filter)
case $out in
*./view*) check "helper keeps ./view (filter is not dropping everything)" 0 ;;
*) check "helper keeps ./view (filter is not dropping everything)" 1 "out=$out" ;;
esac

# --- the CLASS: a fresh build-constrained-only package the helper has never
# seen. A fix keyed to cmd/autopayaudit by name would pass the case above and
# fail this one.
M=$TMP/mod
mkdir -p "$M/taggedonly" "$M/plain" "$M/taggedtest"
cat >"$M/go.mod" <<'EOF'
module example.com/filtercase

go 1.22
EOF
cat >"$M/taggedonly/x.go" <<'EOF'
//go:build neverenabled

package taggedonly

func X() {}
EOF
cat >"$M/plain/x.go" <<'EOF'
package plain

func X() {}
EOF
cat >"$M/taggedtest/x.go" <<'EOF'
package taggedtest

func X() {}
EOF
cat >"$M/taggedtest/x_test.go" <<'EOF'
//go:build neverenabled

package taggedtest

func TestX(t any) {}
EOF

# Run the helper from inside the throwaway module so `go list` resolves the
# synthetic packages. ./plain and ./taggedtest are buildable; ./taggedonly is
# the class instance.
(
	cd "$M"
	source "$GATE"
	printf './taggedonly\n./plain\n./taggedtest\n' | gate_affected_default_build_pkgs
) >"$TMP/out.txt" 2>&1
check "helper runs against the synthetic module" $? "$(cat "$TMP/out.txt")"
! grep -qx './taggedonly' "$TMP/out.txt"
check "helper drops a NEW build-constrained-only package (the class, not the instance)" $? \
	"out=$(tr '\n' ' ' <"$TMP/out.txt")"
grep -qx './plain' "$TMP/out.txt" && grep -qx './taggedtest' "$TMP/out.txt"
check "helper keeps a default-build package and a package with only a build-tagged test" $? \
	"out=$(tr '\n' ' ' <"$TMP/out.txt")"

# Order preserved across the filter (the callers printf the result in order).
got=$(tr '\n' ',' <"$TMP/out.txt")
[ "$got" = "./plain,./taggedtest," ]
check "helper preserves input order" $? "got=$got"

# --- the wiring: match the candidate assignment itself, not the helper's
# definition. Removing the call from this pipeline must fail this assertion.
grep -Eq '^  pkgs=\$\(printf .+ \| gate_affected_default_build_pkgs\)$' "$GATE"
check "candidate derivation pipes pkgs through the default-build filter" $?

# --- the shard partition: complete, disjoint, and covering the digit-prefixed
# names a naive [A-Z] split drops. rules_shard_patterns_from_list is the pure
# half of the gate's shard helper, so it can be fed a synthetic list here.
patterns() ( bash -c "source '$GATE'; rules_shard_patterns_from_list" )

# A list whose first characters are all letters EXCEPT one digit-prefixed name
# (Test3141... exists in the real corpus). A partition keyed to [A-Z] silently
# drops it; this asserts the class is covered.
SYN=$(printf 'TestAlpha\nTestBravo\nTestAlphaTwo\nTest3141Discard\nTestZulu\nTestZuluTwo\nTestZuluThree\n')
out=$(printf '%s\n' "$SYN" | patterns)
nlines=$(printf '%s\n' "$out" | /usr/bin/grep -c '^\^Test\[')
[ "$nlines" = 4 ]
check "shard helper emits exactly four patterns" $? "nlines=$nlines out=$out"
uncovered=$(python3 - "$out" "$SYN" <<'PY'
import re, sys
pats = [l for l in sys.argv[1].split('\n') if l.startswith('^Test[')]
names = [l for l in sys.argv[2].split('\n') if l]
hits = [sum(1 for p in pats if re.match(p, n)) for n in names]
print(' '.join(n for n, h in zip(names, hits) if h == 0))
print(' '.join(n for n, h in zip(names, hits) if h > 1))
PY
)
[ "$(printf '%s\n' "$uncovered" | /usr/bin/grep -c .)" = 0 ]
check "shard partition is complete and disjoint (incl. a digit-prefixed name)" $? "uncovered=[$uncovered]"

# The real ./rules list: every listed test must land in exactly one pattern.
REALLIST=$(go test -list '.*' ./rules/ 2>/dev/null | /usr/bin/grep '^Test' || true)
if [ -n "$REALLIST" ]; then
	printf '%s\n' "$REALLIST" | patterns >"$TMP/patterns.txt"
	printf '%s\n' "$REALLIST" >"$TMP/names.txt"
	bad=$(python3 - "$TMP/patterns.txt" "$TMP/names.txt" <<'PY'
import re, sys
pats = [l for l in open(sys.argv[1]).read().split('\n') if l.startswith('^Test[')]
names = [l for l in open(sys.argv[2]).read().split('\n') if l]
miss = [n for n in names if sum(1 for p in pats if re.match(p, n)) != 1]
print(len(miss))
PY
)
	[ "$bad" = 0 ]
	check "every real ./rules test lands in exactly one shard pattern" $? "bad=$bad"
else
	printf 'skip real-list shard check (no ./rules tests listed)\n'
fi

# --- the compliance/oraclegen/templates shard selectors: exactly four, all
# anchored, the remainder the anchored union of the three named patterns
# (templates_shard_patterns). An unanchored -skip remainder deleted any
# future name that merely extends one of the prefixes from EVERY shard (r2
# review of cli-20261009T182406Z-a616bfea), so the class is pinned below
# with synthetic names, not just today's list.
TPLSEL=$(bash -c "source '$GATE'; templates_shard_patterns")
nlines=$(printf '%s\n' "$TPLSEL" | /usr/bin/grep -c .)
[ "$nlines" = 4 ]
check "templates shard helper emits exactly four selectors" $? "nlines=$nlines out=$TPLSEL"
tplbad=$(python3 - "$TPLSEL" <<'PY'
import sys
sel = [l for l in sys.argv[1].split('\n') if l]
if len(sel) != 4:
    print(f'want 4 selectors, got {len(sel)}')
elif any(not (s.startswith('^') and s.endswith('$')) for s in sel):
    print('unanchored selector: ' + ' '.join(sel))
elif sel[3] != '^(' + '|'.join(s[1:-1] for s in sel[:3]) + ')$':
    print(f'remainder is not the anchored union of the -run shards: {sel[3]}')
PY
)
[ -z "$tplbad" ]
check "templates remainder skip is the anchored union of the three -run patterns" $? "$tplbad"

# The class the anchoring pins: a future name that merely extends one of the
# prefixes runs in the remainder shard, never in no shard. (h4 = everything
# the skip does not match, so its share of a name's hit count is 1 minus the
# skip match.)
tplbad=$(python3 - "$TPLSEL" <<'PY'
import re, sys
even, odd, named, rest = (l for l in sys.argv[1].split('\n') if l)
run = [re.compile(p) for p in (even, odd, named)]
skip = re.compile(rest)
for n in ('TestSameNameAnswerCensusChunk00',
          'TestSameNameAnswerCensusChunk00Extra',
          'TestSetupColourCensus', 'TestSetupColourCensusChunk00'):
    hits = sum(1 for p in run if p.match(n)) + (0 if skip.match(n) else 1)
    if hits != 1:
        print(f'{n}={hits}')
        break
PY
)
[ -z "$tplbad" ]
check "a future prefix-extending name is still run by exactly one shard" $? "miss=[$tplbad]"

# The real ./compliance/oraclegen/templates list: every listed test must be
# run by exactly one of the four shards, mirroring the REALLIST check above.
TPLLIST=$(go test -list '.*' ./compliance/oraclegen/templates 2>/dev/null | /usr/bin/grep '^Test' || true)
if [ -n "$TPLLIST" ]; then
	tplbad=$(python3 - "$TPLSEL" "$TPLLIST" <<'PY'
import re, sys
even, odd, named, rest = (l for l in sys.argv[1].split('\n') if l)
run = [re.compile(p) for p in (even, odd, named)]
skip = re.compile(rest)
bad = []
for n in (l for l in sys.argv[2].split('\n') if l):
	hits = sum(1 for p in run if p.match(n)) + (0 if skip.match(n) else 1)
	if hits != 1:
		bad.append(f'{n}={hits}')
print(' '.join(bad))
PY
)
	[ -z "$tplbad" ]
	check "every real oraclegen/templates test is run by exactly one shard" $? "miss=[$tplbad]"
else
	printf 'skip real templates-list check (no oraclegen/templates tests listed)\n'
fi

# The fallback: a list the helper cannot partition returns nonzero, so the
# gate runs the unsplit command instead of dropping tests.
printf '' | patterns >/dev/null 2>&1
[ "$?" != 0 ]
check "empty list makes the shard helper fail (caller falls back)" $?
printf 'Test\n' | patterns >/dev/null 2>&1
[ "$?" != 0 ]
check "a name shorter than five characters makes the shard helper fail" $?
# Four buckets need four distinct first characters; fewer would print an
# invalid empty character class, so the helper must fail instead and the
# caller falls back to the unsplit run.
printf 'TestAlpha\nTestBravo\nTestAlphaTwo\n' | patterns >/dev/null 2>&1
[ "$?" != 0 ]
check "fewer than four first characters makes the shard helper fail (caller falls back)" $?

# --- the heavy $others packages: compliance/gate and compliance/adopt (plus
# internal/paymirror, cmd/repro, effects, compliance/oraclegen/templates) are
# extracted from the -p=6 batch and split by test_name_packs_from_list into
# three count-balanced packs run as concurrent `go test` processes
# (heavy_shard_pool). A heavy package is never put back in the batch whole.
# compliance/gate and compliance/adopt run on EVERY ticket, so this block pins
# their extraction and the complete+disjoint partition of their real test
# lists: a future test cannot fall outside every pack, and removing either
# package from the heavy selector (putting it back in the whole-package batch)
# fails here.
grep -q 'test_name_packs_from_list()' "$GATE"
check "gate_affected.sh defines test_name_packs_from_list (precondition)" $?
packs() ( bash -c "source '$GATE'; test_name_packs_from_list 3" )

# Fallback: a list the packer cannot split returns nonzero, so the caller runs
# the package whole instead of dropping tests.
printf '' | packs >/dev/null 2>&1
[ "$?" != 0 ]
check "empty list makes the heavy packer fail (caller falls back to whole)" $?
printf 'TestAlpha\nTestBravo\n' | packs >/dev/null 2>&1
[ "$?" != 0 ]
check "fewer names than packs makes the heavy packer fail (caller falls back)" $?

# The partition itself on a synthetic list: exactly three anchored packs and
# every name (including one that merely extends another's prefix) in exactly
# one. Unlike the templates prefixes this is an explicit alternation read from
# the live listing, so the prefix-extending name is packed like any other.
SYNHP=$(printf 'TestAlpha\nTestBravo\nTestAlphaTwo\nTestAlphaTwoExtra\nTestCharlie\nTestDelta\nTestEcho\nTestFoxtrot\n')
HPSEL=$(printf '%s\n' "$SYNHP" | packs)
nlines=$(printf '%s\n' "$HPSEL" | /usr/bin/grep -c '^\^(')
[ "$nlines" = 3 ]
check "heavy packer emits exactly three packs" $? "nlines=$nlines out=$HPSEL"
hpbad=$(python3 - "$HPSEL" "$SYNHP" <<'PY'
import re, sys
sel = [l for l in sys.argv[1].split('\n') if l]
names = [l for l in sys.argv[2].split('\n') if l]
if len(sel) != 3:
    print(f'want 3 packs, got {len(sel)}')
elif any(not (s.startswith('^(') and s.endswith(')$')) for s in sel):
    print('unanchored pack: ' + ' '.join(sel))
else:
    pats = [re.compile(s) for s in sel]
    for n in names:
        hits = sum(1 for p in pats if p.match(n))
        if hits != 1:
            print(f'{n}={hits}')
            break
PY
)
[ -z "$hpbad" ]
check "heavy partition is complete and disjoint (incl. a prefix-extending name)" $? "$hpbad"

# The wiring: the heavy selector names both compliance packages (one left out
# stays in the -p=6 batch and runs whole, the pole this mechanism removes),
# and the batch really excludes the selector.
HEAVYRE=$(sed -n "s/^heavy='\(.*\)'$/\1/p" "$GATE")
[ -n "$HEAVYRE" ]
check "gate defines a heavy-package selector (precondition)" $? "HEAVYRE=[$HEAVYRE]"
nheavy=$(printf '%s\n' ./compliance/gate ./compliance/adopt | /usr/bin/grep -cE "$HEAVYRE" || true)
[ "$nheavy" = 2 ]
check "heavy selector names compliance/gate and compliance/adopt" $? "nheavy=$nheavy"
grep -Eq 'batch=.*grep -v -E "\$heavy"' "$GATE"
check "the -p=6 batch excludes the heavy packages" $?
grep -qF 'go test -p=1 -run "$pat" -skip "^($global)$" "$p"' "$GATE"
check "each heavy pack runs the packed regex with the gate's -skip" $?

# The red path: a failed pack marks the pool bad, the pool's pid joins $pids,
# and the final wait turns it into a nonzero gate exit. Removing any link
# would let a failing heavy pack exit 0.
grep -qF 'wait -n || bad=1' "$GATE" && grep -qF 'return "$bad"' "$GATE"
check "a failed heavy pack marks the pool bad" $?
grep -qF '${hp:+ $hp}' "$GATE" && grep -qF 'wait "$p" || rc=1' "$GATE"
check "the heavy pool pid is waited on by the gate" $?

# The real lists: every listed test of each compliance package lands in
# exactly one of the three packs the gate builds from that same listing, and
# none is caught by the process-global -skip (which would silently drop it
# from every pack). These two packages always have tests, so an empty listing
# is a failure here, not a skip.
GLOBALRE=$(sed -n "s/^global='\(.*\)'$/\1/p" "$GATE")
[ -n "$GLOBALRE" ]
check "gate defines the process-global skip (precondition)" $? "GLOBALRE=[$GLOBALRE]"
for pkg in ./compliance/gate ./compliance/adopt; do
	list=$(go test -list '.*' "$pkg" 2>/dev/null | /usr/bin/grep '^Test' || true)
	[ -n "$list" ]
	check "$pkg lists its tests (precondition for the pack check)" $? "list empty"
	if [ -n "$list" ]; then
		printf '%s\n' "$list" | packs >"$TMP/hp-packs.txt"
		printf '%s\n' "$list" >"$TMP/hp-names.txt"
		hpbad=$(python3 - "$TMP/hp-packs.txt" "$TMP/hp-names.txt" "$GLOBALRE" <<'PY'
import re, sys
sel = [l for l in open(sys.argv[1]).read().split('\n') if l]
names = [l for l in open(sys.argv[2]).read().split('\n') if l]
skip = re.compile('^(' + sys.argv[3] + ')$')
pats = [re.compile(s) for s in sel]
bad = []
for n in names:
    hits = sum(1 for p in pats if p.match(n))
    if hits != 1:
        bad.append(f'{n}={hits}')
    if skip.match(n):
        bad.append(f'{n}=skipped')
print(' '.join(bad))
PY
)
		[ -z "$hpbad" ]
		check "every real $pkg test lands in exactly one heavy pack (and none in the global skip)" $? "bad=[$hpbad]"
	fi
done

# --- the Kr8 runs: TestKr8WorldsInFuzzGames runs six t.Parallel games, so it
# is the same shape as the shards and must carry the same GOMAXPROCS override;
# at the gate env's GOMAXPROCS=2 it runs two games at a time (measured 23.7 s
# vs 14.5 s at 6, alone). Removing either override must fail this check.
nkr8=$(grep -cE "^GOMAXPROCS=4 GOMEMLIMIT=3GiB go test -p=1 -run .*TestKr8" "$GATE")
[ "$nkr8" = 2 ]
check "both Kr8 runs override the gate env's GOMAXPROCS (t.Parallel, like the shards)" $? "nkr8=$nkr8"

printf '\ngate_affected_smoke: %s\n' "$([ $fails = 0 ] && echo ALL GREEN || echo FAILURES ABOVE)"
exit "$fails"
