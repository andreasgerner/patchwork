package version

import "fmt"

// Version and GitCommit are set at build time via -ldflags.
var (
	Version   = "dev"
	GitCommit = "unknown"
)

// Banner returns a one-line version string suitable for startup logging.
func Banner() string {
	return fmt.Sprintf("patchwork %s (commit: %s)", Version, GitCommit)
}
