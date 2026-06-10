#!/usr/bin/env bash
# Forest: two independent blocking trees at once (two separate roots).
#   waiterA ──▶ rootA(row1)        waiterB ──▶ rootB(row2)
source "$(dirname "${BASH_SOURCE[0]}")/_lib.sh"

spawn rootA -v id=1 -f /scenarios/lock_row.sql
spawn rootB -v id=2 -f /scenarios/lock_row.sql
settle
spawn waiterA -v id=1 -f /scenarios/lock_row.sql
spawn waiterB -v id=2 -f /scenarios/lock_row.sql

echo "forest: two independent trees — rootA<-waiterA and rootB<-waiterB."
