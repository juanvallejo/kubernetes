/*
Copyright 2014 The Kubernetes Authors.

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

package resource

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	"net/url"

	kapierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	metav1beta1 "k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	api "k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/kubectl"
	"k8s.io/kubernetes/pkg/kubectl/cmd/templates"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/kubectl/cmd/util/openapi"
	flags "k8s.io/kubernetes/pkg/kubectl/printers"
	"k8s.io/kubernetes/pkg/kubectl/resource"
	"k8s.io/kubernetes/pkg/kubectl/util/i18n"
	"k8s.io/kubernetes/pkg/printers"
	"k8s.io/kubernetes/pkg/util/interrupt"
)

// GetOptions contains the input to the get command.
type GetOptions struct {
	HumanPrintFlags *flags.HumanPrintFlags

	Out, ErrOut io.Writer

	resource.FilenameOptions

	Raw       string
	Watch     bool
	WatchOnly bool
	ChunkSize int64

	LabelSelector     string
	FieldSelector     string
	AllNamespaces     bool
	Namespace         string
	ExplicitNamespace bool

	ServerPrint bool

	PrintObj func(runtime.Object, *resource.Info, io.Writer) error

	SortBy         string
	GenericPrinter bool
	IgnoreNotFound bool
	ShowKind       bool
	LabelColumns   []string
	Export         bool

	OutputFormat string

	IncludeUninitialized bool
}

var (
	getLong = templates.LongDesc(`
		Display one or many resources

		Prints a table of the most important information about the specified resources.
		You can filter the list using a label selector and the --selector flag. If the
		desired resource type is namespaced you will only see results in your current
		namespace unless you pass --all-namespaces.

		Uninitialized objects are not shown unless --include-uninitialized is passed.

		By specifying the output as 'template' and providing a Go template as the value
		of the --template flag, you can filter the attributes of the fetched resources.`)

	getExample = templates.Examples(i18n.T(`
		# List all pods in ps output format.
		kubectl get pods

		# List all pods in ps output format with more information (such as node name).
		kubectl get pods -o wide

		# List a single replication controller with specified NAME in ps output format.
		kubectl get replicationcontroller web

		# List a single pod in JSON output format.
		kubectl get -o json pod web-pod-13je7

		# List a pod identified by type and name specified in "pod.yaml" in JSON output format.
		kubectl get -f pod.yaml -o json

		# Return only the phase value of the specified pod.
		kubectl get -o template pod/web-pod-13je7 --template={{.status.phase}}

		# List all replication controllers and services together in ps output format.
		kubectl get rc,services

		# List one or more resources by their type and names.
		kubectl get rc/web service/frontend pods/web-pod-13je7

		# List all resources with different types.
		kubectl get all`))
)

const (
	useOpenAPIPrintColumnFlagLabel = "use-openapi-print-columns"
	useServerPrintColumns          = "experimental-server-print"
)

// NewGetOptions returns a GetOptions with default chunk size 500.
func NewGetOptions(out io.Writer, errOut io.Writer) *GetOptions {
	return &GetOptions{
		HumanPrintFlags: flags.NewHumanPrintFlags(false, false, false),

		Out:       out,
		ErrOut:    errOut,
		ChunkSize: 500,
	}
}

// NewCmdGet creates a command object for the generic "get" action, which
// retrieves one or more resources from a server.
func NewCmdGet(f cmdutil.Factory, out io.Writer, errOut io.Writer) *cobra.Command {
	options := NewGetOptions(out, errOut)
	validArgs := cmdutil.ValidArgList(f)

	cmd := &cobra.Command{
		Use: "get [(-o|--output=)json|yaml|wide|custom-columns=...|custom-columns-file=...|go-template=...|go-template-file=...|jsonpath=...|jsonpath-file=...] (TYPE [NAME | -l label] | TYPE/NAME ...) [flags]",
		DisableFlagsInUseLine: true,
		Short:   i18n.T("Display one or many resources"),
		Long:    getLong + "\n\n" + cmdutil.ValidResourceTypeList(f),
		Example: getExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(options.Complete(f, cmd, args))
			cmdutil.CheckErr(options.Validate(cmd))
			cmdutil.CheckErr(options.Run(f, cmd, args))
		},
		SuggestFor: []string{"list", "ps"},
		ValidArgs:  validArgs,
		ArgAliases: kubectl.ResourceAliases(validArgs),
	}

	// bind command-specific flags
	cmd.Flags().BoolVar(&options.HumanPrintFlags.NoHeaders, "no-headers", options.HumanPrintFlags.NoHeaders, "When using the default or custom-column output format, don't print headers (default print headers).")
	cmd.Flags().StringVar(&options.Raw, "raw", options.Raw, "Raw URI to request from the server.  Uses the transport specified by the kubeconfig file.")
	cmd.Flags().BoolVarP(&options.Watch, "watch", "w", options.Watch, "After listing/getting the requested object, watch for changes. Uninitialized objects are excluded if no object name is provided.")
	cmd.Flags().BoolVar(&options.WatchOnly, "watch-only", options.WatchOnly, "Watch for changes to the requested object(s), without listing/getting first.")
	cmd.Flags().Int64Var(&options.ChunkSize, "chunk-size", options.ChunkSize, "Return large lists in chunks rather than all at once. Pass 0 to disable. This flag is beta and may change in the future.")
	cmd.Flags().BoolVar(&options.IgnoreNotFound, "ignore-not-found", options.IgnoreNotFound, "If the requested object does not exist the command will return exit code 0.")
	cmd.Flags().StringVarP(&options.LabelSelector, "selector", "l", options.LabelSelector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().StringVar(&options.FieldSelector, "field-selector", options.FieldSelector, "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selector key1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	cmd.Flags().BoolVar(&options.AllNamespaces, "all-namespaces", options.AllNamespaces, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")

	// bind human-readable printer flags
	options.HumanPrintFlags.AddFlags(cmd)
	cmd.Flags().StringVarP(&options.OutputFormat, "output", "o", "", "Output format. One of: json|yaml|wide|name|custom-columns=...|custom-columns-file=...|go-template=...|go-template-file=...|jsonpath=...|jsonpath-file=... See custom columns [http://kubernetes.io/docs/user-guide/kubectl-overview/#custom-columns], golang template [http://golang.org/pkg/text/template/#pkg-overview] and jsonpath template [http://kubernetes.io/docs/user-guide/jsonpath].")
	// manually add remaining printer flags (that were not added through HumanPrintFlags#AddFlags) - for testing, while remaining printer PRs merge
	cmd.Flags().String("template", "", "Template string or path to template file to use when -o=go-template, -o=go-template-file. The template format is golang templates [http://golang.org/pkg/text/template/#pkg-overview].")
	cmd.MarkFlagFilename("template")
	cmd.Flags().BoolP("show-all", "a", true, "When printing, show all resources (default show all pods including terminated one.)")
	cmd.Flags().Bool("allow-missing-template-keys", true, "If true, ignore any errors in templates when a field or map key is missing in the template. Only applies to golang and jsonpath output formats.")
	cmd.Flags().MarkDeprecated("show-all", "will be removed in an upcoming release")

	// bind other external flags
	cmdutil.AddIncludeUninitializedFlag(cmd)
	addOpenAPIPrintColumnFlags(cmd)
	addServerPrintColumnFlags(cmd)
	cmd.Flags().BoolVar(&options.Export, "export", options.Export, "If true, use 'export' for the resources.  Exported resources are stripped of cluster-specific information.")
	cmdutil.AddFilenameOptionFlags(cmd, &options.FilenameOptions, "identifying the resource to get from a server.")
	cmdutil.AddInclude3rdPartyFlags(cmd)
	return cmd
}

// Complete takes the command arguments and factory and infers any remaining options.
func (options *GetOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(options.Raw) > 0 {
		if len(args) > 0 {
			return fmt.Errorf("arguments may not be passed when --raw is specified")
		}
		return nil
	}

	options.ServerPrint = cmdutil.GetFlagBool(cmd, useServerPrintColumns)

	var err error
	options.Namespace, options.ExplicitNamespace, err = f.DefaultNamespace()
	if err != nil {
		return err
	}
	if options.AllNamespaces {
		options.ExplicitNamespace = false
	}

	options.IncludeUninitialized = cmdutil.ShouldIncludeUninitialized(cmd, false)

	switch {
	case options.Watch || options.WatchOnly:
		// include uninitialized objects when watching on a single object
		// unless explicitly set --include-uninitialized=false
		options.IncludeUninitialized = cmdutil.ShouldIncludeUninitialized(cmd, len(args) == 2)
	default:
		if len(args) == 0 && cmdutil.IsFilenameSliceEmpty(options.Filenames) {
			fmt.Fprint(options.ErrOut, "You must specify the type of resource to get. ", cmdutil.ValidResourceTypeList(f))
			fullCmdName := cmd.Parent().CommandPath()
			usageString := "Required resource not specified."
			if len(fullCmdName) > 0 && cmdutil.IsSiblingCommandExists(cmd, "explain") {
				usageString = fmt.Sprintf("%s\nUse \"%s explain <resource>\" for a detailed description of that resource (e.g. %[2]s explain pods).", usageString, fullCmdName)
			}

			return cmdutil.UsageErrorf(cmd, usageString)
		}
	}

	// TODO(juanvallejo): this needs cleanup. Fields for a nested PrintFlags struct should not be set here
	options.HumanPrintFlags.WithNamespace = options.AllNamespaces
	options.HumanPrintFlags.AbsoluteTimestamps = options.Watch

	printer, matches, err := options.HumanPrintFlags.ToPrinter(options.OutputFormat)
	if !matches {
		glog.V(2).Infof("Couldn't match printer for the following flags %#v", options.HumanPrintFlags)
		printer, err = cmdutil.PrinterForOptions(cmdutil.ExtractCmdPrintOptions(cmd, options.AllNamespaces))
	}
	if err != nil {
		return err
	}

	options.ShowKind = options.ShowKind || resource.MultipleTypesRequested(args)
	options.GenericPrinter = printer.IsGeneric()

	options.PrintObj = func(obj runtime.Object, info *resource.Info, out io.Writer) error {
		// handle human-readable printing
		if humanPrinter, ok := printer.(*printers.HumanReadablePrinter); ok && info != nil {
			if options.ShowKind {
				kind := "none"
				if info.Mapping != nil {
					kind = info.Mapping.Resource
				}
				if alias, ok := kubectl.ResourceShortFormFor(info.Mapping.Resource); ok {
					kind = alias
				}
				humanPrinter.EnsurePrintWithKind(kind)
			}
			return humanPrinter.PrintObj(info.AsInternal(), out)
		}

		return printer.PrintObj(obj, out)
	}

	sorting, err := cmd.Flags().GetString("sort-by")
	if err != nil {
		return err
	}

	options.SortBy = sorting
	return nil
}

// Validate checks the set of flags provided by the user.
func (options *GetOptions) Validate(cmd *cobra.Command) error {
	if len(options.Raw) > 0 {
		if options.Watch || options.WatchOnly || len(options.LabelSelector) > 0 || options.Export {
			return fmt.Errorf("--raw may not be specified with other flags that filter the server request or alter the output")
		}
		if len(cmdutil.GetFlagString(cmd, "output")) > 0 {
			return cmdutil.UsageErrorf(cmd, "--raw and --output are mutually exclusive")
		}
		if _, err := url.ParseRequestURI(options.Raw); err != nil {
			return cmdutil.UsageErrorf(cmd, "--raw must be a valid URL path: %v", err)
		}
	}
	if cmdutil.GetFlagBool(cmd, "show-labels") {
		outputOption := cmd.Flags().Lookup("output").Value.String()
		if outputOption != "" && outputOption != "wide" {
			return fmt.Errorf("--show-labels option cannot be used with %s printer", outputOption)
		}
	}
	return nil
}

// Run performs the get operation.
// TODO: remove the need to pass these arguments, like other commands.
func (options *GetOptions) Run(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(options.Raw) > 0 {
		return options.raw(f)
	}
	if options.Watch || options.WatchOnly {
		return options.watch(f, cmd, args)
	}

	r := f.NewBuilder().
		Unstructured().
		NamespaceParam(options.Namespace).DefaultNamespace().AllNamespaces(options.AllNamespaces).
		FilenameParam(options.ExplicitNamespace, &options.FilenameOptions).
		LabelSelectorParam(options.LabelSelector).
		FieldSelectorParam(options.FieldSelector).
		ExportParam(options.Export).
		RequestChunksOf(options.ChunkSize).
		IncludeUninitialized(options.IncludeUninitialized).
		ResourceTypeOrNameArgs(true, args...).
		ContinueOnError().
		Latest().
		Flatten().
		TransformRequests(func(req *rest.Request) {
			if options.ServerPrint && !options.GenericPrinter && len(options.SortBy) == 0 {
				req.SetHeader("Accept", fmt.Sprintf("application/json;as=Table;v=%s;g=%s, application/json",
					metav1beta1.SchemeGroupVersion.Version, metav1beta1.GroupName))
			}
		}).
		Do()

	if options.IgnoreNotFound {
		r.IgnoreErrors(kapierrors.IsNotFound)
	}
	if err := r.Err(); err != nil {
		return err
	}

	if options.GenericPrinter {
		return options.printGeneric(r, cmd)
	}

	infos, err := r.Infos()
	if err != nil {
		return err
	}

	objs := make([]runtime.Object, len(infos))
	for ix := range infos {
		if options.ServerPrint {
			if table, err := options.decodeIntoTable(cmdutil.InternalVersionJSONEncoder(), infos[ix].Object); err == nil {
				infos[ix].Object = table
			} else {
				// if we are unable to decode server response into a v1beta1.Table,
				// fallback to client-side printing with whatever info the server returned.
				glog.V(2).Infof("Unable to decode server response into a Table. Falling back to hardcoded types: %v", err)
			}
		}

		objs[ix] = infos[ix].Object
	}

	// sort all objects
	var sorter *kubectl.RuntimeSort
	if len(options.SortBy) > 0 && len(objs) > 1 {
		// TODO: questionable
		if sorter, err = kubectl.SortObjects(cmdutil.InternalVersionDecoder(), objs, options.SortBy); err != nil {
			return err
		}
		// TODO: sorting for tabled/server-print output
	}

	out := printers.GetNewTabWriter(options.Out)
	allErrs := []error{}
	nonEmptyObjCount := 0
	for ix := range objs {
		info := infos[ix]
		if sorter != nil {
			info = infos[sorter.OriginalPosition(ix)]
		}

		// if dealing with a table that has no rows, skip remaining steps
		// and avoid printing an unnecessary newline
		if table, isTable := info.Object.(*metav1beta1.Table); isTable {
			if len(table.Rows) == 0 {
				continue
			}
		}
		out.Flush()

		nonEmptyObjCount++
		if err := options.PrintObj(info.Object, info, out); err != nil {
			allErrs = append(allErrs, err)
			continue
		}
	}
	out.Flush()

	if nonEmptyObjCount == 0 && !options.IgnoreNotFound {
		fmt.Fprintln(options.ErrOut, "No resources found.")
	}
	return utilerrors.NewAggregate(allErrs)
}

// raw makes a simple HTTP request to the provided path on the server using the default
// credentials.
func (options *GetOptions) raw(f cmdutil.Factory) error {
	restClient, err := f.RESTClient()
	if err != nil {
		return err
	}

	stream, err := restClient.Get().RequestURI(options.Raw).Stream()
	if err != nil {
		return err
	}
	defer stream.Close()

	_, err = io.Copy(options.Out, stream)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

// watch starts a client-side watch of one or more resources.
// TODO: remove the need for arguments here.
func (options *GetOptions) watch(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	r := f.NewBuilder().
		Unstructured().
		NamespaceParam(options.Namespace).DefaultNamespace().AllNamespaces(options.AllNamespaces).
		FilenameParam(options.ExplicitNamespace, &options.FilenameOptions).
		LabelSelectorParam(options.LabelSelector).
		FieldSelectorParam(options.FieldSelector).
		ExportParam(options.Export).
		RequestChunksOf(options.ChunkSize).
		IncludeUninitialized(options.IncludeUninitialized).
		ResourceTypeOrNameArgs(true, args...).
		SingleResourceType().
		Latest().
		Do()
	if err := r.Err(); err != nil {
		return err
	}
	infos, err := r.Infos()
	if err != nil {
		return err
	}
	if len(infos) > 1 {
		gvk := infos[0].Mapping.GroupVersionKind
		uniqueGVKs := 1

		// If requesting a resource count greater than a request's --chunk-size,
		// we will end up making multiple requests to the server, with each
		// request producing its own "Info" object. Although overall we are
		// dealing with a single resource type, we will end up with multiple
		// infos returned by the builder. To handle this case, only fail if we
		// have at least one info with a different GVK than the others.
		for _, info := range infos {
			if info.Mapping.GroupVersionKind != gvk {
				uniqueGVKs++
			}
		}

		if uniqueGVKs > 1 {
			return i18n.Errorf("watch is only supported on individual resources and resource collections - %d resources were found", uniqueGVKs)
		}
	}

	info := infos[0]
	mapping := info.ResourceMapping()
	printOpts := cmdutil.ExtractCmdPrintOptions(cmd, options.AllNamespaces)
	printer, err := cmdutil.PrinterForOptions(printOpts)
	if err != nil {
		return err
	}
	obj, err := r.Object()
	if err != nil {
		return err
	}

	// watching from resourceVersion 0, starts the watch at ~now and
	// will return an initial watch event.  Starting form ~now, rather
	// the rv of the object will insure that we start the watch from
	// inside the watch window, which the rv of the object might not be.
	rv := "0"
	isList := meta.IsListType(obj)
	if isList {
		// the resourceVersion of list objects is ~now but won't return
		// an initial watch event
		rv, err = mapping.MetadataAccessor.ResourceVersion(obj)
		if err != nil {
			return err
		}
	}

	// print the current object
	if !options.WatchOnly {
		var objsToPrint []runtime.Object
		writer := printers.GetNewTabWriter(options.Out)

		if isList {
			objsToPrint, _ = meta.ExtractList(obj)
		} else {
			objsToPrint = append(objsToPrint, obj)
		}
		for _, objToPrint := range objsToPrint {
			// printing always takes the internal version, but the watch event uses externals
			// TODO fix printing to use server-side or be version agnostic
			internalGV := mapping.GroupVersionKind.GroupKind().WithVersion(runtime.APIVersionInternal).GroupVersion()
			if err := printer.PrintObj(attemptToConvertToInternal(objToPrint, mapping, internalGV), writer); err != nil {
				return fmt.Errorf("unable to output the provided object: %v", err)
			}
		}
		writer.Flush()
	}

	// print watched changes
	w, err := r.Watch(rv)
	if err != nil {
		return err
	}

	first := true
	intr := interrupt.New(nil, w.Stop)
	intr.Run(func() error {
		_, err := watch.Until(0, w, func(e watch.Event) (bool, error) {
			if !isList && first {
				// drop the initial watch event in the single resource case
				first = false
				return false, nil
			}

			// printing always takes the internal version, but the watch event uses externals
			// TODO fix printing to use server-side or be version agnostic
			internalGV := mapping.GroupVersionKind.GroupKind().WithVersion(runtime.APIVersionInternal).GroupVersion()
			if err := printer.PrintObj(attemptToConvertToInternal(e.Object, mapping, internalGV), options.Out); err != nil {
				return false, err
			}
			return false, nil
		})
		return err
	})
	return nil
}

// attemptToConvertToInternal tries to convert to an internal type, but returns the original if it can't
func attemptToConvertToInternal(obj runtime.Object, converter runtime.ObjectConvertor, targetVersion schema.GroupVersion) runtime.Object {
	internalObject, err := converter.ConvertToVersion(obj, targetVersion)
	if err != nil {
		glog.V(1).Infof("Unable to convert %T to %v: err", obj, targetVersion, err)
		return obj
	}
	return internalObject
}

func (options *GetOptions) decodeIntoTable(encoder runtime.Encoder, obj runtime.Object) (runtime.Object, error) {
	if obj.GetObjectKind().GroupVersionKind().Kind != "Table" {
		return nil, fmt.Errorf("attempt to decode non-Table object into a v1beta1.Table")
	}

	b, err := runtime.Encode(encoder, obj)
	if err != nil {
		return nil, err
	}

	table := &metav1beta1.Table{}
	err = json.Unmarshal(b, table)
	if err != nil {
		return nil, err
	}

	return table, nil
}

func (options *GetOptions) printGeneric(r *resource.Result, cmd *cobra.Command) error {
	// TODO: This printer should be resolved via PrintFlags once
	// we have an all-encompassing PrintFlags struct.
	printer, err := cmdutil.PrinterForOptions(cmdutil.ExtractCmdPrintOptions(cmd, options.AllNamespaces))
	if err != nil {
		return err
	}

	// we flattened the data from the builder, so we have individual items, but now we'd like to either:
	// 1. if there is more than one item, combine them all into a single list
	// 2. if there is a single item and that item is a list, leave it as its specific list
	// 3. if there is a single item and it is not a list, leave it as a single item
	var errs []error
	singleItemImplied := false
	infos, err := r.IntoSingleItemImplied(&singleItemImplied).Infos()
	if err != nil {
		if singleItemImplied {
			return err
		}
		errs = append(errs, err)
	}

	if len(infos) == 0 && options.IgnoreNotFound {
		return utilerrors.Reduce(utilerrors.Flatten(utilerrors.NewAggregate(errs)))
	}

	var obj runtime.Object
	if !singleItemImplied || len(infos) > 1 {
		// we have more than one item, so coerce all items into a list.
		// we don't want an *unstructured.Unstructured list yet, as we
		// may be dealing with non-unstructured objects. Compose all items
		// into an api.List, and then decode using an unstructured scheme.
		list := api.List{
			TypeMeta: metav1.TypeMeta{
				Kind:       "List",
				APIVersion: "v1",
			},
			ListMeta: metav1.ListMeta{},
		}
		for _, info := range infos {
			list.Items = append(list.Items, info.Object)
		}

		listData, err := json.Marshal(list)
		if err != nil {
			return err
		}

		converted, err := runtime.Decode(unstructured.UnstructuredJSONScheme, listData)
		if err != nil {
			return err
		}

		obj = converted
	} else {
		obj = infos[0].Object
	}

	isList := meta.IsListType(obj)
	if isList {
		items, err := meta.ExtractList(obj)
		if err != nil {
			return err
		}

		// take the items and create a new list for display
		list := &unstructured.UnstructuredList{
			Object: map[string]interface{}{
				"kind":       "List",
				"apiVersion": "v1",
				"metadata":   map[string]interface{}{},
			},
		}
		if listMeta, err := meta.ListAccessor(obj); err == nil {
			list.Object["metadata"] = map[string]interface{}{
				"selfLink":        listMeta.GetSelfLink(),
				"resourceVersion": listMeta.GetResourceVersion(),
			}
		}

		for _, item := range items {
			list.Items = append(list.Items, *item.(*unstructured.Unstructured))
		}
		if err := printer.PrintObj(list, options.Out); err != nil {
			errs = append(errs, err)
		}
		return utilerrors.Reduce(utilerrors.Flatten(utilerrors.NewAggregate(errs)))
	}

	if printErr := printer.PrintObj(obj, options.Out); printErr != nil {
		errs = append(errs, printErr)
	}

	return utilerrors.Reduce(utilerrors.Flatten(utilerrors.NewAggregate(errs)))
}

func addOpenAPIPrintColumnFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(useOpenAPIPrintColumnFlagLabel, true, "If true, use x-kubernetes-print-column metadata (if present) from the OpenAPI schema for displaying a resource.")
}

func addServerPrintColumnFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(useServerPrintColumns, false, "If true, have the server return the appropriate table output. Supports extension APIs and CRD. Experimental.")
}

func shouldGetNewPrinterForMapping(printer printers.ResourcePrinter, lastMapping, mapping *meta.RESTMapping) bool {
	return printer == nil || lastMapping == nil || mapping == nil || mapping.Resource != lastMapping.Resource
}

func cmdSpecifiesOutputFmt(cmd *cobra.Command) bool {
	return cmdutil.GetFlagString(cmd, "output") != ""
}

// outputOptsForMappingFromOpenAPI looks for the output format metatadata in the
// openapi schema and modifies the passed print options for the mapping if found.
func updatePrintOptionsForOpenAPI(f cmdutil.Factory, mapping *meta.RESTMapping, printOpts *printers.PrintOptions) bool {

	// user has not specified any output format, check if OpenAPI has
	// default specification to print this resource type
	api, err := f.OpenAPISchema()
	if err != nil {
		// Error getting schema
		return false
	}
	// Found openapi metadata for this resource
	schema := api.LookupResource(mapping.GroupVersionKind)
	if schema == nil {
		// Schema not found, return empty columns
		return false
	}

	columns, found := openapi.GetPrintColumns(schema.GetExtensions())
	if !found {
		// Extension not found, return empty columns
		return false
	}

	return outputOptsFromStr(columns, printOpts)
}

// outputOptsFromStr parses the print-column metadata and generates printer.OutputOptions object.
func outputOptsFromStr(columnStr string, printOpts *printers.PrintOptions) bool {
	if columnStr == "" {
		return false
	}
	parts := strings.SplitN(columnStr, "=", 2)
	if len(parts) < 2 {
		return false
	}

	printOpts.OutputFormatType = parts[0]
	printOpts.OutputFormatArgument = parts[1]
	printOpts.AllowMissingKeys = true

	return true
}
