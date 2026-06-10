#!/usr/bin/env bash
# Fan-out: one head blocker with several waiters queued behind it.
#   waiter1 ─┐
#   waiter2 ─┼─▶ root(row1, idle in transaction)
#   waiter3 ─┘
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

spawn root -v id=1 -f /scenarios/lock_row.sql
settle
for i in 1 2 3 4; do
  spawn "waiter${i}" -v id=1 -f /scenarios/lock_row.sql
done

echo "fanout: 4 waiters all blocked on a single ROOT holding row 1."
