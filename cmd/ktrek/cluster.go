package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
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

func provider() *cluster.Provider {
	return cluster.NewProvider(cluster.ProviderWithDocker(), cluster.ProviderWithLogger(cmd.NewLogger()))
}

func exists(p *cluster.Provider) (bool, error) {
	names, err := p.List()
	return slices.Contains(names, clusterName), err
}

func up() error {
	p := provider()
	if ok, err := exists(p); err != nil || ok {
		if ok {
			fmt.Println("The ktrek cluster is already running.")
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
	var moved []string
	for _, pm := range ports {
		h, err := hostPort(pm.container, pm.fallback)
		if err != nil {
			return err
		}
		args = append(args, h)
		if h != pm.container {
			moved = append(moved, fmt.Sprintf("cluster port %d is on localhost:%d (%d was taken)", pm.container, h, pm.container))
		}
	}

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
	fmt.Printf("\nCluster ready in %s.\n", time.Since(start).Round(time.Second))
	fmt.Println("kubectl now uses the context kind-ktrek. Your other contexts were not changed.")
	for _, m := range moved {
		fmt.Println(m)
	}
	return nil
}

func down() error {
	p := provider()
	if ok, err := exists(p); err != nil || !ok {
		if err == nil {
			fmt.Println("There is no ktrek cluster to delete.")
		}
		return err
	}
	if err := p.Delete(clusterName, ""); err != nil {
		return err
	}
	fmt.Println("Cluster deleted. The kind-ktrek context was removed from kubectl.")
	return nil
}
