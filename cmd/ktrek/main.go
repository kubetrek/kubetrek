package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "ktrek",
		Short:         "Learn Kubernetes in your own terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(c *cobra.Command, _ []string) {
			kubi(happy, bold("KubeTrek"), "Hi, I am Kubi. Start with "+bold("ktrek up")+" to create your cluster.")
			c.Help()
		},
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
		if colorful() {
			kubi(worried, bold("Something went wrong"), err.Error())
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}
