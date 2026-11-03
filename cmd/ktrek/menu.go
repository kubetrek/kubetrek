package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// clusterState tells a missing cluster from a stopped one. Docker problems come back as errors.
func clusterState() (created, running bool, err error) {
	out, err := docker("", "inspect", "-f", "{{.State.Running}}", nodeName)
	if err != nil {
		if hint(err) != "" {
			return false, false, err
		}
		return false, false, nil
	}
	return true, out == "true", nil
}

// choose draws the options and lets the user move with the arrows (or j/k). It returns -1 on q, Esc or Ctrl+C.
func choose(items []string) int {
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return -1
	}
	defer term.Restore(fd, old)
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\r\x1b[2K\x1b[?25h")

	sel := 0
	draw := func() {
		for i, it := range items {
			if i == sel {
				fmt.Printf("\x1b[2K  %s %s\r\n", cyan("❯"), bold(it))
			} else {
				fmt.Printf("\x1b[2K    %s\r\n", it)
			}
		}
		fmt.Printf("\x1b[2K  %s", gray("↑/↓ move · Enter select · q quit"))
	}
	draw()
	buf := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return -1
		}
		switch string(buf[:n]) {
		case "\x1b[A", "k":
			sel = (sel + len(items) - 1) % len(items)
		case "\x1b[B", "j":
			sel = (sel + 1) % len(items)
		case "\r", "\n":
			return sel
		case "q", "\x1b", "\x03":
			return -1
		}
		fmt.Printf("\r\x1b[%dA", len(items))
		draw()
	}
}

func ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}

func statusLine(created, running, connected bool) string {
	cluster := gray("● ") + "Cluster " + gray("not created")
	switch {
	case running:
		cluster = green("● ") + "Cluster " + green("running")
	case created:
		cluster = gray("● ") + "Cluster " + gray("off")
	}
	web := gray("● ") + "Web " + gray("not connected")
	if connected {
		web = green("● ") + "Web " + green("connected")
	}
	return "  " + cluster + "    " + web + "\n"
}

type option struct {
	label string
	run   func() error
}

// greeting is what Kubi says above the menu, so the result of the last action shows in the state.
func greeting(created, running, connected bool) []string {
	switch {
	case !created:
		return []string{"You don't have a cluster yet. Let's create one!"}
	case !running:
		return []string{"Your cluster is off. Everything you had is still there."}
	case connected:
		return []string{"The web is watching your cluster live.", gray("Use kubectl in any terminal, I'll follow along.")}
	}
	return []string{"Your cluster is running.", "Connect it to the web to see it live."}
}

// menu is what plain `ktrek` opens in a terminal. It runs on the alternate screen and redraws
// from the top after every action; on Quit the terminal looks as it did before.
func menu() error {
	fmt.Print("\x1b[?1049h")
	defer fmt.Print("\x1b[?1049l")
	var last error
	for {
		created, running, err := clusterState()
		if err != nil {
			return err
		}
		_, connected := agentPid()

		fmt.Print("\x1b[H\x1b[2J")
		if last != nil {
			report(last)
		} else {
			kubi(happy, append([]string{bold("KubeTrek") + gray(" · learn Kubernetes in your own terminal")}, greeting(created, running, connected)...)...)
		}
		fmt.Println(statusLine(created, running, connected))

		var opts []option
		switch {
		case !created:
			opts = append(opts, option{"Create the cluster", func() error { return up(time.Hour) }})
		case !running:
			opts = append(opts, option{"Turn the cluster on", func() error { return up(time.Hour) }})
		}
		switch {
		case connected:
			opts = append(opts, option{"Disconnect from the web", disconnect})
		case running:
			opts = append(opts, option{"Connect to the web", func() error {
				code := ask("  Code shown on the web page: ")
				if code == "" {
					return nil
				}
				return connect(defaultServer, code, "default")
			}})
		}
		if created {
			opts = append(opts, option{"Delete the cluster", func() error {
				if ask("  This deletes the cluster and everything in it. Type "+bold("yes")+" to confirm: ") != "yes" {
					return nil
				}
				return down()
			}})
		}
		opts = append(opts, option{"Quit", nil})

		labels := make([]string, len(opts))
		for i, o := range opts {
			labels[i] = o.label
		}
		i := choose(labels)
		if i < 0 || opts[i].run == nil {
			return nil
		}
		last = opts[i].run()
	}
}
