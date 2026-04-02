package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// ownerKey uniquely identifies a Kubernetes object for deduplication.
type ownerKey struct {
	Kind      string
	Namespace string
	Name      string
}

func (k ownerKey) String() string {
	if k.Namespace == "" {
		return fmt.Sprintf("%s/%s", k.Kind, k.Name)
	}
	return fmt.Sprintf("%s/%s/%s", k.Kind, k.Namespace, k.Name)
}

// checkResources lists pods that are missing required resource constraints
// (memory request, memory limit, cpu request) and reports them grouped by
// their ultimate owner (Deployment, StatefulSet, Job, etc.).
func checkResources(ctx context.Context, client kubernetes.Interface, dc dynamic.Interface, ns string, w io.Writer) (int, error) {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing pods: %w", err)
	}

	ownerCache := make(map[ownerKey]*metav1.OwnerReference)

	// Collect issues grouped by parent.
	type parentIssue struct {
		Missing  map[string]bool
		PodCount int
	}
	grouped := make(map[ownerKey]*parentIssue)
	// Track insertion order for stable output.
	var parentOrder []ownerKey

	for i := range pods.Items {
		p := &pods.Items[i]

		// Skip completed pods (Jobs that finished successfully).
		if p.Status.Phase == corev1.PodSucceeded {
			continue
		}

		missing := missingResources(p)
		if len(missing) == 0 {
			continue
		}

		parent := resolveUltimateOwner(ctx, dc, p.Namespace, p.OwnerReferences, ownerCache)

		pi, exists := grouped[parent]
		if !exists {
			pi = &parentIssue{Missing: make(map[string]bool)}
			grouped[parent] = pi
			parentOrder = append(parentOrder, parent)
		}
		pi.PodCount++
		for _, m := range missing {
			pi.Missing[m] = true
		}
	}

	if len(grouped) == 0 {
		return 0, nil
	}

	for _, parent := range parentOrder {
		pi := grouped[parent]
		fields := sortedKeys(pi.Missing)
		fmt.Fprintf(w, "  %-50s missing %s (%d pod(s))\n",
			parent, strings.Join(fields, ", "), pi.PodCount)
	}

	return len(grouped), nil
}

// missingResources checks all containers in a pod and returns a deduplicated
// list of missing resource fields.
func missingResources(p *corev1.Pod) []string {
	seen := make(map[string]bool)
	containers := append(p.Spec.InitContainers, p.Spec.Containers...)
	for _, c := range containers {
		req := c.Resources.Requests
		lim := c.Resources.Limits

		if req.Cpu().IsZero() {
			seen["cpu request"] = true
		}
		if req.Memory().IsZero() {
			seen["memory request"] = true
		}
		if lim.Memory().IsZero() {
			seen["memory limit"] = true
		}
	}
	return sortedKeys(seen)
}

// resolveUltimateOwner walks ownerReferences up to the terminal parent.
// It returns an ownerKey for the ultimate owner. If there are no owners,
// it returns the pod itself.
func resolveUltimateOwner(
	ctx context.Context,
	dc dynamic.Interface,
	namespace string,
	ownerRefs []metav1.OwnerReference,
	cache map[ownerKey]*metav1.OwnerReference,
) ownerKey {
	if len(ownerRefs) == 0 {
		return ownerKey{Kind: "Pod", Namespace: namespace}
	}

	// Walk the first ownerReference (the controller).
	ref := pickControllerRef(ownerRefs)
	current := ownerKey{
		Kind:      ref.Kind,
		Namespace: namespace,
		Name:      ref.Name,
	}

	for {
		// Check cache first.
		if cachedRef, ok := cache[current]; ok {
			if cachedRef == nil {
				// Terminal — no further owners.
				return current
			}
			current = ownerKey{
				Kind:      cachedRef.Kind,
				Namespace: namespace,
				Name:      cachedRef.Name,
			}
			continue
		}

		// Fetch the owner to find its ownerReferences.
		gvr, err := gvrFromRef(ref)
		if err != nil {
			cache[current] = nil
			return current
		}

		obj, err := dc.Resource(gvr).Namespace(namespace).Get(ctx, current.Name, metav1.GetOptions{})
		if err != nil {
			// Owner not found or inaccessible — stop here.
			cache[current] = nil
			return current
		}

		parentRefs := obj.GetOwnerReferences()
		if len(parentRefs) == 0 {
			cache[current] = nil
			return current
		}

		parentRef := pickControllerRef(parentRefs)
		cache[current] = &parentRef
		ref = parentRef
		current = ownerKey{
			Kind:      parentRef.Kind,
			Namespace: namespace,
			Name:      parentRef.Name,
		}
	}
}

// pickControllerRef returns the controller ownerReference, or the first one if
// none is marked as controller.
func pickControllerRef(refs []metav1.OwnerReference) metav1.OwnerReference {
	for _, r := range refs {
		if r.Controller != nil && *r.Controller {
			return r
		}
	}
	return refs[0]
}

// gvrFromRef derives a GroupVersionResource from an OwnerReference.
func gvrFromRef(ref metav1.OwnerReference) (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	// Convert Kind to lowercase plural as a best-effort resource name.
	resource := strings.ToLower(ref.Kind) + "s"
	// Handle common irregular plurals.
	switch strings.ToLower(ref.Kind) {
	case "ingress":
		resource = "ingresses"
	}
	return schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: resource,
	}, nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
