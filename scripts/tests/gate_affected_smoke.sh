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
[ "$nlines" = 2 ]
check "shard helper emits exactly two patterns" $? "nlines=$nlines out=$out"
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

# The fallback: a list the helper cannot partition returns nonzero, so the
# gate runs the unsplit command instead of dropping tests.
printf '' | patterns >/dev/null 2>&1
[ "$?" != 0 ]
check "empty list makes the shard helper fail (caller falls back)" $?
printf 'Test\n' | patterns >/dev/null 2>&1
[ "$?" != 0 ]
check "a name shorter than five characters makes the shard helper fail" $?

printf '\ngate_affected_smoke: %s\n' "$([ $fails = 0 ] && echo ALL GREEN || echo FAILURES ABOVE)"
exit "$fails"
