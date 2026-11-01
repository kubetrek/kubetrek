package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:          "ktrek",
		Short:        "Learn Kubernetes in your own terminal",
		SilenceUsage: true,
	}
	root.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Create the local ktrek cluster",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return up() },
		},
		&cobra.Command{
			Use:   "down",
			Short: "Delete the local ktrek cluster",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return down() },
		},
	)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
