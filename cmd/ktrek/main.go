package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const usageTemplate = `
{{bold "Usage"}}
  {{if .HasAvailableSubCommands}}{{.CommandPath}} {{cyan "<command>"}}{{else}}{{.UseLine}}{{end}}
{{if .HasAvailableSubCommands}}
{{bold "Commands"}}{{range .Commands}}{{if .IsAvailableCommand}}
  {{cyan (rpad .Name 10)}}  {{.Short}}{{end}}{{end}}
{{end}}{{if .HasAvailableLocalFlags}}
{{bold "Flags"}}
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}
{{end}}{{if .HasAvailableSubCommands}}
{{gray (printf "Run %s <command> --help to learn more about a command." .CommandPath)}}
{{end}}
`

func main() {
	cobra.AddTemplateFunc("bold", bold)
	cobra.AddTemplateFunc("cyan", cyan)
	cobra.AddTemplateFunc("gray", gray)

	root := &cobra.Command{
		Use:           "ktrek",
		Short:         "Learn Kubernetes in your own terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(c *cobra.Command, _ []string) {
			kubi(happy,
				bold("KubeTrek")+gray(" · learn Kubernetes in your own terminal"),
				"Hi, I'm "+cyan("Kubi")+". I'll be watching your cluster with you.",
				"",
				"New here? Run "+cyan("ktrek up")+" and we'll build your first cluster.",
			)
			c.Usage()
		},
	}
	root.CompletionOptions.HiddenDefaultCmd = true
	root.SetUsageTemplate(usageTemplate)
	root.SetHelpTemplate("{{with .Short}}{{bold .}}\n{{end}}{{.UsageString}}")
	var idle time.Duration
	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Create your local cluster, or wake it up if it is off",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return up(idle) },
	}
	upCmd.Flags().DurationVar(&idle, "idle", time.Hour, "turn the cluster off after this long without kubectl commands, 0 keeps it on")
	var namespace string
	agentCmd := &cobra.Command{
		Use:    "agent",
		Short:  "Print the messages the agent would send to the web",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt)
			defer stop()
			return runAgent(ctx, namespace, os.Stdout)
		},
	}
	agentCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to watch")
	root.AddCommand(
		upCmd,
		agentCmd,
		&cobra.Command{
			Use:   "down",
			Short: "Delete your local cluster",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return down() },
		},
	)
	if err := root.Execute(); err != nil {
		msg := []string{err.Error()}
		if h := hint(err); h != "" {
			msg = []string{h, "", gray(err.Error())}
		}
		if colorful() {
			kubi(worried, append([]string{bold("Uh-oh, something is off.")}, msg...)...)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", strings.Join(msg, "\n"))
		}
		os.Exit(1)
	}
}
