package sitewarm

import (
	"runtime/debug"
	"strings"
)

const modulePath = "github.com/sitewarm/sitewarm"

// A build without -ldflags (go install ...@vX) leaves Version at "dev"; take
// the module version from the build info instead. Release builds set it with
// -ldflags and are not touched.
func init() {
	if Version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := moduleVersion(bi); v != "" {
			Version = v
		}
	}
}

// moduleVersion returns the version of this module in bi without the leading
// "v", or "" when it is unknown. When the package is imported by another
// program the version of that program is not ours, so only this module's own
// path is matched.
func moduleVersion(bi *debug.BuildInfo) string {
	v := ""
	if bi.Main.Path == modulePath {
		v = bi.Main.Version
	}
	for _, d := range bi.Deps {
		if d.Path == modulePath {
			if d.Replace != nil {
				d = d.Replace
			}
			v = d.Version
		}
	}
	if v == "" || v == "(devel)" {
		return ""
	}
	return strings.TrimPrefix(v, "v")
}
