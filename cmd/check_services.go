package cmd

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkServices lists services that have no endpoints.
func checkServices(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error) {
	svcs, err := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing services: %w", err)
	}

	bad := 0
	for _, svc := range svcs.Items {
		if reason := serviceProblem(ctx, client, &svc); reason != "" {
			bad++
			fmt.Fprintf(w, "  %-50s %s\n", svc.Namespace+"/"+svc.Name, reason)
		}
	}
	return bad, nil
}

// serviceProblem returns a short reason if the service is unhealthy, or "".
func serviceProblem(ctx context.Context, client kubernetes.Interface, svc *corev1.Service) string {
	// ExternalName services don't use endpoints.
	if svc.Spec.Type == corev1.ServiceTypeExternalName {
		return ""
	}
	// Headless services without a selector are used for manual endpoint management;
	// skip them to avoid false positives.
	if svc.Spec.ClusterIP == "None" && len(svc.Spec.Selector) == 0 {
		return ""
	}

	slices, err := client.DiscoveryV1().EndpointSlices(svc.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("kubernetes.io/service-name=%s", svc.Name),
	})
	if err != nil {
		return "no endpoints (error fetching)"
	}

	ready := 0
	notReady := 0
	for _, slice := range slices.Items {
		for _, ep := range slice.Endpoints {
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				ready += len(ep.Addresses)
			} else {
				notReady += len(ep.Addresses)
			}
		}
	}

	if ready == 0 && notReady == 0 {
		return "no endpoints"
	}
	if ready == 0 {
		return fmt.Sprintf("0 ready endpoints (%d not ready)", notReady)
	}
	return ""
}
