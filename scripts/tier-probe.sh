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

# A local vLLM/sglang endpoint answers /v1/models with or without a key; a
# hosted one needs the key it is configured with. Try the configured key when
# there is an env var for it, then bare.
key=${BM_LLMS_API_KEY:-}
code=$(curl -s -o /dev/null -m "$TIMEOUT" -w '%{http_code}' \
	${key:+-H "Authorization: Bearer $key"} "$base/models" 2>/dev/null)
case $code in
200)
	printf 'tier-probe: %s healthy (%s/models 200)\n' "$PROVIDER" "$base"
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
