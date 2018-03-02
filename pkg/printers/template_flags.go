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

package printers

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// TemplatePrintFlags provides default flags necessary for template printing.
// Given the following flag values, a printer can be requested that knows
// how to handle printing based on these values.
type TemplatePrintFlags struct {
	// indicates if it is OK to ignore missing keys for rendering
	// an output template.
	AllowMissingKeys *bool
	TemplateArgument *string
}

func (f *TemplatePrintFlags) Validate() error {
	if f.TemplateArgument == nil || len(*f.TemplateArgument) == 0 {
		return fmt.Errorf("missing --template value")
	}
	return nil
}

// ToPrinter receives an outputFormat and returns a printer capable of
// handling --template format printing.
// Returns false if the specified outputFormat does not match a template format.
func (f *TemplatePrintFlags) ToPrinter(outputFormat string) (ResourcePrinter, bool, error) {
	if f.TemplateArgument == nil {
		return nil, true, fmt.Errorf("missing --template argument")
	}
	templateArg := *f.TemplateArgument

	// if no output format is explicitly given via --output
	// but a template value is provided via --template=...
	// default --output format to go-template.
	if len(outputFormat) == 0 && len(templateArg) > 0 {
		outputFormat = "go-template"
	}

	// templates are logically optional for specifying a format.
	// this allows a user to specify a template format value
	// as --output=go-template=
	templateFormats := map[string]bool{
		"go-template":      true,
		"go-template-file": true,
		"jsonpath":         true,
		"jsonpath-file":    true,
	}

	for format := range templateFormats {
		f := format + "="
		if strings.HasPrefix(outputFormat, f) {
			templateArg = outputFormat[len(f):]
			outputFormat = f[:len(f)-1]
		}
	}

	if _, supportedFormat := templateFormats[outputFormat]; !supportedFormat && outputFormat != "template" {
		return nil, false, nil
	}

	p, err := NewTemplatePrinter([]byte(templateArg))
	if err != nil {
		return nil, true, err
	}

	allowMissingKeys := true
	if f.AllowMissingKeys != nil {
		allowMissingKeys = *f.AllowMissingKeys
	}

	p.AllowMissingKeys(allowMissingKeys)
	return p, true, nil
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
	}
}
