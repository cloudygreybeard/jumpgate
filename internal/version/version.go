package version

import "fmt"

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

func String() string {
	return fmt.Sprintf("jumpgate %s (commit: %s, built: %s)", Version, Commit, Date)
}
