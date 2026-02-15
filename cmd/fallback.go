package cmd

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// maxParallelNamespaces is the number of namespaces queried concurrently
// when falling back from a cluster-wide list.
const maxParallelNamespaces = 5

// CheckFunc checks a single namespace and returns the number of issues found.
type CheckFunc func(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error)

// isForbidden returns true if the error looks like a 403 / RBAC denial.
func isForbidden(err error) bool {
	// k8s.io/apimachinery errors embed the status code in the message and
	// implement StatusReason, but a simple string check is the most robust
	// approach across client-go versions.
	if err == nil {
		return false
	}
	s := err.Error()
	for _, substr := range []string{"forbidden", "Forbidden", "is forbidden"} {
		if len(s) >= len(substr) && containsSubstring(s, substr) {
			return true
		}
	}
	return false
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// checkWithFallback tries fn cluster-wide (ns=""). If the call is forbidden it
// falls back to listing accessible namespaces and calling fn per-namespace with
// bounded parallelism, printing a warning for each inaccessible namespace.
func checkWithFallback(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer, fn CheckFunc) (int, error) {
	// If a specific namespace was requested, just call directly.
	if ns != "" {
		return fn(ctx, client, ns, w)
	}

	// Try cluster-wide first.
	n, err := fn(ctx, client, "", w)
	if err == nil {
		return n, nil
	}
	if !isForbidden(err) {
		return 0, err
	}

	// Cluster-wide access denied — fall back to per-namespace.
	fmt.Fprintln(w, "  (cluster-wide access denied, falling back to per-namespace queries)")

	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing namespaces: %w", err)
	}

	var (
		total atomic.Int64
		mu    sync.Mutex // serialises writes to w
		sem   = make(chan struct{}, maxParallelNamespaces)
		wg    sync.WaitGroup
	)

	for _, nsObj := range nsList.Items {
		nsName := nsObj.Name
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			count, err := fn(ctx, client, nsName, &syncWriter{mu: &mu, w: w})
			if err != nil {
				if isForbidden(err) {
					mu.Lock()
					fmt.Fprintf(w, "  WARNING: cannot access namespace %q (forbidden)\n", nsName)
					mu.Unlock()
					return
				}
				mu.Lock()
				fmt.Fprintf(w, "  WARNING: error checking namespace %q: %v\n", nsName, err)
				mu.Unlock()
				return
			}
			total.Add(int64(count))
		}()
	}

	wg.Wait()
	return int(total.Load()), nil
}

// syncWriter wraps a writer with a mutex so concurrent goroutines produce
// complete lines without interleaving.
type syncWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
