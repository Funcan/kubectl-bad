package cmd

import (
	"context"
	"fmt"
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var externalSecretGVR = schema.GroupVersionResource{
	Group:    "external-secrets.io",
	Version:  "v1",
	Resource: "externalsecrets",
}

// checkExternalSecrets lists ExternalSecrets whose Ready condition is not True.
func checkExternalSecrets(ctx context.Context, client dynamic.Interface, ns string, w io.Writer) (int, error) {
	var list *unstructured.UnstructuredList
	var err error
	if ns == "" {
		list, err = client.Resource(externalSecretGVR).List(ctx, metav1.ListOptions{})
	} else {
		list, err = client.Resource(externalSecretGVR).Namespace(ns).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return 0, fmt.Errorf("listing externalsecrets: %w", err)
	}

	bad := 0
	for i := range list.Items {
		es := &list.Items[i]
		if reason := externalSecretProblem(es); reason != "" {
			bad++
			fmt.Fprintf(w, "  %-50s %s\n", es.GetNamespace()+"/"+es.GetName(), reason)
		}
	}
	return bad, nil
}

// externalSecretProblem returns a short reason if the ExternalSecret is
// unhealthy (Ready condition is not True), or "" if it looks healthy.
func externalSecretProblem(es *unstructured.Unstructured) string {
	conditions, found, err := unstructured.NestedSlice(es.Object, "status", "conditions")
	if err != nil || !found || len(conditions) == 0 {
		return "No status conditions"
	}

	for _, raw := range conditions {
		cond, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] != "Ready" {
			continue
		}
		if cond["status"] == "True" {
			return ""
		}
		reason, _ := cond["reason"].(string)
		message, _ := cond["message"].(string)
		if reason != "" && message != "" {
			return fmt.Sprintf("%s: %s", reason, message)
		}
		if reason != "" {
			return reason
		}
		status, _ := cond["status"].(string)
		return "Ready=" + status
	}

	return "No Ready condition"
}
