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

	"github.com/spf13/cobra"

	"k8s.io/apimachinery/pkg/runtime"
)

// HumanPrintFlags provides default flags necessary for template printing.
// Given the following flag values, a printer can be requested that knows
// how to handle printing based on these values.
type HumanPrintFlags struct {
	NoHeaders     *bool
	WithNamespace *bool
	Wide          *bool

	// TODO: this is deprecated - remove in next version
	ShowAll *bool

	ShowLabels         *bool
	AbsoluteTimestamps *bool
	ColumnLabels       *[]string
	SortBy             *string

	Kind string
}

// ToPrinter receives an outputFormat and returns a printer capable of
// handling human-readable output.
func (f *HumanPrintFlags) ToPrinter(outputFormat string, encoder runtime.Encoder, decoder runtime.Decoder) (ResourcePrinter, bool, error) {
	if len(outputFormat) > 0 && outputFormat != "wide" {
		return nil, false, nil
	}

	if encoder == nil || decoder == nil {
		return nil, false, fmt.Errorf("both an encoder and decoder must be specified for this printer")
	}

	p := NewHumanReadablePrinter(encoder, decoder)

	if f.NoHeaders != nil {
		noHeaders := *f.NoHeaders
		if !noHeaders {
			p.EnsurePrintHeaders()
		}
	}
	if f.WithNamespace != nil && *f.WithNamespace {
		p.EnsurePrintNamespace()
	}
	if f.ShowLabels != nil && *f.ShowLabels {
		p.EnsurePrintLabels()
	}
	if f.AbsoluteTimestamps != nil && *f.AbsoluteTimestamps {
		p.EnsureAbsoluteTimestamps()
	}
	if f.Wide != nil && *f.Wide {
		p.EnsureWideOutput()
	}

	if len(f.Kind) > 0 {
		p.EnsurePrintWithKind(f.Kind)
	}
	if f.ColumnLabels != nil {
		columnLabels := *f.ColumnLabels
		if len(columnLabels) > 0 {
			p.EnsurePrintWithColumnLabels(columnLabels)
		}
	}

	// handle sorting
	if f.SortBy != nil {
		sortBy := *f.SortBy
		if len(sortBy) > 0 {
			// TODO: handle sorting. importing "kubectl.SortingPrinter"
			// causes an import cycle. Sorting also needs to be redone.
		}
	}

	return p, true, nil
}

// AddFlags receives a *cobra.Command reference and binds
// flags related to human-readable printing to it
func (f *HumanPrintFlags) AddFlags(c *cobra.Command) {
	// TODO: bind flags to non-nil defaulted fields
}

// NewHumanPrintFlags returns flags associated with
// human-readable printing, with default values set.
func NewHumanPrintFlags() *HumanPrintFlags {
	return &HumanPrintFlags{}
}
