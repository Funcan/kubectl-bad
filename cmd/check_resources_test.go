package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
)

func boolPtr(b bool) *bool { return &b }

func podWithResources(name string, reqs, lims corev1.ResourceList) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "nginx",
				Resources: corev1.ResourceRequirements{
					Requests: reqs,
					Limits:   lims,
				},
			}},
		},
	}
}

// ---------------------------------------------------------------------------
// missingResources tests
// ---------------------------------------------------------------------------

func TestMissingResources_AllSet(t *testing.T) {
	p := podWithResources("full", corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}, corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	})
	got := missingResources(p)
	if len(got) != 0 {
		t.Fatalf("expected no missing resources, got %v", got)
	}
}

func TestMissingResources_MissingCPURequest(t *testing.T) {
	p := podWithResources("nocpu", corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}, corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	})
	got := missingResources(p)
	if len(got) != 1 || got[0] != "cpu request" {
		t.Fatalf("expected [cpu request], got %v", got)
	}
}

func TestMissingResources_MissingMemoryLimit(t *testing.T) {
	p := podWithResources("nomlim", corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}, corev1.ResourceList{})
	got := missingResources(p)
	if len(got) != 1 || got[0] != "memory limit" {
		t.Fatalf("expected [memory limit], got %v", got)
	}
}

func TestMissingResources_MissingEverything(t *testing.T) {
	p := podWithResources("empty", nil, nil)
	got := missingResources(p)
	want := []string{"cpu request", "memory limit", "memory request"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestMissingResources_InitContainerMissing(t *testing.T) {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "init", Namespace: "default"},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name:  "init",
				Image: "busybox",
				// no resources
			}},
			Containers: []corev1.Container{{
				Name:  "app",
				Image: "nginx",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
			}},
		},
	}
	got := missingResources(p)
	// The init container is missing everything.
	want := []string{"cpu request", "memory limit", "memory request"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestMissingResources_SucceededPod(t *testing.T) {
	// missingResources doesn't check phase — it should still report missing fields.
	p := podWithResources("done", nil, nil)
	p.Status.Phase = corev1.PodSucceeded
	got := missingResources(p)
	if len(got) != 3 {
		t.Fatalf("expected 3 missing fields even for succeeded pod, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// resolveUltimateOwner tests
// ---------------------------------------------------------------------------

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"},
		&unstructured.Unstructured{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSetList"},
		&unstructured.UnstructuredList{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		&unstructured.Unstructured{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymentList"},
		&unstructured.UnstructuredList{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"},
		&unstructured.Unstructured{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "JobList"},
		&unstructured.UnstructuredList{},
	)
	return s
}

func TestResolveUltimateOwner_BarePod(t *testing.T) {
	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme)
	cache := make(map[ownerKey]*metav1.OwnerReference)

	got := resolveUltimateOwner(context.Background(), dc, "default", nil, cache)
	want := ownerKey{Kind: "Pod", Namespace: "default"}
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestResolveUltimateOwner_RSToDeployment(t *testing.T) {
	deploy := &unstructured.Unstructured{}
	deploy.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	deploy.SetNamespace("default")
	deploy.SetName("my-deploy")

	rs := &unstructured.Unstructured{}
	rs.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"})
	rs.SetNamespace("default")
	rs.SetName("my-rs")
	rs.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "my-deploy",
		Controller: boolPtr(true),
	}})

	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme, rs, deploy)
	cache := make(map[ownerKey]*metav1.OwnerReference)

	podRefs := []metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "ReplicaSet",
		Name:       "my-rs",
		Controller: boolPtr(true),
	}}

	got := resolveUltimateOwner(context.Background(), dc, "default", podRefs, cache)
	want := ownerKey{Kind: "Deployment", Namespace: "default", Name: "my-deploy"}
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestResolveUltimateOwner_JobTerminal(t *testing.T) {
	job := &unstructured.Unstructured{}
	job.SetGroupVersionKind(schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"})
	job.SetNamespace("default")
	job.SetName("my-job")
	// No owner references — terminal.

	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme, job)
	cache := make(map[ownerKey]*metav1.OwnerReference)

	podRefs := []metav1.OwnerReference{{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       "my-job",
		Controller: boolPtr(true),
	}}

	got := resolveUltimateOwner(context.Background(), dc, "default", podRefs, cache)
	want := ownerKey{Kind: "Job", Namespace: "default", Name: "my-job"}
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestResolveUltimateOwner_MissingOwner(t *testing.T) {
	// The ReplicaSet doesn't exist in the dynamic client → graceful fallback.
	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme)
	cache := make(map[ownerKey]*metav1.OwnerReference)

	podRefs := []metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "ReplicaSet",
		Name:       "ghost-rs",
		Controller: boolPtr(true),
	}}

	got := resolveUltimateOwner(context.Background(), dc, "default", podRefs, cache)
	want := ownerKey{Kind: "ReplicaSet", Namespace: "default", Name: "ghost-rs"}
	if got != want {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

// ---------------------------------------------------------------------------
// checkResources integration tests
// ---------------------------------------------------------------------------

func TestCheckResources_DeduplicatedByDeployment(t *testing.T) {
	deploy := &unstructured.Unstructured{}
	deploy.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
	deploy.SetNamespace("default")
	deploy.SetName("web")

	rs := &unstructured.Unstructured{}
	rs.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"})
	rs.SetNamespace("default")
	rs.SetName("web-rs")
	rs.SetOwnerReferences([]metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "web",
		Controller: boolPtr(true),
	}})

	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme, rs, deploy)

	ownerRefs := []metav1.OwnerReference{{
		APIVersion: "apps/v1",
		Kind:       "ReplicaSet",
		Name:       "web-rs",
		Controller: boolPtr(true),
	}}

	pod1 := podWithResources("web-1", nil, nil)
	pod1.OwnerReferences = ownerRefs
	pod2 := podWithResources("web-2", nil, nil)
	pod2.OwnerReferences = ownerRefs

	client := fake.NewClientset(pod1, pod2)

	var buf bytes.Buffer
	count, err := checkResources(context.Background(), client, dc, "default", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 group, got %d", count)
	}
	if !strings.Contains(buf.String(), "2 pod(s)") {
		t.Fatalf("expected '2 pod(s)' in output, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "Deployment/default/web") {
		t.Fatalf("expected Deployment/default/web in output, got: %s", buf.String())
	}
}

func TestCheckResources_MixHealthyAndUnhealthy(t *testing.T) {
	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme)

	healthy := podWithResources("good", corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}, corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	})

	unhealthy := podWithResources("bad", nil, nil)

	client := fake.NewClientset(healthy, unhealthy)

	var buf bytes.Buffer
	count, err := checkResources(context.Background(), client, dc, "default", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 group (only unhealthy), got %d", count)
	}
	if strings.Contains(buf.String(), "good") {
		t.Fatalf("healthy pod should not appear in output: %s", buf.String())
	}
}

func TestCheckResources_AllHealthy(t *testing.T) {
	scheme := newScheme()
	dc := fakedynamic.NewSimpleDynamicClient(scheme)

	p := podWithResources("ok", corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("100m"),
		corev1.ResourceMemory: resource.MustParse("128Mi"),
	}, corev1.ResourceList{
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	})

	client := fake.NewClientset(p)

	var buf bytes.Buffer
	count, err := checkResources(context.Background(), client, dc, "default", &buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got: %s", buf.String())
	}
}
