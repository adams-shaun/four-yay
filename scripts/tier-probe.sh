#!/usr/bin/env bash
# tier-probe.sh — is a seat tier's endpoint answering RIGHT NOW?
#
#   scripts/tier-probe.sh <provider>        # exit 0 healthy, 1 unreachable, 2 unknown provider
#
# The journal's provider_failure entries are HISTORY. On 2026-09-29 the free glm
# tier was down for a redeploy window; by the time the seed cycle read 87
# failures from the last 30 minutes the endpoint was already back, and the cycle
# suppressed new work and filed a P1 for a problem that had fixed itself. A
# storm is therefore only believed when a live probe agrees with it.
#
# The provider's baseUrl and key come from pi's own models.json, so this cannot
# drift from what a seat would actually dial.
set -uo pipefail

PROVIDER=${1:?usage: tier-probe.sh <provider>}
MODELS=${PI_MODELS_JSON:-$HOME/.pi/agent/models.json}
TIMEOUT=${TIER_PROBE_TIMEOUT:-8}

base=$(python3 - "$MODELS" "$PROVIDER" <<'PY'
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except (OSError, ValueError):
    sys.exit(0)
provs = d.get("providers", d)
p = provs.get(sys.argv[2])
print((p or {}).get("baseUrl", ""))
PY
)
if [ -z "$base" ]; then
	printf 'tier-probe: %s has no baseUrl in %s\n' "$PROVIDER" "$MODELS"
	exit 2
fi

# The key. A timer-run probe has no interactive shell environment, so an env var
# alone is not enough: the daemon resolves its key from a kubectl secret
# (`[harness.env_from_cmd.<VAR>]` in .agentctl/config.toml) and the probe
# resolves it the same way rather than keeping a second copy of the answer.
key=${BM_LLMS_API_KEY:-}
if [ -z "$key" ]; then
	key=$(python3 - "${GORGE_TARGET_REPO:-/home/sadams/projects/gorge}/.agentctl/config.toml" <<'PY' 2>/dev/null
import base64, shlex, subprocess, sys, tomllib
try:
    cfg = tomllib.load(open(sys.argv[1], "rb"))
except (OSError, ValueError):
    sys.exit(0)
for var, spec in ((cfg.get("harness") or {}).get("env_from_cmd") or {}).items():
    cmd = spec.get("cmd")
    if not cmd:
        continue
    try:
        out = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
    except (OSError, subprocess.SubprocessError):
        continue
    if out.returncode != 0:
        continue
    val = out.stdout.strip()
    if spec.get("decode") == "base64":
        try:
            val = base64.b64decode(val).decode().strip()
        except Exception:
            continue
    if val:
        print(val)
        break
PY
	)
fi

code=$(curl -s -o /dev/null -m "$TIMEOUT" -w '%{http_code}' \
	${key:+-H "Authorization: Bearer $key"} "$base/models" 2>/dev/null)
case $code in
200)
	printf 'tier-probe: %s healthy (%s/models 200)\n' "$PROVIDER" "$base"
	exit 0
	;;
401 | 403)
	# The endpoint IS up; this probe just could not present a key the server
	# accepts. The daemon holds its own key, so calling the tier down here
	# would withhold work for a probe limitation -- which is exactly the class
	# of mistake this script exists to prevent.
	if [ -n "$key" ]; then
		printf 'tier-probe: %s reachable but REJECTED the resolved key (%s/models http %s) -- likely a stale key, not an outage\n' \
			"$PROVIDER" "$base" "$code"
	else
		printf 'tier-probe: %s reachable, no key available to this probe (%s/models http %s)\n' \
			"$PROVIDER" "$base" "$code"
	fi
	exit 0
	;;
000)
	printf 'tier-probe: %s unreachable (%s/models no answer in %ss)\n' "$PROVIDER" "$base" "$TIMEOUT"
	exit 1
	;;
*)
	printf 'tier-probe: %s unhealthy (%s/models http %s)\n' "$PROVIDER" "$base" "$code"
	exit 1
	;;
esac
