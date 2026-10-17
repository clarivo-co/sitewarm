package source

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func urlset(locs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, l := range locs {
		fmt.Fprintf(&b, "<url><loc>%s</loc><lastmod>2026-10-01</lastmod></url>", l)
	}
	b.WriteString("</urlset>")
	return b.String()
}

func index(locs ...string) string {
	var b strings.Builder
	b.WriteString(`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, l := range locs {
		fmt.Fprintf(&b, "<sitemap><loc>%s</loc></sitemap>", l)
	}
	b.WriteString("</sitemapindex>")
	return b.String()
}

// site serves path -> body. {base} in a body becomes the server URL, and a
// path ending in .gz is served gzip-compressed, as a static .gz file.
type site struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
}

func newSite(t *testing.T, files map[string]string) *site {
	s := &site{hits: map[string]int{}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data := []byte(strings.ReplaceAll(body, "{base}", s.srv.URL))
		if strings.HasSuffix(r.URL.Path, ".gz") {
			w.Header().Set("Content-Type", "application/gzip")
			data = gzipBytes(t, string(data))
		}
		w.Write(data)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *site) count(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

func fetcher(s *site) Fetcher {
	return Fetcher{Client: s.srv.Client(), UserAgent: "test sitewarm/dev"}
}

// Regression for the Python CLI: nested sitemap indexes and .gz sitemaps.
func TestReadSitemapNestedIndexAndGzip(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml":       index("{base}/sitemap-posts.xml", "{base}/nested-index.xml"),
		"/sitemap-posts.xml": urlset("{base}/a", "{base}/b#frag", "{base}/a"),
		"/nested-index.xml":  index("{base}/pages.xml.gz", "{base}/sitemap.xml"),
		"/pages.xml.gz":      urlset("{base}/c", "{base}/b"),
	})
	got, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/sitemap.xml", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{s.srv.URL + "/a", s.srv.URL + "/b", s.srv.URL + "/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if n := s.count("/sitemap.xml"); n != 1 {
		t.Fatalf("the index was read %d times; a loop must be read once", n)
	}
}

func TestReadSitemapGzipRoot(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml.gz": index("{base}/s.xml"),
		"/s.xml":          urlset("{base}/x"),
	})
	got, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/sitemap.xml.gz", 0, nil)
	if err != nil || len(got) != 1 || got[0] != s.srv.URL+"/x" {
		t.Fatalf("got %v, %v", got, err)
	}
}

// A server that compresses on the fly (Content-Encoding: gzip) is decoded
// by the HTTP client, not mistaken for a .gz file.
func TestReadSitemapContentEncodingGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Write(gzipBytes(t, urlset("https://example.com/y")))
	}))
	defer srv.Close()
	got, err := Fetcher{Client: srv.Client()}.ReadSitemap(context.Background(), srv.URL, 0, nil)
	if err != nil || len(got) != 1 || got[0] != "https://example.com/y" {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestReadSitemapLimitStopsEarly(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml": index("{base}/one.xml", "{base}/two.xml"),
		"/one.xml":     urlset("{base}/1", "{base}/2"),
		"/two.xml":     urlset("{base}/3"),
	})
	got, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/sitemap.xml", 2, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	if s.count("/two.xml") != 0 {
		t.Fatal("read a sitemap after the limit was reached")
	}
}

// Only accepted URLs count toward the limit; rejected ones are still
// returned, and the walk stops at the limit without reading later sitemaps.
func TestReadSitemapAcceptCountsTowardLimit(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml": index("{base}/one.xml", "{base}/two.xml", "{base}/three.xml"),
		"/one.xml":     urlset("{base}/1", "{base}/blocked/2"),
		"/two.xml":     urlset("{base}/blocked/3", "{base}/4", "{base}/blocked/5"),
		"/three.xml":   urlset("{base}/6"),
	})
	var asked []string
	accept := func(u string) bool {
		asked = append(asked, u)
		return !strings.Contains(u, "/blocked/")
	}
	got, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/sitemap.xml", 2, accept)
	if err != nil {
		t.Fatal(err)
	}
	b := s.srv.URL
	want := []string{b + "/1", b + "/blocked/2", b + "/blocked/3", b + "/4"}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(asked, want) {
		t.Fatalf("got %v, asked %v; want %v", got, asked, want)
	}
	if s.count("/three.xml") != 0 {
		t.Fatal("read a sitemap after the limit was reached")
	}
}

// On an error the URLs read so far come back with it, so an interrupted
// run can report them.
func TestReadSitemapErrorKeepsURLsRead(t *testing.T) {
	s := newSite(t, map[string]string{
		"/sitemap.xml": index("{base}/one.xml", "{base}/missing.xml"),
		"/one.xml":     urlset("{base}/1"),
	})
	got, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/sitemap.xml", 0, nil)
	if err == nil || len(got) != 1 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestReadSitemapErrors(t *testing.T) {
	s := newSite(t, map[string]string{
		"/html":      "<html><body>hi</body></html>",
		"/broken":    "<urlset><url><loc>",
		"/relative":  urlset("/a"),
		"/empty":     urlset(),
		"/missing-c": index("{base}/nope.xml"),
	})
	cases := map[string]string{
		"/html":      "not a sitemap",
		"/broken":    "invalid XML",
		"/relative":  "not an absolute",
		"/missing-c": "HTTP 404",
		"/nope":      "HTTP 404",
	}
	for path, want := range cases {
		_, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+path, 0, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v, want %q", path, err, want)
		}
	}
	if _, err := fetcher(s).ReadSitemap(context.Background(), s.srv.URL+"/empty", 0, nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: got %v", err)
	}
	if _, err := fetcher(s).ReadSitemap(context.Background(), "sitemap.xml", 0, nil); err == nil {
		t.Error("a relative sitemap URL must fail")
	}
}

func TestReadSitemapSendsUserAgent(t *testing.T) {
	var mu sync.Mutex
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ua = r.Header.Get("User-Agent")
		mu.Unlock()
		w.Write([]byte(urlset("https://example.com/")))
	}))
	defer srv.Close()
	f := Fetcher{Client: srv.Client(), UserAgent: "Mozilla/5.0 sitewarm/dev"}
	if _, err := f.ReadSitemap(context.Background(), srv.URL, 0, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if ua != "Mozilla/5.0 sitewarm/dev" {
		t.Fatalf("User-Agent %q", ua)
	}
}
