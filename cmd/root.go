package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/kubernetes"
)

// Options holds the configuration for the plugin command.
type Options struct {
	ConfigFlags   *genericclioptions.ConfigFlags
	AllNamespaces bool
	Streams       genericiooptions.IOStreams
}

// NewCmd creates the cobra command for kubectl-bad.
func NewCmd(streams genericiooptions.IOStreams, version string) *cobra.Command {
	o := &Options{
		ConfigFlags: genericclioptions.NewConfigFlags(true),
		Streams:     streams,
	}

	cmd := &cobra.Command{
		Use:          "kubectl-bad",
		Short:        "A kubectl plugin to find bad things in your cluster",
		Version:      version,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			return nil
		},
	}

	o.ConfigFlags.AddFlags(cmd.Flags())
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "If true, list across all namespaces")

	return cmd
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
