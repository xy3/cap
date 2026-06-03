package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Version is the build version, injected at build time via
//
//	-ldflags "-X capper/cmd.Version=v1.2.3"
//
// It stays "dev" for local builds.
var Version = "dev"

// updateRepo is the GitHub "owner/name" the self-updater pulls releases from.
const updateRepo = "xy3/cap"

// restartExitCode is returned after a successful in-place update so the
// run.bat relaunch loop starts the new binary. See scripts/run.bat.
const restartExitCode = 42

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the capper version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("capper %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
