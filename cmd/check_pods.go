package cmd

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// checkPods lists pods that are not running successfully.
func checkPods(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error) {
	pods, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing pods: %w", err)
	}

	bad := 0
	for _, p := range pods.Items {
		if reason := podProblem(&p); reason != "" {
			bad++
			fmt.Fprintf(w, "  %-50s %-12s %s\n", p.Namespace+"/"+p.Name, p.Status.Phase, reason)
		}
	}
	return bad, nil
}

// podProblem returns a short reason string if the pod is unhealthy, or "" if OK.
func podProblem(p *corev1.Pod) string {
	// Succeeded pods (e.g. completed Jobs) are fine.
	if p.Status.Phase == corev1.PodSucceeded {
		return ""
	}

	// Check container statuses for waiting/terminated problems.
	for _, cs := range append(p.Status.InitContainerStatuses, p.Status.ContainerStatuses...) {
		if w := cs.State.Waiting; w != nil {
			switch w.Reason {
			case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull",
				"CreateContainerConfigError", "InvalidImageName",
				"CreateContainerError":
				return w.Reason
			}
		}
		if t := cs.State.Terminated; t != nil && t.ExitCode != 0 {
			reason := t.Reason
			if reason == "" {
				reason = fmt.Sprintf("exit %d", t.ExitCode)
			}
			return reason
		}
	}

	switch p.Status.Phase {
	case corev1.PodFailed:
		reason := p.Status.Reason
		if reason == "" {
			reason = "Failed"
		}
		return reason
	case corev1.PodPending:
		// Check for unschedulable conditions.
		for _, c := range p.Status.Conditions {
			if c.Type == corev1.PodScheduled && c.Status == corev1.ConditionFalse {
				return "Unschedulable: " + c.Message
			}
		}
		return "Pending"
	case corev1.PodUnknown:
		return "Unknown"
	}

	// Running with all containers ready is fine.
	return ""
}
