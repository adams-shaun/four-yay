#!/bin/sh
# Runs sbagent with its stderr (the -stats line, diagnostics) appended to a per-process log.
exec /mnt/sata/gorge-training/sb-bridge2/bin/sbagent "$@" 2>>"${SBAGENT_LOGDIR:-/mnt/sata/gorge-training/sb-bridge2/logs}/sbagent-$$.log"
