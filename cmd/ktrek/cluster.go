package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/exec"
	"sigs.k8s.io/kind/pkg/log"
)

const clusterName = "ktrek"

// Only the user's own requests are audited; system components would flood the log.
const auditPolicy = `apiVersion: audit.k8s.io/v1
kind: Policy
omitStages: ["RequestReceived"]
rules:
- level: Metadata
  users: ["kubernetes-admin"]
- level: None
`

const clusterConfig = `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: ClusterConfiguration
    apiServer:
      extraArgs:
      - name: audit-log-path
        value: /var/log/kubernetes/audit.log
      - name: audit-policy-file
        value: /etc/kubernetes/policies/audit-policy.yaml
      - name: audit-log-maxsize
        value: "10"
      - name: audit-log-maxbackup
        value: "1"
      extraVolumes:
      - name: audit-policies
        hostPath: /etc/kubernetes/policies
        mountPath: /etc/kubernetes/policies
        readOnly: true
        pathType: DirectoryOrCreate
      - name: audit-logs
        hostPath: /var/log/kubernetes
        mountPath: /var/log/kubernetes
        pathType: DirectoryOrCreate
  extraMounts:
  - hostPath: %s
    containerPath: /etc/kubernetes/policies/audit-policy.yaml
    readOnly: true
  extraPortMappings:
  - {containerPort: 80, hostPort: %d}
  - {containerPort: 443, hostPort: %d}
  - {containerPort: 30080, hostPort: %d}
`

// Ports exposed on localhost so `curl localhost` works the same on every OS.
// If the usual one is taken, the first free port from fallback on is used.
var ports = []struct{ container, fallback int }{
	{80, 8080},
	{443, 8443},
	{30080, 30081},
}

// Only sees listeners reachable on 127.0.0.1. Rootless Docker cannot bind ports below 1024; ktrek doctor should catch that.
func hostPort(preferred, fallback int) (int, error) {
	if free(preferred) {
		return preferred, nil
	}
	for p := fallback; p < fallback+20; p++ {
		if free(p) {
			return p, nil
		}
	}
	return 0, fmt.Errorf("port %d and %d-%d are all in use", preferred, fallback, fallback+19)
}

func free(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return true
	}
	c.Close()
	return false
}

func provider(logger log.Logger) *cluster.Provider {
	return cluster.NewProvider(cluster.ProviderWithDocker(), cluster.ProviderWithLogger(logger))
}

func exists(p *cluster.Provider) (bool, error) {
	names, err := p.List()
	return slices.Contains(names, clusterName), err
}

func up(idle time.Duration) error {
	p := provider(cmd.NewLogger())
	if ok, err := exists(p); err != nil || ok {
		if ok {
			return wake(idle)
		}
		return err
	}

	dir, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	policy := filepath.Join(dir, "ktrek", "audit-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(policy), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(policy, []byte(auditPolicy), 0o644); err != nil {
		return err
	}

	args := []any{policy}
	var portLines []string
	for _, pm := range ports {
		h, err := hostPort(pm.container, pm.fallback)
		if err != nil {
			return err
		}
		args = append(args, h)
		line := fmt.Sprintf("%s → %s", blue(fmt.Sprint(pm.container)), cyan(fmt.Sprintf("localhost:%d", h)))
		if h != pm.container {
			line = fmt.Sprintf("%s → %s %s", blue(fmt.Sprint(pm.container)), amber(fmt.Sprintf("localhost:%d", h)), gray(fmt.Sprintf("(%d was taken)", pm.container)))
		}
		portLines = append(portLines, line)
	}

	kubi(happy,
		bold("Let's build your cluster!"),
		"One node, with auditing on so I can see your kubectl commands.",
		gray("About 30 seconds. The first time takes longer: it downloads the node image."),
	)
	start := time.Now()
	err = p.Create(clusterName,
		cluster.CreateWithRawConfig(fmt.Appendf(nil, clusterConfig, args...)),
		cluster.CreateWithWaitForReady(3*time.Minute),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	)
	if err != nil {
		return err
	}
	if err := prepareNode(idle); err != nil {
		return err
	}

	fmt.Printf("\n %s Your cluster is ready %s\n\n", green("✓"), gray(fmt.Sprintf("(%s)", time.Since(start).Round(time.Second))))
	fmt.Printf("   %s  %s %s\n", gray("context"), cyan("kind-ktrek"), gray("· kubectl uses it now, your other contexts are untouched"))
	for i, l := range portLines {
		label := "       "
		if i == 0 {
			label = gray("ports  ")
		}
		fmt.Printf("   %s  %s\n", label, l)
	}
	fmt.Printf("   %s  %s\n", gray("idle   "), idleLine(idle))
	fmt.Printf("\n   Say hi to it:  %s\n\n", cyan("kubectl get nodes"))
	return nil
}

// wake handles a cluster that already exists: it may be running or turned off.
func wake(idle time.Duration) error {
	on, err := nodeRunning()
	if err != nil {
		return err
	}
	if on {
		if err := prepareNode(idle); err != nil {
			return err
		}
		fmt.Printf("\n %s Your cluster is already up and running.\n\n", green("●"))
		fmt.Printf("   %s  %s\n\n", gray("idle"), idleLine(idle))
		fmt.Printf("   Take a look:  %s\n", cyan("kubectl get nodes"))
		fmt.Printf("   Start over:   %s, then %s\n\n", cyan("ktrek down"), cyan("ktrek up"))
		return nil
	}

	fmt.Printf("\n %s Your cluster was off. Waking it up...\n", gray("●"))
	start := time.Now()
	if err := startNode(); err != nil {
		return err
	}
	if err := prepareNode(idle); err != nil {
		return err
	}
	fmt.Printf(" %s Your cluster is back %s. Everything you had is still there.\n\n", green("✓"), gray(fmt.Sprintf("(%s)", time.Since(start).Round(time.Second))))
	fmt.Printf("   %s  %s\n\n", gray("idle"), idleLine(idle))
	return nil
}

func down() error {
	if err := stopAgent(); err != nil {
		return err
	}
	p := provider(log.NoopLogger{})
	if ok, err := exists(p); err != nil || !ok {
		if err == nil {
			fmt.Printf("\n %s There is no cluster to delete. Create one with %s.\n\n", gray("●"), cyan("ktrek up"))
		}
		return err
	}
	fmt.Printf("\n %s Deleting your cluster...\n", gray("●"))
	if err := p.Delete(clusterName, ""); err != nil {
		return err
	}
	fmt.Printf(" %s Cluster deleted. Your other kubectl contexts are just as you left them.\n", green("✓"))
	fmt.Printf("\n   See you next time! %s\n\n", gray("Run ktrek up whenever you want to come back."))
	return nil
}

// hint turns the Docker errors people hit most into something they can act on.
func hint(err error) string {
	out := err.Error()
	if re := exec.RunErrorForError(err); re != nil {
		out += string(re.Output)
	}
	switch {
	case strings.Contains(out, "executable file not found"):
		return "I can't find the " + cyan("docker") + " command. Install Docker (Docker Desktop, OrbStack, Colima...) and try again."
	case strings.Contains(out, "permission denied") && strings.Contains(out, "docker"):
		return "Your user can't talk to Docker. Run " + cyan("sudo usermod -aG docker $USER") + ", then log out and back in."
	case strings.Contains(out, "Cannot connect to the Docker daemon"), strings.Contains(out, "failed to connect to the docker API"):
		return "Docker isn't running. Start it and run " + cyan("ktrek up") + " again."
	}
	return ""
}
