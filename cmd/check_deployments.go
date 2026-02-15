package cmd

import (
	"context"
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkDeployments lists deployments with unavailable replicas.
func checkDeployments(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error) {
	deploys, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing deployments: %w", err)
	}

	bad := 0
	for _, d := range deploys.Items {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		available := d.Status.AvailableReplicas
		unavailable := d.Status.UnavailableReplicas

		if unavailable > 0 || available < desired {
			bad++
			fmt.Fprintf(w, "  %-50s %d/%d available, %d unavailable\n",
				d.Namespace+"/"+d.Name, available, desired, unavailable)
		}
	}
	return bad, nil
}
