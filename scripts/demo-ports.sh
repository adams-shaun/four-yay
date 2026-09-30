#!/usr/bin/env bash
# demo-ports.sh — the ONE home for "which ports must the demo port sweep cover".
#
# deploy-demo.sh retires a demo listener by EMPTYING its *_PORT variable
# (OMNI_PORT and MB_PORT are both retired that way today). Before this helper,
# the sweep's port set was built only from the CURRENT values of PUB_PORT,
# OMNI_PORT and MB_PORT, so emptying a variable also removed that port from the
# sweep -- and the server a previous layout had left bound there was never
# stopped again.
#
# That is exactly how the stability event of 2026-09-30 happened: commit
# 09750f374 changed MB_PORT=${MB_PORT-8081} to MB_PORT=${MB_PORT-}, and the
# -manabrew gorged still listening on 127.0.0.1:8081 (started seconds earlier
# by the previous layout) fell out of every later deploy's sweep. It stood on
# :8081 for hours as a second standing (agent-unowned) gorged process, which is
# standing_gorged_excess=1 -- the 100x stability veto.
#
# So the sweep is the UNION of every port the demo has EVER bound and every
# port this run binds. DEMO_PORT_HISTORY is append-only: adding a listener on a
# NEW port means appending that port here in the same change that introduces
# it, so retiring it later can never take a previous layout's server off the
# sweep. Demo ports are, by construction, only ever PUB_PORT, OMNI_PORT and
# MB_PORT values, so this set is the complete history.
DEMO_PORT_HISTORY=${DEMO_PORT_HISTORY:-"8080 8081"}

# demo_sweep_ports <bind-port>... — every port the sweep must match, one per
# line, deduped and sorted. Blank/empty arguments are ignored, so a retired
# *_PORT (empty) contributes nothing instead of poisoning the regex with `|`.
demo_sweep_ports() {
	printf '%s\n' $DEMO_PORT_HISTORY "$@" |
		grep -E '^[0-9]+$' | sort -u
}
