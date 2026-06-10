#!/usr/bin/env bash
# Three-deep chain — exercises a mid-chain node (holds one lock, waits on another):
#   leaf(row2) ──▶ mid(holds row2, waits row1) ──▶ root(row1, idle in transaction)
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

spawn root -v id=1 -f /scenarios/lock_row.sql            # holds row 1, idle
settle
spawn mid  -v hold=2 -v wait=1 -f /scenarios/lock_two.sql # holds row 2, blocks on row 1
settle
spawn leaf -v id=2 -f /scenarios/lock_row.sql            # blocks on row 2

echo "chain: leaf -> mid -> root (depth 3). 'mid' is a blocker that is itself blocked."
