#!/usr/bin/env bash
# t2.sh: measure whether warming after a deploy helps, with a random
# warmed/control split of the same URL list on the same deploy.
#
#   scripts/t2.sh split top.csv [warmed.csv control.csv]
#       Shuffles the data rows of top.csv and deals them alternately into the
#       two files, so the groups differ in size by at most one. Both keep the
#       header. Blank lines, # comments and repeated rows are dropped. One URL
#       per line: quoted fields with newlines are not supported. Set T2_SEED
#       to an integer to make the split reproducible.
#
#   scripts/t2.sh compare warmed.json control.json
#       Reads two `sitewarm scan --report` files taken 5 minutes after the
#       deploy with one request per URL (--max-wait 0) and prints, per group,
#       the HIT share and the median ttfb_first_ms, plus a verdict:
#         pass          warmed has at least 20 points more HIT, or a median
#                       first-byte time at least 200 ms lower (exit 0)
#         not-met       neither threshold is reached (exit 1)
#         inconclusive  the groups were served by different PoPs, a run
#                       stopped early, or a group has no results (exit 3)
#       Usage errors exit 2. Needs jq.
#
# The procedure:
#   1. Before the deploy: scripts/t2.sh split top.csv
#   2. Deploy with a cache purge or a new build.
#   3. Right after: sitewarm scan --top-from warmed.csv --report warm.json
#      Leave the control group alone.
#   4. Five minutes after the deploy, one request per URL in both groups:
#      sitewarm scan --top-from warmed.csv --max-wait 0 --fail-on "" --report w5.json
#      sitewarm scan --top-from control.csv --max-wait 0 --fail-on "" --report c5.json
#   5. scripts/t2.sh compare w5.json c5.json
# Run every step from the same machine, against one CDN, ideally off-peak:
# real visitors warm the control group too.
set -euo pipefail

min_hit_points=20
min_ttfb_ms=200

usage() {
	sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//' >&2
	exit 2
}

split_list() {
	local in=$1 warmed=${2:-warmed.csv} control=${3:-control.csv}
	[ -r "$in" ] || { echo "t2: cannot read $in" >&2; exit 2; }
	local header
	header=$(grep -v -e '^[[:space:]]*$' -e '^#' "$in" | head -n 1 | tr -d '\r')
	case $(printf '%s' "$header" | tr '[:upper:]' '[:lower:]') in
	*url* | *path*) ;;
	*) echo "t2: $in needs a header with a url or path column" >&2; exit 2 ;;
	esac
	printf '%s\n' "$header" >"$warmed"
	printf '%s\n' "$header" >"$control"
	grep -v -e '^[[:space:]]*$' -e '^#' "$in" | tr -d '\r' | tail -n +2 |
		awk -v seed="${T2_SEED:-}" '
			BEGIN { if (seed != "") srand(seed); else srand() }
			!seen[$0]++ { printf "%.9f\t%s\n", rand(), $0 }' |
		sort -n | cut -f2- |
		awk -v w="$warmed" -v c="$control" '
			{ if (NR % 2) print >> w; else print >> c }'
	echo "t2: $(($(wc -l <"$warmed") - 1)) URLs in $warmed, $(($(wc -l <"$control") - 1)) in $control"
}

# Prints: hit_points median_ttfb_ms results pops stopped
group_stats() {
	jq -r '
		def median: sort | length as $n
			| if $n == 0 then "none"
			  elif $n % 2 == 1 then .[($n - 1) / 2]
			  else (.[$n / 2 - 1] + .[$n / 2]) / 2 end;
		(.results // []) as $r
		| ($r | length) as $n
		| [ (if $n == 0 then 0 else ([$r[] | select(.state == "hit")] | length) * 100 / $n end),
		    ([$r[] | .ttfb_first_ms | select(. != null)] | median),
		    $n,
		    ([$r[] | .pop // "" | select(. != "")] | unique | join(",") | if . == "" then "-" else . end),
		    (.stopped // "" | if . == "" then "-" else . end) ]
		| map(tostring) | join(" ")' "$1"
}

compare_reports() {
	command -v jq >/dev/null || { echo "t2: jq is required" >&2; exit 2; }
	local f
	for f in "$1" "$2"; do
		[ -r "$f" ] || { echo "t2: cannot read $f" >&2; exit 2; }
		jq -e 'type == "object" and has("results")' "$f" >/dev/null 2>&1 ||
			{ echo "t2: $f is not a sitewarm report" >&2; exit 2; }
	done
	local wh wt wn wp ws ch ct cn cp cs
	read -r wh wt wn wp ws <<<"$(group_stats "$1")"
	read -r ch ct cn cp cs <<<"$(group_stats "$2")"
	printf '%-8s %8s %10s %18s  %s\n' group results HIT% "median TTFB (ms)" PoPs
	printf '%-8s %8s %10.1f %18s  %s\n' warmed "$wn" "$wh" "$wt" "$wp"
	printf '%-8s %8s %10.1f %18s  %s\n' control "$cn" "$ch" "$ct" "$cp"

	local why=""
	[ "$wn" -gt 0 ] && [ "$cn" -gt 0 ] || why="a group has no results"
	[ "$ws" = - ] && [ "$cs" = - ] || why="${why:+$why; }a run stopped early (warmed: $ws, control: $cs)"
	[ "$wp" = "$cp" ] || why="${why:+$why; }the groups were served by different PoPs"
	if [ -n "$why" ]; then
		echo "verdict: inconclusive: $why; repeat the measurement"
		exit 3
	fi
	[ "$wp" != - ] || echo "warning: no PoP in either report; the same-PoP check could not run"

	local verdict
	verdict=$(awk -v wh="$wh" -v ch="$ch" -v wt="$wt" -v ct="$ct" \
		-v mh="$min_hit_points" -v mt="$min_ttfb_ms" 'BEGIN {
			dh = wh - ch
			msg = sprintf("HIT %+.1f points", dh)
			ok = dh >= mh
			if (wt != "none" && ct != "none") {
				dt = ct - wt
				msg = msg sprintf(", median TTFB %d ms faster", dt)
				ok = ok || dt >= mt
			} else {
				msg = msg ", no TTFB to compare"
			}
			print (ok ? "pass" : "not-met") ": " msg \
				sprintf(" (needs >= %d points or >= %d ms)", mh, mt)
		}')
	echo "verdict: $verdict"
	case $verdict in pass*) exit 0 ;; *) exit 1 ;; esac
}

case ${1:-} in
split) [ $# -ge 2 ] && [ $# -le 4 ] || usage; shift; split_list "$@" ;;
compare) [ $# -eq 3 ] || usage; compare_reports "$2" "$3" ;;
*) usage ;;
esac
