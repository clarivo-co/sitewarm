#!/usr/bin/env bash
# check_boundary.sh: keep private material out of this public repository.
#
#   scripts/check_boundary.sh [repo-dir]
#
# Scans the files git tracks (legacy/ excluded) and exits 1 when it finds
#   - a path that belongs in the private repository: docs/adr/, plans/,
#     .claude/ or CLAUDE.md;
#   - a reference to the private repository (sitemap_saas), an ADR number,
#     a recurring price such as $29/mo, or a local absolute path;
#   - something that looks like a secret.
# A line that contains "boundary:allow" is skipped. This script and its test
# are skipped too, since they spell out the patterns.
set -euo pipefail

root=${1:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$root"

paths='^(docs/adr/|docs/plans?/|plans?/|\.claude/|CLAUDE\.md$)'
content=(
	'sitemap_saas'
	'\bADR[ -]?[0-9]{3,4}\b'
	'\$[0-9][0-9,.]*[[:space:]]*/[[:space:]]*(mo|month|seat|user)\b'
	'/Volumes/|/Users/[A-Za-z]'
	'sk_(live|test)_[0-9A-Za-z]{10,}'
	'-----BEGIN [A-Z ]*PRIVATE KEY-----'
	'gh[pousr]_[0-9A-Za-z]{30,}'
	'AKIA[0-9A-Z]{16}'
)

failures=0
scanned=()
while IFS= read -r -d '' f; do
	case $f in
	legacy/* | scripts/check_boundary.sh | scripts/check_boundary_test.sh) continue ;;
	esac
	if [[ $f =~ $paths ]]; then
		echo "boundary: $f is a private path"
		failures=$((failures + 1))
		continue
	fi
	scanned+=("$f")
done < <(git ls-files -z)

for pat in "${content[@]}"; do
	[ "${#scanned[@]}" -gt 0 ] || break
	hits=$(grep -IHnE -e "$pat" -- "${scanned[@]}" | grep -v 'boundary:allow' || true)
	if [ -n "$hits" ]; then
		echo "boundary: pattern $pat"
		echo "  ${hits//$'\n'/$'\n'  }"
		failures=$((failures + 1))
	fi
done

if [ "$failures" -gt 0 ]; then
	echo "boundary: $failures problem(s); this repository is public"
	exit 1
fi
echo "boundary: ok (${#scanned[@]} files)"
