package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
)

// AllResourceTypes lists every resource kind the plugin can check.
var AllResourceTypes = []string{
	"deployments",
	"nodes",
	"pods",
	"pvcs",
	"replicasets",
	"services",
}

// Options holds the configuration for the plugin command.
type Options struct {
	ConfigFlags   *genericclioptions.ConfigFlags
	AllNamespaces bool
	Resources     []string
	Streams       genericiooptions.IOStreams
}

// NewCmd creates the cobra command for kubectl-bad.
func NewCmd(streams genericiooptions.IOStreams, version string) *cobra.Command {
	o := &Options{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
		Streams:     streams,
	}

	cmd := &cobra.Command{
		Use:          "kubectl-bad [TYPE ...]",
		Short:        "A kubectl plugin to find bad things in your cluster",
		Long: `A kubectl plugin to find bad things in your cluster.

Specify one or more resource types to check: pods, nodes, deployments,
replicasets, services, pvcs. Pass "all" or omit arguments to check everything.`,
		Example:      "  kubectl bad pods services\n  kubectl bad all\n  kubectl bad",
		Version:      version,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return append([]string{"all"}, AllResourceTypes...), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			resources, err := resolveResources(args)
			if err != nil {
				return err
			}
			o.Resources = resources

			clientset, err := o.Clientset()
			if err != nil {
				return err
			}

			sv, err := clientset.Discovery().ServerVersion()
			if err != nil {
				return err
			}
			fmt.Fprintf(o.Streams.Out, "Connected to Kubernetes %s\n", sv.GitVersion)

			ns, err := o.Namespace()
			if err != nil {
				return err
			}
			if ns == "" {
				fmt.Fprintln(o.Streams.Out, "Namespace: all namespaces")
			} else {
				fmt.Fprintf(o.Streams.Out, "Namespace: %s\n", ns)
			}

			ctx := cmd.Context()
			totalBad := 0

			for _, r := range o.Resources {
				switch r {
				case "deployments":
					fmt.Fprintln(o.Streams.Out, "\n=== Deployments ===")
					n, err := checkWithFallback(ctx, clientset, ns, o.Streams.Out, checkDeployments)
					if err != nil {
						return err
					}
					totalBad += n
				case "nodes":
					fmt.Fprintln(o.Streams.Out, "\n=== Nodes ===")
					n, err := checkNodes(ctx, clientset, o.Streams.Out)
					if err != nil {
						return err
					}
					totalBad += n
				case "pods":
					fmt.Fprintln(o.Streams.Out, "\n=== Pods ===")
					n, err := checkWithFallback(ctx, clientset, ns, o.Streams.Out, checkPods)
					if err != nil {
						return err
					}
					totalBad += n
				case "replicasets":
					fmt.Fprintln(o.Streams.Out, "\n=== ReplicaSets ===")
					n, err := checkWithFallback(ctx, clientset, ns, o.Streams.Out, checkReplicaSets)
					if err != nil {
						return err
					}
					totalBad += n
				case "services":
					fmt.Fprintln(o.Streams.Out, "\n=== Services ===")
					n, err := checkWithFallback(ctx, clientset, ns, o.Streams.Out, checkServices)
					if err != nil {
						return err
					}
					totalBad += n
				}
			}

			fmt.Fprintf(o.Streams.Out, "\n%d issue(s) found\n", totalBad)
			return nil
		},
	}

	o.ConfigFlags.AddFlags(cmd.Flags())
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "If true, list across all namespaces")

	return cmd
}

// resolveResources validates positional args and returns the list of resource
// types to check. An empty args list or "all" returns every known type.
func resolveResources(args []string) ([]string, error) {
	if len(args) == 0 {
		return AllResourceTypes, nil
	}

	seen := make(map[string]bool)
	var out []string
	for _, a := range args {
		a = strings.ToLower(a)
		if a == "all" {
			return AllResourceTypes, nil
		}
		if !slices.Contains(AllResourceTypes, a) {
			return nil, fmt.Errorf("unknown resource type %q (valid: %s)", a, strings.Join(AllResourceTypes, ", "))
		}
		if !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// Clientset returns a Kubernetes clientset from the resolved kubeconfig.
func (o *Options) Clientset() (*kubernetes.Clientset, error) {
	config, err := o.ConfigFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// Namespace returns the resolved namespace. Returns "" when --all-namespaces is set.
func (o *Options) Namespace() (string, error) {
	if o.AllNamespaces {
		return "", nil
	}
	ns, _, err := o.ConfigFlags.ToRawKubeConfigLoader().Namespace()
	return ns, err
}
