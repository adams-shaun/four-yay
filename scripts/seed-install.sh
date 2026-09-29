#!/usr/bin/env bash
# seed-install.sh — install (or remove) the reward loop's systemd user units.
#
#   scripts/seed-install.sh install     copy the units, reload, enable the timer
#   scripts/seed-install.sh status      timer state, last cycle, next firing
#   scripts/seed-install.sh uninstall   disable the timer, remove the units
#
# The units are user units, so they run as the operator with the operator's
# environment and need no root. The slices are installed too: they are what
# actually bounds heavy work, and a transient --slice= without a unit file gets
# no limits at all (verified 2026-09-29: MemoryMax read back as infinity).
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel)
DEST=${SYSTEMD_USER_DIR:-$HOME/.config/systemd/user}
UNITS=(gorge-seed.service gorge-seed.timer gorge-heavy.slice gorge-probe.slice)

say() { printf 'seed-install: %s\n' "$*"; }

case ${1:-status} in
install)
	mkdir -p "$DEST"
	for u in "${UNITS[@]}"; do
		install -m 0644 "$ROOT/deploy/$u" "$DEST/$u"
		say "installed $u"
	done
	systemctl --user daemon-reload
	systemctl --user enable --now gorge-seed.timer
	say "timer enabled; next firing:"
	systemctl --user list-timers gorge-seed.timer --no-pager
	;;
status)
	systemctl --user list-timers gorge-seed.timer --no-pager
	printf '\n'
	systemctl --user status gorge-seed.service --no-pager -n 12 2>&1 | head -20
	printf '\nlast cycle journal entry:\n'
	tail -n 1 "$ROOT/.ds4/reward/seed-journal.jsonl" 2>/dev/null | cut -c1-400
	;;
uninstall)
	systemctl --user disable --now gorge-seed.timer 2>/dev/null
	for u in "${UNITS[@]}"; do rm -f "$DEST/$u"; done
	systemctl --user daemon-reload
	say "removed"
	;;
*)
	printf 'usage: seed-install.sh {install|status|uninstall}\n' >&2
	exit 2
	;;
esac
