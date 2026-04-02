package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// maxParallelNamespaces is the number of namespaces queried concurrently
// when falling back from a cluster-wide list.
const maxParallelNamespaces = 5

// nsCheckFunc checks a single namespace and returns the number of issues found.
// The writer is safe for concurrent use when called from the fallback path.
type nsCheckFunc func(ns string, w io.Writer) (int, error)

// CheckFunc checks a single namespace and returns the number of issues found.
type CheckFunc func(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer) (int, error)

// DynamicCheckFunc checks a single namespace using the dynamic client and
// returns the number of issues found.
type DynamicCheckFunc func(ctx context.Context, client dynamic.Interface, ns string, w io.Writer) (int, error)

// ResourceCheckFunc checks a single namespace using both the typed and dynamic
// clients and returns the number of issues found.
type ResourceCheckFunc func(ctx context.Context, client kubernetes.Interface, dc dynamic.Interface, ns string, w io.Writer) (int, error)

// isForbidden returns true if the error looks like a 403 / RBAC denial.
func isForbidden(err error) bool {
	// k8s.io/apimachinery errors embed the status code in the message and
	// implement StatusReason, but a simple string check is the most robust
	// approach across client-go versions.
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "forbidden") || strings.Contains(s, "Forbidden")
}

// checkWithFallback tries fn cluster-wide (ns=""). If the call is forbidden it
// falls back to listing accessible namespaces and calling fn per-namespace with
// bounded parallelism, printing a warning for each inaccessible namespace.
func checkWithFallback(ctx context.Context, client kubernetes.Interface, ns string, w io.Writer, fn CheckFunc) (int, error) {
	return doFallback(ctx, client, ns, w, func(ns string, w io.Writer) (int, error) {
		return fn(ctx, client, ns, w)
	})
}

// checkWithFallbackDynamic mirrors checkWithFallback but uses the dynamic
// client for the actual resource check.
func checkWithFallbackDynamic(ctx context.Context, kclient kubernetes.Interface, dclient dynamic.Interface, ns string, w io.Writer, fn DynamicCheckFunc) (int, error) {
	return doFallback(ctx, kclient, ns, w, func(ns string, w io.Writer) (int, error) {
		return fn(ctx, dclient, ns, w)
	})
}

// checkWithFallbackResources mirrors checkWithFallback but passes both the
// typed and dynamic clients to the check function.
func checkWithFallbackResources(ctx context.Context, kclient kubernetes.Interface, dclient dynamic.Interface, ns string, w io.Writer, fn ResourceCheckFunc) (int, error) {
	return doFallback(ctx, kclient, ns, w, func(ns string, w io.Writer) (int, error) {
		return fn(ctx, kclient, dclient, ns, w)
	})
}

// doFallback implements the common try-cluster-wide-then-per-namespace pattern.
func doFallback(ctx context.Context, kclient kubernetes.Interface, ns string, w io.Writer, fn nsCheckFunc) (int, error) {
	if ns != "" {
		return fn(ns, w)
	}

	n, err := fn("", w)
	if err == nil {
		return n, nil
	}
	if !isForbidden(err) {
		return 0, err
	}

	fmt.Fprintln(w, "  (cluster-wide access denied, falling back to per-namespace queries)")

	nsList, err := kclient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("listing namespaces: %w", err)
	}

	var (
		total atomic.Int64
		mu    sync.Mutex
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

			count, err := fn(nsName, &syncWriter{mu: &mu, w: w})
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
