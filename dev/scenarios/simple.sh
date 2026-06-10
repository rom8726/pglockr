#!/usr/bin/env bash
# Simplest case: one idle-in-transaction holder, one waiter.
#   waiter(row1) ──▶ holder(row1, ROOT, idle in transaction)
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

spawn holder -v id=1 -f /scenarios/lock_row.sql
settle
spawn waiter -v id=1 -f /scenarios/lock_row.sql

echo "simple: holder holds row 1 (idle in transaction, ROOT); waiter blocks on it."
