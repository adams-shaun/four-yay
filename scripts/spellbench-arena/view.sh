#!/bin/sh
# view.sh TRACE GAME_ID: the decisions of one game from an sbv1agent -trace
# file, one header + the picked candidate each (pure passes elided).
awk -v g="$2" '/^m[0-9]+p[0-9]+g[0-9] #/{show=($1==g); hdr=$0; next} show && /^ \*/{ if ($0 ~ / pass$/) next; print substr(hdr,1,230); print "      " $0 }' "$1"
