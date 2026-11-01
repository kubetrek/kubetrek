package main

import (
	"encoding/json"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
