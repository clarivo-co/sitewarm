package source

import (
	"context"
	"reflect"
	"testing"
)

func TestValidLastMod(t *testing.T) {
	for _, s := range []string{
		"2026", "2026-10", "2026-10-01",
		"2026-10-01T08:30Z", "2026-10-01T08:30:15Z",
		"2026-10-01T08:30:15+02:00", "2026-10-01T08:30:15.123Z",
	} {
		if !validLastMod(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{
		"", "yesterday", "2026-13-01", "2026-10-32", "10/01/2026",
		"2026-10-01 08:30:15", "2026-10-01T08:30:15",
	} {
		if validLastMod(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestReadSitemapLastMod(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml": `<urlset>
<url><loc>{base}/a</loc><lastmod> 2026-10-01 </lastmod></url>
<url><loc>{base}/b</loc></url>
<url><loc>{base}/c</loc><lastmod>soon</lastmod></url>
<url><loc>{base}/d</loc><lastmod>2026-10-02T08:30:15Z</lastmod></url>
<url><loc>{base}/a</loc><lastmod>2020-01-01</lastmod></url>
</urlset>`,
	})
	f := Fetcher{Client: s.srv.Client(), UserAgent: "test"}
	urls, lm, err := f.ReadSitemapLastMod(context.Background(), s.srv.URL+"/sitemap.xml", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 4 {
		t.Fatalf("urls = %v", urls)
	}
	want := map[string]string{
		s.srv.URL + "/a": "2026-10-01",
		s.srv.URL + "/d": "2026-10-02T08:30:15Z",
	}
	if !reflect.DeepEqual(lm, want) {
		t.Fatalf("lastmod = %v, want %v (missing and invalid are left out, the first duplicate wins)", lm, want)
	}
}

// The <lastmod> of a sitemap inside an index belongs to that sitemap, not
// to a page.
func TestReadSitemapLastModIgnoresIndexEntries(t *testing.T) {
	s := newSite(t, map[string]string{
		"/index.xml": `<sitemapindex><sitemap><loc>{base}/p.xml</loc><lastmod>2026-10-05</lastmod></sitemap></sitemapindex>`,
		"/p.xml":     `<urlset><url><loc>{base}/a</loc></url></urlset>`,
	})
	f := Fetcher{Client: s.srv.Client(), UserAgent: "test"}
	urls, lm, err := f.ReadSitemapLastMod(context.Background(), s.srv.URL+"/index.xml", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || len(lm) != 0 {
		t.Fatalf("urls = %v, lastmod = %v", urls, lm)
	}
}
