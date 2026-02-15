package cmd

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkPVCs lists PersistentVolumeClaims that are not Bound.
func checkPVCs(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error) {
	pvcs, err := client.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing pvcs: %w", err)
	}

	bad := 0
	for _, pvc := range pvcs.Items {
		if reason := pvcProblem(&pvc); reason != "" {
			bad++
			fmt.Fprintf(w, "  %-50s %s\n", pvc.Namespace+"/"+pvc.Name, reason)
		}
	}
	return bad, nil
}

// pvcProblem returns a short reason if the PVC is unhealthy, or "".
func pvcProblem(pvc *corev1.PersistentVolumeClaim) string {
	switch pvc.Status.Phase {
	case corev1.ClaimBound:
		return ""
	case corev1.ClaimPending:
		return "Pending"
	case corev1.ClaimLost:
		return "Lost"
	default:
		return string(pvc.Status.Phase)
	}
}
