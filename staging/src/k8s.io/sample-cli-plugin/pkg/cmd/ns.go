/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var (
	namespace_example = `
	# view the current namespace in your KUBECONFIG
	%[1]s ns

	# view all of the namespaces in use by contexts in your KUBECONFIG
	%[1]s ns --list

	# switch your current-context to one that contains the desired namespace
	%[1]s ns foo
`

	errNoContext = fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
)

type NamespaceOptions struct {
	configFlags *genericclioptions.ConfigFlags

	newClusterValue string
	newContextName  string
	nsValue         string
	rawConfig       api.Config
	listNamespaces  bool
	args            []string

	genericclioptions.IOStreams
}

func NewNamespaceOptions(streams genericclioptions.IOStreams) *NamespaceOptions {
	return &NamespaceOptions{
		configFlags: genericclioptions.NewConfigFlags(),

		IOStreams: streams,
	}
}

func NewCmdNamespace(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewNamespaceOptions(streams)

	cmd := &cobra.Command{
		Use:          "ns [new-namespace] [flags]",
		Short:        "View or set the current namespace",
		Example:      fmt.Sprintf(namespace_example, "kubectl"),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&o.listNamespaces, "list", o.listNamespaces, "if true, print the list of all namespaces in the current KUBECONFIG")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func (o *NamespaceOptions) Complete(cmd *cobra.Command, args []string) error {
	var err error
	o.rawConfig, err = o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}

	o.nsValue, err = cmd.Flags().GetString("namespace")
	if err != nil {
		return err
	}

	o.newContextName, err = cmd.Flags().GetString("context")
	if err != nil {
		return err
	}

	o.newClusterValue, err = cmd.Flags().GetString("cluster")
	if err != nil {
		return err
	}

	o.args = args
	return nil
}

func (o *NamespaceOptions) Validate() error {
	if len(o.rawConfig.CurrentContext) == 0 {
		return errNoContext
	}
	if len(o.args) > 1 {
		return fmt.Errorf("either one or no arguments are allowed")
	}

	if len(o.args) > 0 && len(o.nsValue) > 0 {
		return fmt.Errorf("cannot specify both a --namespace value and a new namespace argument")
	}

	return nil
}

func (o *NamespaceOptions) Run() error {
	newNamespace := o.nsValue
	if len(o.args) > 0 && len(o.args[0]) > 0 {
		newNamespace = o.args[0]
	}

	if len(newNamespace) > 0 {
		return o.setNamespace(newNamespace)
	}

	namespaces := map[string]bool{}

	for name, c := range o.rawConfig.Contexts {
		if !o.listNamespaces && name == o.rawConfig.CurrentContext {
			if len(c.Namespace) == 0 {
				return fmt.Errorf("no namespace is set for your current context: %q", name)
			}

			fmt.Fprintf(o.Out, "%s\n", c.Namespace)
			return nil
		}

		// skip if dealing with a namespace we have already seen
		// or if the namespace for the current context is empty
		if len(c.Namespace) == 0 {
			continue
		}
		if namespaces[c.Namespace] {
			continue
		}

		namespaces[c.Namespace] = true
	}

	if !o.listNamespaces {
		return fmt.Errorf("unable to find information for the current namespace in your configuration")
	}

	for n := range namespaces {
		fmt.Fprintf(o.Out, "%s\n", n)
	}

	return nil
}

// isUsingRequestedNamespace determines if a user's current context is already
// suitable for providing the user-specified namespace. A context is only
// "suitable" if it follows the rules detailed in the "setNamespace" method doc.
func (o *NamespaceOptions) isUsingRequestedNamespace(newNamespace string) bool {
	existingCtx, ok := o.rawConfig.Contexts[o.rawConfig.CurrentContext]
	if !ok {
		return false
	}

	ret := existingCtx.Namespace == newNamespace
	if len(o.newContextName) > 0 {
		ret = ret && o.rawConfig.CurrentContext == o.newContextName
	}
	if len(o.newClusterValue) > 0 {
		ret = ret && existingCtx.Cluster == o.newClusterValue
	}

	return ret
}

// setNamespace receives a new namespace and finds an existing, qualifying KUBECONFIG
// context containing the provided namespace, or creates a new qualifying context containing it.
// a context is "qualifying" if:
//   1. it contains the user-specified cluster or the same cluster as the current context
//   2. it contains the user-specified auth info or the same auth info as the current context
//   3. it contains the user-specified namespace
func (o *NamespaceOptions) setNamespace(newNamespace string) error {
	if len(newNamespace) == 0 {
		return fmt.Errorf("a non-empty namespace must be provided")
	}

	existingCtx, ok := o.rawConfig.Contexts[o.rawConfig.CurrentContext]
	if !ok {
		return errNoContext
	}

	if o.isUsingRequestedNamespace(newNamespace) {
		fmt.Fprintf(o.Out, "already using namespace %q\n", newNamespace)
		return nil
	}

	existingClusterName := o.newClusterValue
	if len(existingClusterName) == 0 {
		existingClusterName = existingCtx.Cluster
	}

	existingCtxName := ""

	// determine if a suitable context exists, which contains the new namespace.
	// we only do this if a context name was not explicitly given by the user.
	if len(o.newContextName) == 0 {
		for name, c := range o.rawConfig.Contexts {
			if c.Namespace != newNamespace || c.Cluster != existingClusterName || c.AuthInfo != existingCtx.AuthInfo {
				continue
			}

			existingCtxName = name
			break
		}
	}

	// no existing context was found to be suitable, create new ctx containing
	// existing context's auth and cluster info, and set the new namespace
	if len(existingCtxName) == 0 {
		newCtx := api.NewContext()
		newCtx.AuthInfo = existingCtx.AuthInfo
		newCtx.Cluster = existingClusterName
		newCtx.Namespace = newNamespace

		// we only generate a new name for our context
		// if a user did not explicitly provide one
		newCtxName := o.newContextName
		if len(newCtxName) == 0 {
			newCtxName = newNamespace
			if len(existingClusterName) > 0 {
				newCtxName = fmt.Sprintf("%s/%s", newCtxName, existingClusterName)
			}
			if len(existingCtx.AuthInfo) > 0 {
				cleanAuthInfo := strings.Split(existingCtx.AuthInfo, "/")[0]
				newCtxName = fmt.Sprintf("%s/%s", newCtxName, cleanAuthInfo)
			}
		}

		o.rawConfig.Contexts[newCtxName] = newCtx
		existingCtxName = newCtxName
	}

	configAccess := clientcmd.NewDefaultPathOptions()
	o.rawConfig.CurrentContext = existingCtxName

	if err := clientcmd.ModifyConfig(configAccess, o.rawConfig, true); err != nil {
		return err
	}

	fmt.Fprintf(o.Out, "namespace changed to %q\n", newNamespace)
	return nil
}
