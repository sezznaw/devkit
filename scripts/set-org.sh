#!/bin/sh
# Replace the placeholder GitHub organisation "sezznaw" across the repository.
#
#   scripts/set-org.sh <github-org-or-user>
#   scripts/set-org.sh acme-inc
set -eu
[ $# -eq 1 ] || { echo "usage: $0 <github-org-or-user>" >&2; exit 1; }
org=$1
cd "$(dirname "$0")/.."
files=$(grep -rl --exclude-dir=.git --exclude-dir=bin --exclude-dir=dist -e 'sezznaw' . || true)
for f in $files; do
  sed -i.tmp -e "s#sezznaw#$org#g" "$f" && rm -f "$f.tmp"
done
echo "updated:"; printf '  %s\n' $files
echo "now run: go mod tidy && make build"
