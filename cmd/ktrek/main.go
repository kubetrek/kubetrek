package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
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
		RunE: func(c *cobra.Command, _ []string) error {
			if interactive() {
				return menu()
			}
			kubi(happy,
				bold("KubeTrek")+gray(" · learn Kubernetes in your own terminal"),
				"Hi, I'm "+cyan("Kubi")+". I'll be watching your cluster with you.",
				"",
				"New here? Run "+cyan("ktrek up")+" and we'll build your first cluster.",
			)
			return c.Usage()
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
	var namespace, server, code string
	var detached bool
	agentCmd := &cobra.Command{
		Use:    "agent",
		Short:  "Print the messages the agent would send to the web, or send them with --connect",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(c.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if code != "" {
				return runConnected(ctx, server, code, namespace, detached)
			}
			return runAgent(ctx, namespace, os.Stdout, nil)
		},
	}
	agentCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to watch")
	agentCmd.Flags().StringVar(&code, "connect", "", "send the messages to the web page with this code")
	agentCmd.Flags().StringVar(&server, "server", defaultServer, "KubeTrek server")
	agentCmd.Flags().BoolVar(&detached, "detached", false, "report the connection on stdout and keep running in the background")
	connectCmd := &cobra.Command{
		Use:   "connect <code>",
		Short: "Show your cluster live in the browser tab with this code",
		Args:  cobra.ExactArgs(1),
		RunE:  func(_ *cobra.Command, args []string) error { return connect(server, args[0], namespace) },
	}
	connectCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "namespace to watch")
	connectCmd.Flags().StringVar(&server, "server", defaultServer, "KubeTrek server")
	root.AddCommand(
		upCmd,
		connectCmd,
		&cobra.Command{
			Use:   "disconnect",
			Short: "Stop showing your cluster on the web",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return disconnect() },
		},
		agentCmd,
		&cobra.Command{
			Use:   "down",
			Short: "Delete your local cluster",
			Args:  cobra.NoArgs,
			RunE:  func(*cobra.Command, []string) error { return down() },
		},
	)
	if err := root.Execute(); err != nil {
		report(err)
		os.Exit(1)
	}
}

func report(err error) {
	msg := []string{err.Error()}
	if h := hint(err); h != "" {
		msg = []string{h, "", gray(err.Error())}
	}
	if colorful() {
		kubi(worried, append([]string{bold("Uh-oh, something is off.")}, msg...)...)
	} else {
		fmt.Fprintln(os.Stderr, "Error:", strings.Join(msg, "\n"))
	}
}
