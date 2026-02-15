package main

import (
	"os"

	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericiooptions"

	"kubeplugin/cmd"
)

var version = "dev"

func main() {
	pflag.CommandLine = pflag.NewFlagSet("kubectl-bad", pflag.ExitOnError)

	root := cmd.NewCmd(genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, version)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
