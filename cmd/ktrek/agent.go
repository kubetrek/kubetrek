package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
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
	if e, ok := obj.(*corev1.Event); ok {
		if t != protocol.Deleted {
			ce := clusterEvent(e)
			a.send(protocol.Message{Type: protocol.Event, Event: &ce})
		}
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

func runAgent(ctx context.Context, namespace string, w io.Writer) error {
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: "kind-" + clusterName},
	).ClientConfig()
	if err != nil {
		return fmt.Errorf("can't reach the ktrek cluster, is it up? %w", err)
	}
	cfg.UserAgent = "ktrek-agent/dev"
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
	<-ctx.Done()
	f.Shutdown()
	return nil
}
