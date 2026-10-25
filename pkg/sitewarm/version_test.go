package sitewarm

import (
	"runtime/debug"
	"testing"
)

func TestModuleVersion(t *testing.T) {
	tests := []struct {
		name string
		bi   debug.BuildInfo
		want string
	}{
		{"go install", debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "v1.0.1"}}, "1.0.1"},
		{"local build", debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}}, ""},
		{"empty", debug.BuildInfo{Main: debug.Module{Path: modulePath}}, ""},
		{"other program", debug.BuildInfo{Main: debug.Module{Path: "example.com/app", Version: "v9.9.9"}}, ""},
		{"imported", debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app", Version: "v9.9.9"},
			Deps: []*debug.Module{{Path: modulePath, Version: "v1.0.1"}},
		}, "1.0.1"},
		{"replaced", debug.BuildInfo{
			Main: debug.Module{Path: "example.com/app"},
			Deps: []*debug.Module{{Path: modulePath, Version: "v1.0.1", Replace: &debug.Module{Path: "../sitewarm", Version: "(devel)"}}},
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moduleVersion(&tt.bi); got != tt.want {
				t.Fatalf("moduleVersion = %q, want %q", got, tt.want)
			}
		})
	}
}
