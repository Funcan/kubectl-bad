package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Common node-group label keys across managed Kubernetes providers.
var nodeGroupLabels = []string{
	"eks.amazonaws.com/nodegroup",      // EKS managed
	"karpenter.sh/nodepool",            // Karpenter
	"cloud.google.com/gke-nodepool",    // GKE
	"agentpool",                        // AKS
	"node.kubernetes.io/instance-type", // fallback: instance type
	"kubernetes.azure.com/agentpool",   // AKS (alternative)
	"alpha.eksctl.io/nodegroup-name",   // eksctl
}

// checkNodes lists nodes that are not Ready, grouped by node group when possible.
func checkNodes(ctx context.Context, client kubernetes.Interface, w io.Writer) (int, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing nodes: %w", err)
	}

	type badNode struct {
		name   string
		reason string
	}

	grouped := make(map[string][]badNode) // nodegroup -> bad nodes
	for _, n := range nodes.Items {
		if reason := nodeProblem(&n); reason != "" {
			group := nodeGroup(&n)
			grouped[group] = append(grouped[group], badNode{name: n.Name, reason: reason})
		}
	}

	if len(grouped) == 0 {
		return 0, nil
	}

	// Sort groups for stable output.
	groups := make([]string, 0, len(grouped))
	for g := range grouped {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	bad := 0
	for _, g := range groups {
		nodes := grouped[g]
		bad += len(nodes)
		fmt.Fprintf(w, "  [%s] (%d node(s))\n", g, len(nodes))
		for _, n := range nodes {
			fmt.Fprintf(w, "    %-50s %s\n", n.name, n.reason)
		}
	}
	return bad, nil
}

// nodeProblem returns a short reason if the node is unhealthy, or "" if OK.
func nodeProblem(n *corev1.Node) string {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status != corev1.ConditionTrue {
				reason := c.Reason
				if reason == "" {
					reason = "NotReady"
				}
				if c.Message != "" {
					reason += ": " + c.Message
				}
				return reason
			}
			return ""
		}
	}
	// No Ready condition at all.
	return "NotReady (no condition)"
}

// nodeGroup returns the node-group name from well-known labels, or "(ungrouped)".
func nodeGroup(n *corev1.Node) string {
	for _, key := range nodeGroupLabels {
		if v, ok := n.Labels[key]; ok && v != "" {
			return v
		}
	}
	return "(ungrouped)"
}
