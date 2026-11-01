package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubetrek/kubetrek/internal/protocol"
)

func TestProjectKeepsSpecOut(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "web-x2k", Namespace: "default", Labels: map[string]string{"app": "web"},
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "web-7d4b9"}},
			Annotations:     map[string]string{"note": "hidden"},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "web", Env: []corev1.EnvVar{{Name: "PASSWORD", Value: "s3cret"}},
		}}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	b, _ := json.Marshal(project("Pod", pod))
	got := string(b)
	for _, want := range []string{`"owners":["ReplicaSet/web-7d4b9"]`, `"phase":"Running"`, `"app":"web"`} {
		if !strings.Contains(got, want) {
			t.Errorf("projection %s is missing %s", got, want)
		}
	}
	for _, leak := range []string{"s3cret", "PASSWORD", "hidden"} {
		if strings.Contains(got, leak) {
			t.Errorf("projection %s leaks %q", got, leak)
		}
	}
}

func TestProjectServiceSelector(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "web"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "web"}},
	}
	if o := project("Service", svc); o.Selector["app"] != "web" {
		t.Errorf("selector = %v, want app=web", o.Selector)
	}
	if o := project("Deployment", &appsv1.Deployment{}); o.Selector != nil {
		t.Errorf("only Services carry a selector, got %v", o.Selector)
	}
}

func TestEmitDropsUnchangedUpdates(t *testing.T) {
	var buf bytes.Buffer
	a := &agent{ready: true, out: json.NewEncoder(&buf)}
	ev := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-x2k"},
		Reason:         "Pulling",
		LastTimestamp:  metav1.Now(),
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "web-x2k", ResourceVersion: "1"}}
	relisted := pod.DeepCopy()
	relisted.ResourceVersion = "2"

	a.emit(protocol.Modified, "Event", ev, ev.DeepCopy())
	a.emit(protocol.Modified, "Pod", pod, relisted)
	if buf.Len() != 0 {
		t.Fatalf("unchanged updates were sent: %s", buf.String())
	}

	again := ev.DeepCopy()
	again.Count = 2
	again.LastTimestamp = metav1.NewTime(ev.LastTimestamp.Add(time.Minute))
	a.emit(protocol.Modified, "Event", ev, again)
	if !strings.Contains(buf.String(), `"reason":"Pulling"`) {
		t.Fatalf("a repeated event was dropped: %q", buf.String())
	}
}
