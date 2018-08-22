package main

import (
	"os"

	"github.com/spf13/pflag"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/sample-cli-plugin/pkg/cmd"
)

type Executable interface {
	Execute() error
}

func newRootCommand() Executable {
	return cmd.NewCmdNamespace(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
}

func main() {
	flags := pflag.NewFlagSet("kubectl-ns", pflag.ExitOnError)
	pflag.CommandLine = flags

	runnable := newRootCommand()
	if err := runnable.Execute(); err != nil {
		os.Exit(1)
	}
}
