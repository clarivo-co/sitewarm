package sitewarm_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/clarivo-co/sitewarm/pkg/sitewarm"
)

// sitemapSite serves a sitemap index -> nested index -> gzip sitemap, a
// robots.txt answered by robots, and HIT pages. It counts page requests.
type sitemapSite struct {
	*httptest.Server
	mu    sync.Mutex
	pages map[string]int
}

func newSitemapSite(t *testing.T, robots func(w http.ResponseWriter)) *sitemapSite {
	s := &sitemapSite{pages: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := s.URL
		switch r.URL.Path {
		case "/robots.txt":
			robots(w)
		case "/sitemap.xml":
			fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/nested.xml</loc></sitemap></sitemapindex>`, base)
		case "/nested.xml":
			fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/pages.xml.gz</loc></sitemap></sitemapindex>`, base)
		case "/pages.xml.gz":
			var b bytes.Buffer
			gz := gzip.NewWriter(&b)
			fmt.Fprintf(gz, `<urlset><url><loc>%[1]s/a</loc></url><url><loc>%[1]s/private/b</loc></url><url><loc>%[1]s/c</loc></url></urlset>`, base)
			gz.Close()
			w.Write(b.Bytes())
		default:
			s.mu.Lock()
			s.pages[r.URL.Path]++
			s.mu.Unlock()
			w.Header().Set("Cf-Cache-Status", "HIT")
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html><head></head></html>"))
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *sitemapSite) requested(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pages[path]
}

func sitemapOptions(s *sitemapSite) sitewarm.Options {
	o := fastOptions("")
	o.TopFrom = ""
	o.FromSitemap = s.URL + "/sitemap.xml"
	return o
}

func robotsBody(body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.Write([]byte(body)) }
}

func robotsStatus(code int) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) { w.WriteHeader(code) }
}

func states(res *sitewarm.Result) string {
	var out []string
	for _, e := range res.Results {
		out = append(out, strings.TrimPrefix(e.URL, "http://")+"="+string(e.State))
	}
	return strings.Join(out, " ")
}

func TestScanFromSitemapRespectsRobots(t *testing.T) {
	s := newSitemapSite(t, robotsBody("User-agent: *\nDisallow: /\n\nUser-agent: sitewarm\nDisallow: /private\n"))
	res, err := sitewarm.Scan(context.Background(), sitemapOptions(s))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Total != 6 || res.Summary.ByState[sitewarm.Hit] != 4 || res.Summary.ByState[sitewarm.SkippedRobots] != 2 {
		t.Fatalf("summary %+v: %s", res.Summary, states(res))
	}
	// List order is kept: /a, /private/b, /c, two profiles each.
	if res.Results[2].URL != s.URL+"/private/b" || res.Results[2].State != sitewarm.SkippedRobots || res.Results[2].Attempts != 0 {
		t.Fatalf("results: %s", states(res))
	}
	if !res.Summary.Passed {
		t.Fatalf("skipped-robots must not fail by default: %s", res.Summary.Verdict)
	}
	if n := s.requested("/private/b"); n != 0 {
		t.Fatalf("a disallowed URL was requested %d times", n)
	}
	if res.Settings.Source != s.URL+"/sitemap.xml" {
		t.Fatalf("source %q", res.Settings.Source)
	}
}

func TestScanFromSitemapRobots404AllowsAll(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusNotFound))
	res, err := sitewarm.Scan(context.Background(), sitemapOptions(s))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.ByState[sitewarm.Hit] != 6 || !res.Summary.Passed {
		t.Fatalf("summary %+v", res.Summary)
	}
}

// robots.txt with 5xx under --from-sitemap makes the run fail: every URL is
// skipped-robots, so nothing was verified.
func TestScanFromSitemapRobots5xxFailsTheRun(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusInternalServerError))
	res, err := sitewarm.Scan(context.Background(), sitemapOptions(s))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.ByState[sitewarm.SkippedRobots] != 6 || res.Summary.Passed {
		t.Fatalf("summary %+v", res.Summary)
	}
	if !strings.Contains(res.Summary.Verdict, "no URL was verified") {
		t.Fatalf("verdict %q", res.Summary.Verdict)
	}
	found := false
	for _, w := range res.Warnings {
		found = found || strings.Contains(w, "robots.txt returned HTTP 500")
	}
	if !found {
		t.Fatalf("warnings %v", res.Warnings)
	}
	if n := s.requested("/a"); n != 0 {
		t.Fatalf("a page was requested %d times", n)
	}
}

func TestScanFromSitemapIgnoreRobots(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusInternalServerError))
	o := sitemapOptions(s)
	o.IgnoreRobots = true
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.ByState[sitewarm.Hit] != 6 || !res.Summary.Passed || !res.Settings.IgnoreRobots {
		t.Fatalf("summary %+v settings %+v", res.Summary, res.Settings)
	}
}

// --top counts allowed URLs only. The URLs skipped before the Nth allowed
// one are reported; the rest of the sitemap is not checked.
func TestScanFromSitemapTopCountsAllowedURLs(t *testing.T) {
	s := newSitemapSite(t, robotsBody("User-agent: *\nDisallow: /private\n"))
	o := sitemapOptions(s)
	o.Top = 2
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	// /a and /c are the 2 allowed URLs; /private/b, between them, is skipped.
	if res.Summary.Total != 6 || res.Summary.ByState[sitewarm.Hit] != 4 || res.Summary.ByState[sitewarm.SkippedRobots] != 2 ||
		res.Results[4].URL != s.URL+"/c" {
		t.Fatalf("results: %s", states(res))
	}

	o.Top = 1
	res, err = sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Total != 2 || res.Summary.ByState[sitewarm.SkippedRobots] != 0 {
		t.Fatalf("a skipped URL after the Nth allowed one must not be reported: %s", states(res))
	}
}

// With robots.txt checks on, the sitemap walk still stops at the Nth
// allowed URL: later sitemaps of the index are not fetched.
func TestScanFromSitemapRobotsStopsReadingAtTop(t *testing.T) {
	var mu sync.Mutex
	fetched := map[string]int{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetched[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			w.Write([]byte("User-agent: *\nDisallow: /private\n"))
		case "/sitemap.xml":
			fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%[1]s/one.xml</loc></sitemap><sitemap><loc>%[1]s/two.xml</loc></sitemap></sitemapindex>`, srv.URL)
		case "/one.xml":
			fmt.Fprintf(w, `<urlset><url><loc>%[1]s/private/x</loc></url><url><loc>%[1]s/a</loc></url></urlset>`, srv.URL)
		case "/two.xml":
			fmt.Fprintf(w, `<urlset><url><loc>%s/b</loc></url></urlset>`, srv.URL)
		default:
			w.Header().Set("Cf-Cache-Status", "HIT")
			w.Write([]byte("<html><head></head></html>"))
		}
	}))
	defer srv.Close()
	o := fastOptions("")
	o.TopFrom, o.FromSitemap = "", srv.URL+"/sitemap.xml"
	o.Top = 1
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Total != 4 || res.Summary.ByState[sitewarm.SkippedRobots] != 2 || res.Summary.ByState[sitewarm.Hit] != 2 {
		t.Fatalf("results: %s", states(res))
	}
	mu.Lock()
	defer mu.Unlock()
	if fetched["/two.xml"] != 0 {
		t.Fatal("read a sitemap after --top allowed URLs were found")
	}
}

// Sitemap and robots.txt fetches share the --rps budget with warming.
func TestScanFromSitemapRPSCoversSourceFetches(t *testing.T) {
	var mu sync.Mutex
	var times []time.Time
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
		case "/sitemap.xml":
			fmt.Fprintf(w, `<sitemapindex><sitemap><loc>%s/pages.xml</loc></sitemap></sitemapindex>`, srv.URL)
		case "/pages.xml":
			fmt.Fprintf(w, `<urlset><url><loc>%[1]s/a</loc></url><url><loc>%[1]s/b</loc></url></urlset>`, srv.URL)
		default:
			w.Header().Set("Cf-Cache-Status", "HIT")
			w.Write([]byte("<html><head></head></html>"))
		}
	}))
	defer srv.Close()
	o := fastOptions("")
	o.TopFrom, o.FromSitemap = "", srv.URL+"/sitemap.xml"
	o.RPS = 20 // 50ms apart
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil || !res.Summary.Passed {
		t.Fatalf("%v %+v", err, res.Summary)
	}
	mu.Lock()
	defer mu.Unlock()
	// 2 sitemaps + 1 robots.txt + 2 URLs x 2 profiles = 7 requests.
	if len(times) != 7 {
		t.Fatalf("%d requests", len(times))
	}
	if span := times[len(times)-1].Sub(times[0]); span < 270*time.Millisecond {
		t.Fatalf("7 requests at 20 rps spanned %v, want >= 300ms", span)
	}
}

func TestScanFromSitemapTopKeepsOrder(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusNotFound))
	o := sitemapOptions(s)
	o.Top = 2
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.Total != 4 || res.Results[0].URL != s.URL+"/a" || res.Results[2].URL != s.URL+"/private/b" {
		t.Fatalf("results: %s", states(res))
	}
}

// --top-from lists are chosen by their operator: robots.txt is not read.
func TestScanTopFromIgnoresRobots(t *testing.T) {
	s := newSitemapSite(t, robotsBody("User-agent: *\nDisallow: /\n"))
	res, err := sitewarm.Scan(context.Background(), fastOptions(writeCSV(t, s.URL+"/a")))
	if err != nil {
		t.Fatal(err)
	}
	if res.Summary.ByState[sitewarm.Hit] != 2 {
		t.Fatalf("summary %+v", res.Summary)
	}
}

func TestScanSourcesAreExclusive(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusNotFound))
	both := sitemapOptions(s)
	both.TopFrom = writeCSV(t, s.URL+"/a")
	none := sitemapOptions(s)
	none.FromSitemap = ""
	for name, o := range map[string]sitewarm.Options{"both": both, "none": none} {
		_, err := sitewarm.Scan(context.Background(), o)
		if !errors.Is(err, sitewarm.ErrConfig) || !errors.Is(err, sitewarm.ErrSource) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

// blockingSite blocks on path until the request is canceled; everything
// else is a HIT page or a one-URL sitemap.
func blockingSite(t *testing.T, path string) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case path:
			<-r.Context().Done()
		case "/sitemap.xml":
			fmt.Fprintf(w, `<urlset><url><loc>%s/a</loc></url></urlset>`, srv.URL)
		default:
			w.Header().Set("Cf-Cache-Status", "HIT")
			w.Write([]byte("<html><head></head></html>"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// An interrupt while the sitemap is read is a stopped run with a report
// that fails, not a configuration error.
func TestScanCanceledWhileReadingSitemap(t *testing.T) {
	srv := blockingSite(t, "/sitemap.xml")
	o := fastOptions("")
	o.TopFrom, o.FromSitemap = "", srv.URL+"/sitemap.xml"
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	res, err := sitewarm.Scan(ctx, o)
	if err != nil {
		t.Fatalf("got error %v, want a stopped report", err)
	}
	if res.Stopped != "canceled" || res.Summary.Total != 0 || res.Summary.Passed {
		t.Fatalf("stopped %q summary %+v", res.Stopped, res.Summary)
	}
}

// --run-timeout also bounds the robots.txt checks, and URLs left unchecked
// are incomplete, not skipped-robots.
func TestScanRunTimeoutWhileReadingRobots(t *testing.T) {
	srv := blockingSite(t, "/robots.txt")
	o := fastOptions("")
	o.TopFrom, o.FromSitemap = "", srv.URL+"/sitemap.xml"
	o.RunTimeout = 200 * time.Millisecond
	res, err := sitewarm.Scan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stopped != "run-timeout" || res.Summary.ByState[sitewarm.Incomplete] != 2 || res.Summary.ByState[sitewarm.SkippedRobots] != 0 {
		t.Fatalf("stopped %q: %s", res.Stopped, states(res))
	}
	if len(res.Warnings) != 2 {
		t.Fatalf("a canceled robots.txt fetch must not warn: %v", res.Warnings)
	}
}

func TestScanFromSitemapUnreachableIsConfigError(t *testing.T) {
	s := newSitemapSite(t, robotsStatus(http.StatusNotFound))
	o := sitemapOptions(s)
	o.FromSitemap = s.URL + "/missing.xml"
	if _, err := sitewarm.Scan(context.Background(), o); !errors.Is(err, sitewarm.ErrConfig) {
		t.Fatalf("got %v", err)
	}
}
