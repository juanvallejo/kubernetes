/*
Copyright 2017 The Kubernetes Authors.

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

package printers

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
)

// ResourcePrinter is an interface that knows how to print runtime objects.
type ResourcePrinter interface {
	// Print receives a runtime object, formats it and prints it to a writer.
	PrintObj(runtime.Object, io.Writer) error
	HandledResources() []string
	//Can be used to print out warning/clarifications if needed
	//after all objects were printed
	AfterPrint(io.Writer, string) error
	// Identify if it is a generic printer
	IsGeneric() bool
}

// ResourcePrinterFunc is a function that can print objects
type ResourcePrinterFunc func(runtime.Object, io.Writer) error

// PrintObj implements ResourcePrinter
func (fn ResourcePrinterFunc) PrintObj(obj runtime.Object, w io.Writer) error {
	return fn(obj, w)
}

// TODO: implement HandledResources()
func (fn ResourcePrinterFunc) HandledResources() []string {
	return []string{}
}

func (fn ResourcePrinterFunc) AfterPrint(io.Writer, string) error {
	return nil
}

func (fn ResourcePrinterFunc) IsGeneric() bool {
	return true
}

type PrintOptions struct {
	// supported Format types can be found in pkg/printers/printers.go
	OutputFormatType string

	NoHeaders          bool
	WithNamespace      bool
	WithKind           bool
	Wide               bool
	ShowAll            bool
	ShowLabels         bool
	AbsoluteTimestamps bool
	Kind               string
	ColumnLabels       []string

	SortBy string
}

// separate template flags
type TemplatePrintFlags struct {
	// indicates if it is OK to ignore missing keys for rendering
	// an output template.
	AllowMissingKeys *bool
	TemplateArgument *string

	// not actually bound to a flag - this may be set by
	// a more general flags struct that composes this one,
	// or its value may be provided via an arg to ToPrinter.
	// We need this to select the appropriate PrintBuilder.
	OutputFormat string

	PrinterBuilders []func(*TemplatePrintFlags) (ResourcePrinter, bool, error)
}

func (f *TemplatePrintFlags) Validate() error {
	if f.TemplateArgument == nil || len(*f.TemplateArgument) == 0 {
		return fmt.Errorf("missing --template value")
	}
	return nil
}

func (f *TemplatePrintFlags) ToPrinter(outputFormat string) (ResourcePrinter, error) {
	f.OutputFormat = outputFormat

	// templates are logically optional for specifying a format.
	if len(f.OutputFormat) == 0 && f.TemplateArgument != nil && len(*f.TemplateArgument) != 0 {
		f.OutputFormat = "template"
	}

	templateFormats := []string{
		"go-template=", "go-template-file=", "jsonpath=", "jsonpath-file=", "custom-columns=", "custom-columns-file=",
	}

	for _, format := range templateFormats {
		if strings.HasPrefix(f.OutputFormat, format) {
			templateFile := outputFormat[len(format):]
			f.TemplateArgument = &templateFile
			f.OutputFormat = format[:len(format)-1]
			break
		}
	}

	// iterate through all o.PrinterBuilders here.
	// On the first PrinterBuilder that matches, use the
	// ResourcePrinter that it returns.
	for _, builder := range f.PrinterBuilders {
		p, match, err := builder(f)
		if !match {
			continue
		}
		if err != nil {
			return nil, err
		}

		return p, nil
	}

	return nil, fmt.Errorf("no printers matched current flag values")
}

// AddFlags receives a *cobra.Command reference and binds
// flags related to template printing to it
func (f *TemplatePrintFlags) AddFlags(c *cobra.Command) {
	if f.TemplateArgument != nil {
		c.Flags().StringVar(f.TemplateArgument, "template", *f.TemplateArgument, "Template string or path to template file to use when -o=go-template, -o=go-template-file. The template format is golang templates [http://golang.org/pkg/text/template/#pkg-overview].")
		c.MarkFlagFilename("template")
	}
	if f.AllowMissingKeys != nil {
		c.Flags().BoolVar(f.AllowMissingKeys, "allow-missing-template-keys", *f.AllowMissingKeys, "If true, ignore any errors in templates when a field or map key is missing in the template. Only applies to golang and jsonpath output formats.")
	}
}

// NewTemplatePrintFlags returns flags associated with
// --template printing, with default values set.
func NewTemplatePrintFlags() *TemplatePrintFlags {
	allowMissingKeysPtr := true
	templateArgPtr := ""

	return &TemplatePrintFlags{
		TemplateArgument: &templateArgPtr,
		AllowMissingKeys: &allowMissingKeysPtr,

		PrinterBuilders: []func(*TemplatePrintFlags) (ResourcePrinter, bool, error){
			NewTemplatePrinter,
			NewJSONPathPrinter,
		},
	}
}

// Describer generates output for the named resource or an error
// if the output could not be generated. Implementers typically
// abstract the retrieval of the named object from a remote server.
type Describer interface {
	Describe(namespace, name string, describerSettings DescriberSettings) (output string, err error)
}

// DescriberSettings holds display configuration for each object
// describer to control what is printed.
type DescriberSettings struct {
	ShowEvents bool
}

// ObjectDescriber is an interface for displaying arbitrary objects with extra
// information. Use when an object is in hand (on disk, or already retrieved).
// Implementers may ignore the additional information passed on extra, or use it
// by default. ObjectDescribers may return ErrNoDescriber if no suitable describer
// is found.
type ObjectDescriber interface {
	DescribeObject(object interface{}, extra ...interface{}) (output string, err error)
}

// ErrNoDescriber is a structured error indicating the provided object or objects
// cannot be described.
type ErrNoDescriber struct {
	Types []string
}

// Error implements the error interface.
func (e ErrNoDescriber) Error() string {
	return fmt.Sprintf("no describer has been defined for %v", e.Types)
}
