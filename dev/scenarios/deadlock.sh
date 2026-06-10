#!/usr/bin/env bash
# Transient deadlock: two sessions grab rows in opposite order. PostgreSQL
# detects the cycle within deadlock_timeout (1s here) and aborts one victim.
# Useful to see a cycle flash in pglockr and confirm it resolves itself.
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

spawn dlx -v first=1 -v second=2 -f /scenarios/deadlock.sql
spawn dly -v first=2 -v second=1 -f /scenarios/deadlock.sql

echo "deadlock: started two crossing sessions; PG aborts one within ~1-2s (see 'make logs')."
