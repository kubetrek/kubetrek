package main

import (
	"fmt"
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
  - {containerPort: 80, hostPort: 80}
  - {containerPort: 443, hostPort: 443}
  - {containerPort: 30080, hostPort: 30080}
`

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

	start := time.Now()
	err = p.Create(clusterName,
		cluster.CreateWithRawConfig(fmt.Appendf(nil, clusterConfig, policy)),
		cluster.CreateWithWaitForReady(3*time.Minute),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	)
	if err != nil {
		return err
	}
	fmt.Printf("\nCluster ready in %s.\n", time.Since(start).Round(time.Second))
	fmt.Println("kubectl now uses the context kind-ktrek. Your other contexts were not changed.")
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
