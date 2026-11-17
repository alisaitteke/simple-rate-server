// Package version holds build information injected at link time.
package version

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// UserAgent is sent to upstream providers.
func UserAgent() string { return "rate-server/" + Version }
