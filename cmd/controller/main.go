package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// Version information (injected at build time)
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "saltbox-docker-controller",
	Short: "Saltbox Docker Container Orchestrator",
	Long: `A dependency-aware Docker container orchestrator for Saltbox.
Manages container startup/shutdown order based on dependency labels.`,
	Version: fmt.Sprintf("%s (commit: %s, built: %s)", Version, GitCommit, BuildTime),
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
