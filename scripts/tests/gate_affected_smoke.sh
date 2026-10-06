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

# Precondition: cmd/autopayaudit really is build-constrained-only (if it ever
# gains a default-build file this test's positive case is stale, not silently
# passing).
auto_files=$(ls "$ROOT"/cmd/autopayaudit/*.go 2>/dev/null | wc -l)
auto_default=$(go list -e -f '{{if or .GoFiles .TestGoFiles .XTestGoFiles}}y{{end}}' \
	"$ROOT/cmd/autopayaudit" 2>/dev/null)
[ "$auto_files" -gt 0 ] && [ -z "$auto_default" ]
check "precondition: cmd/autopayaudit exists and has no default-build files" $? \
	"files=$auto_files default='$auto_default'"

out=$(printf './cmd/autopayaudit\n./view\n' | filter)
case $out in
*./cmd/autopayaudit*) check "helper drops ./cmd/autopayaudit" 1 "out=$out" ;;
*) check "helper drops ./cmd/autopayaudit" 0 ;;
esac
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

printf '\ngate_affected_smoke: %s\n' "$([ $fails = 0 ] && echo ALL GREEN || echo FAILURES ABOVE)"
exit "$fails"
