package report

import (
	"reflect"
	"strings"
	"testing"

	"github.com/clarivo-co/sitewarm/internal/cdn"
	"github.com/clarivo-co/sitewarm/internal/probe"
	"github.com/clarivo-co/sitewarm/internal/verify"
)

func TestDefaultFailOn(t *testing.T) {
	f, err := ParseFailOn(DefaultFailOn)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"miss", "client-error", "error", "redirect-error", "incomplete", "noindex"}
	if !reflect.DeepEqual(f.List(), want) {
		t.Fatalf("default --fail-on = %v, want %v", f.List(), want)
	}
	cases := []struct {
		state cdn.State
		idx   probe.Indexability
		fails bool
	}{
		{cdn.Hit, probe.Indexable, false},
		{cdn.Miss, probe.Indexable, true},
		{cdn.ClientError, "", true},
		{cdn.Error, "", true},
		{cdn.RedirectError, "", true},
		{cdn.Incomplete, "", true},
		{cdn.Hit, probe.NoIndex, true},
		{cdn.Hit, probe.IndexabilityError, true},
		// Not failing by default: highlighted only.
		{cdn.Hit, probe.IndexabilityInconclusive, false},
		{cdn.Stale, probe.Indexable, false},
		{cdn.Bypass, probe.Indexable, false},
		{cdn.Unknown, probe.Indexable, false},
		{cdn.VariantUnverifiable, probe.Indexable, false},
		{cdn.Challenged, "", false},
		{cdn.SkippedRobots, "", false},
	}
	for _, c := range cases {
		if got := f.Fails(c.state, c.idx); got != c.fails {
			t.Errorf("Fails(%s, %s) = %v, want %v", c.state, c.idx, got, c.fails)
		}
	}
}

func TestParseFailOn(t *testing.T) {
	f, err := ParseFailOn("bypass, indexability-inconclusive")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Fails(cdn.Bypass, "") || !f.Fails(cdn.Hit, probe.IndexabilityInconclusive) || f.Fails(cdn.Miss, "") {
		t.Fatalf("unexpected set %v", f.List())
	}
	if _, err := ParseFailOn("hit"); err == nil {
		t.Fatal("hit is not a failure state")
	}
	if _, err := ParseFailOn("bogus"); err == nil {
		t.Fatal("want error for unknown token")
	}
	if f, _ := ParseFailOn(""); len(f) != 0 {
		t.Fatal("empty --fail-on must fail on nothing")
	}
}

func TestBuildSummaryAndRedaction(t *testing.T) {
	failOn, _ := ParseFailOn(DefaultFailOn)
	hit := probe.Observation{Header: map[string][]string{"Set-Cookie": {"session=secret"}, "Cf-Cache-Status": {"HIT"}}, Status: 200, Indexability: probe.Indexable}
	outs := []verify.Outcome{
		{State: cdn.Hit, GotHit: true, Last: &hit, First: &hit},
		{State: cdn.Incomplete},
	}
	r := &Report{}
	Build(r, outs, failOn)
	if r.Summary.Total != 2 || r.Summary.Failed != 1 || r.Summary.Passed || r.Summary.HitRatio != 0.5 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if !r.Results[0].SetCookie {
		t.Fatal("Set-Cookie presence must be recorded")
	}
	if len(r.Warnings) != 2 {
		t.Fatalf("standing warnings missing: %v", r.Warnings)
	}
}

func TestSkippedRobotsFailOn(t *testing.T) {
	f, err := ParseFailOn("skipped-robots")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Fails(cdn.SkippedRobots, "") {
		t.Fatal("skipped-robots must fail when added to --fail-on")
	}
}

func TestBuildKeepsSourceWarnings(t *testing.T) {
	r := &Report{Warnings: []string{"robots.txt returned HTTP 503"}}
	Build(r, nil, FailOn{})
	if len(r.Warnings) != 3 || r.Warnings[2] != "robots.txt returned HTTP 503" {
		t.Fatalf("warnings = %v", r.Warnings)
	}
}

// A run where every result is skipped-robots verified nothing: it fails
// even though skipped-robots is not in --fail-on.
func TestBuildNoneVerifiedFails(t *testing.T) {
	failOn, _ := ParseFailOn(DefaultFailOn)
	p := verify.Pair{URL: "https://example.com/"}
	r := &Report{}
	Build(r, []verify.Outcome{SkippedOutcome(p), SkippedOutcome(p)}, failOn)
	if r.Summary.Passed || r.Summary.Failed != 0 || r.Summary.ByState[cdn.SkippedRobots] != 2 {
		t.Fatalf("summary = %+v", r.Summary)
	}
	if !strings.Contains(r.Summary.Verdict, "no URL was verified") {
		t.Fatalf("verdict %q", r.Summary.Verdict)
	}

	hit := probe.Observation{Status: 200, Indexability: probe.Indexable}
	r = &Report{}
	Build(r, []verify.Outcome{SkippedOutcome(p), {Pair: p, State: cdn.Hit, Last: &hit}}, failOn)
	if !r.Summary.Passed {
		t.Fatalf("one verified hit and one skip must pass: %+v", r.Summary)
	}
}

// The verdict must not claim a verified cache unless a cache state was
// observed. Exit semantics are unchanged: unknown is not in the default
// --fail-on, so an all-unknown run still passes.
func TestBuildVerdictWording(t *testing.T) {
	failOn, _ := ParseFailOn(DefaultFailOn)
	p := verify.Pair{URL: "https://example.com/"}
	obs := probe.Observation{Status: 200, Indexability: probe.Indexable}
	out := func(st cdn.State) verify.Outcome {
		return verify.Outcome{Pair: p, State: st, GotHit: st == cdn.Hit, Last: &obs, First: &obs}
	}
	tests := []struct {
		name       string
		outs       []verify.Outcome
		passed     bool
		verdict    string
		notContain string
	}{
		{"all unknown", []verify.Outcome{out(cdn.Unknown), out(cdn.Unknown)}, true,
			"no cache state observed: all 2 requested results are unknown; deploy freshness not verified", "cache verified"},
		{"all unknown with skipped-robots", []verify.Outcome{out(cdn.Unknown), out(cdn.SkippedRobots), out(cdn.Unknown)}, true,
			"no cache state observed: all 2 requested results are unknown; deploy freshness not verified", "cache verified"},
		{"mixed hit and unknown", []verify.Outcome{out(cdn.Hit), out(cdn.Unknown)}, true,
			verdictPass, ""},
		{"all hit", []verify.Outcome{out(cdn.Hit), out(cdn.Hit)}, true,
			verdictPass, ""},
		{"all miss", []verify.Outcome{out(cdn.Miss), out(cdn.Miss)}, false,
			"failed: 2 of 2 results are in --fail-on", "cache verified"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Report{}
			Build(r, tt.outs, failOn)
			if r.Summary.Passed != tt.passed || r.Summary.Verdict != tt.verdict {
				t.Fatalf("passed=%v verdict=%q, want passed=%v verdict=%q", r.Summary.Passed, r.Summary.Verdict, tt.passed, tt.verdict)
			}
			if tt.notContain != "" && strings.Contains(r.Summary.Verdict, tt.notContain) {
				t.Fatalf("verdict %q must not contain %q", r.Summary.Verdict, tt.notContain)
			}
			requireStatesKept(t, r, tt.outs)
		})
	}
}

// requireStatesKept fails when Build changed a result state, so unknown
// stays unknown whatever the verdict says.
func requireStatesKept(t *testing.T, r *Report, outs []verify.Outcome) {
	t.Helper()
	for i, e := range r.Results {
		if e.State != outs[i].State {
			t.Fatalf("result %d state is %s, want %s", i, e.State, outs[i].State)
		}
	}
}

// Regression for the Python CLI's CSV export.
func TestWriteCSV(t *testing.T) {
	r := &Report{Results: []Entry{
		{URL: "https://example.com/a", Profile: "desktop", HTTPStatus: 200, State: cdn.Hit},
		{URL: "https://example.com/a", Profile: "mobile", HTTPStatus: 200, State: cdn.Miss},
		{URL: "https://example.com/b,c", Profile: "desktop", State: cdn.Error},
		{URL: "https://example.com/d", Profile: "desktop", HTTPStatus: 503, State: cdn.Error},
		{URL: "https://example.com/e", Profile: "desktop", State: cdn.SkippedRobots},
	}}
	var b strings.Builder
	if err := WriteCSV(&b, r); err != nil {
		t.Fatal(err)
	}
	want := "url,status,state\n" +
		"https://example.com/a,200,hit\n" +
		"https://example.com/a,200,miss\n" +
		"\"https://example.com/b,c\",,error\n" +
		"https://example.com/d,503,error\n" +
		"https://example.com/e,,skipped-robots\n"
	if b.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", b.String(), want)
	}
}
