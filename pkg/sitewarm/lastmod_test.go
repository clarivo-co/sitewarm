package sitewarm_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sitewarm/sitewarm/pkg/sitewarm"
)

// lastmod comes from the sitemap, is the same for every profile of a URL,
// is left out of the JSON when the sitemap has none, and does not touch the
// state or the verdict.
func TestScanFromSitemapReportsLastMod(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(404))
	res, err := sitewarm.Scan(context.Background(), sitemapOptions(s))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Summary.Passed {
		t.Fatalf("lastmod must not change the verdict: %+v", res.Summary)
	}
	seen := map[string]int{}
	for _, e := range res.Results {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(string(b), `"lastmod"`)
		switch {
		case strings.HasSuffix(e.URL, "/a"):
			if e.LastMod != "2026-10-01" || !has {
				t.Fatalf("%s %s: lastmod %q, json %s", e.URL, e.Profile, e.LastMod, b)
			}
		default:
			if e.LastMod != "" || has {
				t.Fatalf("%s %s: lastmod must be omitted, got %q in %s", e.URL, e.Profile, e.LastMod, b)
			}
		}
		seen[e.URL]++
	}
	if len(seen) != 3 {
		t.Fatalf("results = %v", seen)
	}
}
