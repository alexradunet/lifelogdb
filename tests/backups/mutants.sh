#!/bin/sh
for m in backup_instead_of_vacuum no_orphan_check no_fk_check no_integrity verify_after_rename no_tmp_name prune_keeps_15 prune_no_monthly offbox_optional no_sha_check no_wal_removal no_wal_mode restore_no_fk restore_delete_old; do
  out=$(timeout 200 ${PYTHON:-python3} "$(dirname "$0")/scripts_test.py" --mutate $m < /dev/null 2>&1)
  tail1=$(echo "$out" | tail -1)
  failing=$(echo "$out" | grep '^FAIL' | cut -c1-110 | head -2 | tr '\n' '|')
  printf '%-26s %s  %s\n' "$m" "$tail1" "$failing"
done
