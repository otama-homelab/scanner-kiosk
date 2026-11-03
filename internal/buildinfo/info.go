// Package buildinfo reports non-secret executable identity without loading app
// configuration, opening stores or initializing providers/devices.
package buildinfo

import "runtime"

// Release builders replace these using Go -ldflags -X. Defaults explicitly do
// not identify a published release or a source commit.
var Version = "0.0.0-dev"
var Revision = "unreleased"

type Info struct {
	Version      string `json:"version"`
	Revision     string `json:"revision"`
	GoVersion    string `json:"go_version"`
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
}

func Current() Info {
	return Info{Version: Version, Revision: Revision, GoVersion: runtime.Version(), OS: runtime.GOOS, Architecture: runtime.GOARCH}
}
