package main

import (
	"fmt"
	"strings"
	"time"

	"sigs.k8s.io/kind/pkg/exec"
)

const nodeName = clusterName + "-control-plane"

// Runs inside the node. The audit log only records the user's own requests, so its mtime is the last time they used kubectl.
const idleScript = `#!/bin/sh
[ "$1" -gt 0 ] || exit 0
start=$(date +%s)
while sleep 30; do
	last=$(stat -c %Y /var/log/kubernetes/audit.log 2>/dev/null || echo 0)
	[ "$last" -gt "$start" ] || last=$start
	if [ $(($(date +%s) - last)) -ge "$1" ]; then
		systemctl poweroff
	fi
done
`

const idleUnit = `[Unit]
Description=Power off the ktrek node after a while without kubectl requests

[Service]
ExecStart=/usr/local/bin/ktrek-idle %d

[Install]
WantedBy=multi-user.target
`

func docker(stdin string, args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	if stdin != "" {
		cmd.SetStdin(strings.NewReader(stdin))
	}
	lines, err := exec.OutputLines(cmd)
	return strings.Join(lines, "\n"), err
}

func nodeRunning() (bool, error) {
	out, err := docker("", "inspect", "-f", "{{.State.Running}}", nodeName)
	return out == "true", err
}

// prepareNode keeps the node off after the computer restarts and installs the idle shutdown.
// It runs on every up, so a new --idle value replaces the old one.
func prepareNode(idle time.Duration) error {
	if _, err := docker("", "update", "--restart=no", nodeName); err != nil {
		return err
	}
	if _, err := docker(idleScript, "exec", "-i", nodeName, "sh", "-c", "cat > /usr/local/bin/ktrek-idle && chmod +x /usr/local/bin/ktrek-idle"); err != nil {
		return err
	}
	unit := fmt.Sprintf(idleUnit, int(idle.Seconds()))
	if _, err := docker(unit, "exec", "-i", nodeName, "sh", "-c", "cat > /etc/systemd/system/ktrek-idle.service"); err != nil {
		return err
	}
	// Some system pods ignore SIGTERM, and systemd would wait 90 s for each of them before powering off.
	_, err := docker("", "exec", nodeName, "sh", "-c", `mkdir -p /etc/systemd/system.conf.d &&
		printf '[Manager]\nDefaultTimeoutStopSec=10s\n' > /etc/systemd/system.conf.d/ktrek.conf &&
		systemctl daemon-reload && systemctl enable ktrek-idle && systemctl restart ktrek-idle`)
	return err
}

func startNode() error {
	if _, err := docker("", "start", nodeName); err != nil {
		return err
	}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		out, _ := docker("", "exec", nodeName, "kubectl", "--kubeconfig=/etc/kubernetes/admin.conf", "get", "--raw", "/readyz")
		if out == "ok" {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("the cluster started but its API server did not answer within 3 minutes")
}

// human prints 1h0m0s as 1h and 30m0s as 30m.
func human(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

func idleLine(idle time.Duration) string {
	if idle <= 0 {
		return gray("stays on until you run ktrek down or turn off your computer")
	}
	return "turns off after " + cyan(human(idle)) + gray(" without kubectl commands, and when you turn off your computer")
}
