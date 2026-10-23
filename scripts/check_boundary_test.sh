#!/usr/bin/env bash
# Tests for scripts/check_boundary.sh. Run: bash scripts/check_boundary_test.sh
set -euo pipefail

check="$(cd "$(dirname "$0")" && pwd)/check_boundary.sh"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
failures=0

fail() { echo "FAIL: $*"; failures=$((failures + 1)); }

# repo <name> <path> <content>: a fresh git repo holding one tracked file.
repo() {
	local name=$1 path=$2 content=$3
	mkdir -p "$dir/$name/$(dirname "$path")"
	printf '%s\n' "$content" >"$dir/$name/$path"
	git -C "$dir/$name" init -q
	git -C "$dir/$name" add -A
}

# expect <name> <exit code> <output substring>
expect() {
	local name=$1 want=$2 needle=$3 got=0 out
	out=$(bash "$check" "$dir/$name" 2>&1) || got=$?
	[ "$got" = "$want" ] || { fail "$name: exit $got, want $want"; echo "$out"; return; }
	case $out in *"$needle"*) echo "ok: $name" ;; *) fail "$name: output lacks \"$needle\""; echo "$out" ;; esac
}

repo clean README.md 'sitewarm warms a CDN. See /pricing on your own site.'
expect clean 0 "boundary: ok"

repo private-repo notes.md 'see sitemap_saas for the hosted service'
expect private-repo 1 "sitemap_saas"

repo adr notes.md 'decided in ADR 0002'
expect adr 1 "ADR"

repo price notes.md "the plan costs \$29/mo"
expect price 1 'pattern'

repo local-path notes.md 'cd /Volumes/disk/repo'
expect local-path 1 "/Volumes/"

repo secret notes.md 'key sk_live_abcdefghij1234567890'
expect secret 1 "sk_(live|test)"

repo private-path docs/adr/0001.md 'anything'
expect private-path 1 "private path"

repo allowed notes.md 'ADR 0002 is mentioned here boundary:allow'
expect allowed 0 "boundary: ok"

repo legacy-skipped legacy/old.md 'cd /Users/someone/x'
expect legacy-skipped 0 "boundary: ok"

if [ "$failures" -gt 0 ]; then
	echo "$failures failure(s)"
	exit 1
fi
echo "all ok"
