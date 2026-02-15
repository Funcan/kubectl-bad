package cmd

import (
	"context"
	"fmt"
	"io"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkReplicaSets lists replicasets whose owner has been deleted or that have
// fewer ready replicas than desired.
func checkReplicaSets(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error) {
	rsList, err := client.AppsV1().ReplicaSets(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing replicasets: %w", err)
	}

	bad := 0
	for _, rs := range rsList.Items {
		if reason := replicaSetProblem(&rs); reason != "" {
			bad++
			fmt.Fprintf(w, "  %-50s %s\n", rs.Namespace+"/"+rs.Name, reason)
		}
	}
	return bad, nil
}

// replicaSetProblem returns a short reason if the replicaset is unhealthy, or "".
func replicaSetProblem(rs *appsv1.ReplicaSet) string {
	// Skip scaled-to-zero replicasets (old rollout remnants).
	desired := int32(0)
	if rs.Spec.Replicas != nil {
		desired = *rs.Spec.Replicas
	}
	if desired == 0 {
		return ""
	}

	ownedByDeployment := hasControllerOwner(rs.OwnerReferences, "Deployment")

	// Check for orphaned replicasets (owner deleted).
	if isOrphaned(rs.OwnerReferences) {
		return fmt.Sprintf("orphaned (owner deleted), %d/%d ready", rs.Status.ReadyReplicas, desired)
	}

	// Under-replicated replicasets owned by a Deployment are already
	// surfaced by the deployment checker, so skip them here.
	if !ownedByDeployment && rs.Status.ReadyReplicas < desired {
		return fmt.Sprintf("%d/%d ready", rs.Status.ReadyReplicas, desired)
	}

	return ""
}

// isOrphaned returns true if owner references exist but none claim to be the
// controller, suggesting the controlling owner was deleted.
func isOrphaned(refs []metav1.OwnerReference) bool {
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller {
			return false
		}
	}
	return true
}

// hasControllerOwner returns true if any owner reference is a controller of the
// given Kind.
func hasControllerOwner(refs []metav1.OwnerReference, kind string) bool {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller && ref.Kind == kind {
			return true
		}
	}
	return false
}
