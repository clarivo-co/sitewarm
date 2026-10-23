#!/usr/bin/env bash
# Tests for scripts/t2.sh. Run: bash scripts/t2_test.sh (needs jq).
set -euo pipefail

t2="$(cd "$(dirname "$0")" && pwd)/t2.sh"
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT
cd "$dir"
failures=0

fail() { echo "FAIL: $*"; failures=$((failures + 1)); }

# expect <name> <exit code> <output substring> <command...>
expect() {
	local name=$1 want=$2 needle=$3 got=0 out
	shift 3
	out=$("$@" 2>&1) || got=$?
	[ "$got" = "$want" ] || { fail "$name: exit $got, want $want"; echo "$out"; return; }
	case $out in *"$needle"*) echo "ok: $name" ;; *) fail "$name: output lacks \"$needle\""; echo "$out" ;; esac
}

# report <file> <pop> <stopped> <state:ttfb>...
report() {
	local file=$1 pop=$2 stopped=$3 results="" sep="" st tt
	shift 3
	for r in "$@"; do
		st=${r%%:*} tt=${r##*:}
		results="$results$sep{\"url\":\"https://e.test/$RANDOM\",\"state\":\"$st\",\"pop\":\"$pop\",\"ttfb_first_ms\":$tt}"
		sep=,
	done
	[ "$stopped" != - ] || stopped=""
	printf '{"tool":"sitewarm","stopped":"%s","results":[%s]}\n' "$stopped" "$results" >"$file"
}

# split
{
	echo "# exported 2026-10-17"
	echo "url,clicks"
	for i in $(seq 1 11); do echo "https://e.test/p$i,$i"; done
	echo "https://e.test/p1,1"
	echo
} >top.csv
expect "split deals 11 unique rows 6/5" 0 "6 URLs in warmed.csv, 5 in control.csv" bash "$t2" split top.csv
[ "$(head -n 1 warmed.csv)" = "url,clicks" ] && [ "$(head -n 1 control.csv)" = "url,clicks" ] ||
	fail "split keeps the header in both files"
[ "$(tail -n +2 warmed.csv | cat - <(tail -n +2 control.csv) | sort | uniq -d)" = "" ] ||
	fail "split puts no URL in both groups"
[ "$(tail -n +2 warmed.csv | cat - <(tail -n +2 control.csv) | wc -l | tr -d ' ')" = 11 ] ||
	fail "split keeps every unique URL"
T2_SEED=7 bash "$t2" split top.csv a1.csv b1.csv >/dev/null
T2_SEED=7 bash "$t2" split top.csv a2.csv b2.csv >/dev/null
if ! { cmp -s a1.csv a2.csv && cmp -s b1.csv b2.csv; }; then
	fail "T2_SEED makes the split reproducible"
fi
printf 'loc\nhttps://e.test/\n' >bad.csv
expect "split rejects a file without url/path header" 2 "needs a header" bash "$t2" split bad.csv

# compare
report w.json FRA - hit:40 hit:50 hit:60 miss:300
report c.json FRA - hit:45 miss:280 miss:300 miss:320
expect "pass on HIT share" 0 "verdict: pass: HIT +50.0 points" bash "$t2" compare w.json c.json

report w.json FRA - miss:100 miss:120 hit:30
report c.json FRA - miss:400 miss:420 hit:30
expect "pass on TTFB alone" 0 "median TTFB 300 ms faster" bash "$t2" compare w.json c.json

report w.json FRA - hit:300 miss:300 miss:300 miss:300 miss:300
report c.json FRA - miss:300 miss:300 miss:300 miss:300 miss:300
expect "exactly 20 points passes" 0 "verdict: pass: HIT +20.0 points" bash "$t2" compare w.json c.json

report w.json FRA - miss:100 miss:100
report c.json FRA - miss:300 miss:300
expect "exactly 200 ms passes" 0 "median TTFB 200 ms faster" bash "$t2" compare w.json c.json

report w.json FRA - hit:300 miss:300 miss:300 miss:300 miss:300 miss:300
report c.json FRA - miss:350 miss:350 miss:350 miss:350 miss:350 miss:350
expect "not met: 16.7 points and 50 ms" 1 "verdict: not-met: HIT +16.7 points, median TTFB 50 ms faster" bash "$t2" compare w.json c.json
expect "comma-decimal locale still prints a verdict" 1 "verdict: not-met: HIT +16.7 points" \
	env LC_ALL=de_DE.UTF-8 LANG=de_DE.UTF-8 bash "$t2" compare w.json c.json

report w.json FRA - hit:10 hit:10
report c.json AMS - miss:500 miss:500
expect "inconclusive on different PoPs" 3 "different PoPs" bash "$t2" compare w.json c.json

report w.json FRA run-timeout hit:10
report c.json FRA - miss:500
expect "inconclusive when a run stopped" 3 "stopped early (warmed: run-timeout" bash "$t2" compare w.json c.json

report w.json FRA -
report c.json FRA - miss:500
expect "inconclusive on an empty group" 3 "a group has no results" bash "$t2" compare w.json c.json

report w.json FRA - hit:null hit:null
report c.json FRA - miss:null miss:null
expect "no TTFB still decides on HIT" 0 "no TTFB to compare" bash "$t2" compare w.json c.json

echo '[]' >notreport.json
expect "rejects a non-report file" 2 "not a sitewarm report" bash "$t2" compare notreport.json c.json
expect "usage error" 2 "split top.csv" bash "$t2" compare w.json

[ "$failures" = 0 ] || { echo "$failures failure(s)"; exit 1; }
echo "all t2.sh tests passed"
