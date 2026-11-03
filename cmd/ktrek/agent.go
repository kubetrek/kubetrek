package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	rbacv1ac "k8s.io/client-go/applyconfigurations/rbac/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubetrek/kubetrek/internal/protocol"
)

// agent watches one namespace and writes a protocol.Message per change.
type agent struct {
	mu    sync.Mutex
	ready bool
	out   *json.Encoder
	kinds map[string]cache.SharedIndexInformer
}

func project(kind string, obj any) protocol.Object {
	m, _ := meta.Accessor(obj)
	o := protocol.Object{Kind: kind, Namespace: m.GetNamespace(), Name: m.GetName(), Labels: m.GetLabels()}
	for _, ref := range m.GetOwnerReferences() {
		o.Owners = append(o.Owners, ref.Kind+"/"+ref.Name)
	}
	if svc, ok := obj.(*corev1.Service); ok {
		o.Selector = svc.Spec.Selector
	}
	if f := reflect.ValueOf(obj).Elem().FieldByName("Status"); f.IsValid() {
		o.Status, _ = json.Marshal(f.Interface())
	}
	return o
}

func clusterEvent(e *corev1.Event) protocol.ClusterEvent {
	t := e.LastTimestamp.Time
	if t.IsZero() {
		t = e.EventTime.Time
	}
	if t.IsZero() {
		t = e.CreationTimestamp.Time
	}
	return protocol.ClusterEvent{
		Object:  e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
		Type:    e.Type,
		Reason:  e.Reason,
		Message: e.Message,
		Time:    t,
	}
}

func (a *agent) send(m protocol.Message) {
	a.out.Encode(m)
}

// emit runs for every informer callback. Until the snapshot is sent, changes are skipped:
// the caches already hold them and the snapshot includes them.
func (a *agent) emit(t protocol.Type, kind string, old, obj any) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.ready {
		return
	}
	// When a watch expires the informer relists and calls Update for every object it already had,
	// so updates that change nothing are dropped, Events included.
	if e, ok := obj.(*corev1.Event); ok {
		if t == protocol.Deleted {
			return
		}
		ce := clusterEvent(e)
		if t == protocol.Modified && clusterEvent(old.(*corev1.Event)) == ce {
			return
		}
		a.send(protocol.Message{Type: protocol.Event, Event: &ce})
		return
	}
	o := project(kind, obj)
	if t == protocol.Modified {
		before, _ := json.Marshal(project(kind, old))
		after, _ := json.Marshal(o)
		if bytes.Equal(before, after) {
			return
		}
	}
	a.send(protocol.Message{Type: t, Object: &o})
}

func (a *agent) snapshot() {
	a.mu.Lock()
	defer a.mu.Unlock()
	objects := []protocol.Object{}
	for kind, inf := range a.kinds {
		if kind == "Event" {
			continue
		}
		for _, obj := range inf.GetStore().List() {
			objects = append(objects, project(kind, obj))
		}
	}
	a.send(protocol.Message{Type: protocol.Snapshot, Objects: objects})
	a.ready = true
}

const agentAccount = "ktrek-agent"

// agentConfig gives the agent its own read-only identity. The audit policy only records
// kubernetes-admin, so the agent's watches no longer count as kubectl activity and the idle
// shutdown still works while it runs. The admin kubeconfig is used once, to set this up.
func agentConfig(ctx context.Context) (*rest.Config, error) {
	admin, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: "kind-" + clusterName},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("can't reach the ktrek cluster, is it up? %w", err)
	}
	admin.UserAgent = "ktrek-agent/dev"
	cs, err := kubernetes.NewForConfig(admin)
	if err != nil {
		return nil, err
	}

	apply := metav1.ApplyOptions{FieldManager: "ktrek", Force: true}
	read := []string{"get", "list", "watch"}
	if _, err := cs.CoreV1().ServiceAccounts("kube-system").Apply(ctx,
		corev1ac.ServiceAccount(agentAccount, "kube-system"), apply); err != nil {
		return nil, err
	}
	if _, err := cs.RbacV1().ClusterRoles().Apply(ctx, rbacv1ac.ClusterRole(agentAccount).WithRules(
		rbacv1ac.PolicyRule().WithAPIGroups("").WithResources("pods", "services", "events").WithVerbs(read...),
		rbacv1ac.PolicyRule().WithAPIGroups("apps").WithResources("deployments", "replicasets").WithVerbs(read...),
	), apply); err != nil {
		return nil, err
	}
	if _, err := cs.RbacV1().ClusterRoleBindings().Apply(ctx, rbacv1ac.ClusterRoleBinding(agentAccount).
		WithRoleRef(rbacv1ac.RoleRef().WithAPIGroup("rbac.authorization.k8s.io").WithKind("ClusterRole").WithName(agentAccount)).
		WithSubjects(rbacv1ac.Subject().WithKind("ServiceAccount").WithName(agentAccount).WithNamespace("kube-system")),
		apply); err != nil {
		return nil, err
	}

	// ponytail: one token per run; an agent alive for over a week would need to ask for a new one.
	week := int64(7 * 24 * 3600)
	tr, err := cs.CoreV1().ServiceAccounts("kube-system").CreateToken(ctx, agentAccount,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &week}},
		metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	cfg := rest.AnonymousClientConfig(admin)
	cfg.BearerToken = tr.Status.Token
	cfg.UserAgent = admin.UserAgent
	return cfg, nil
}

// runAgent sends a snapshot, then deltas, and a new snapshot each time resync fires.
func runAgent(ctx context.Context, namespace string, w io.Writer, resync <-chan struct{}) error {
	cfg, err := agentConfig(ctx)
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}

	f := informers.NewSharedInformerFactoryWithOptions(cs, 0, informers.WithNamespace(namespace))
	a := &agent{out: json.NewEncoder(w), kinds: map[string]cache.SharedIndexInformer{
		"Pod":        f.Core().V1().Pods().Informer(),
		"Deployment": f.Apps().V1().Deployments().Informer(),
		"ReplicaSet": f.Apps().V1().ReplicaSets().Informer(),
		"Service":    f.Core().V1().Services().Informer(),
		"Event":      f.Core().V1().Events().Informer(),
	}}
	var synced []cache.InformerSynced
	for kind, inf := range a.kinds {
		reg, err := inf.AddEventHandler(cache.ResourceEventHandlerDetailedFuncs{
			AddFunc: func(o any, initial bool) {
				if !initial {
					a.emit(protocol.Added, kind, nil, o)
				}
			},
			UpdateFunc: func(old, o any) { a.emit(protocol.Modified, kind, old, o) },
			DeleteFunc: func(o any) {
				if t, ok := o.(cache.DeletedFinalStateUnknown); ok {
					o = t.Obj
				}
				a.emit(protocol.Deleted, kind, nil, o)
			},
		})
		if err != nil {
			return err
		}
		synced = append(synced, reg.HasSynced)
	}

	// The registrations, not the caches: they are synced once the handler got the whole initial list.
	f.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), synced...) {
		return ctx.Err()
	}
	a.snapshot()
	for {
		select {
		case <-resync:
			a.snapshot()
		case <-ctx.Done():
			f.Shutdown()
			return nil
		}
	}
}
