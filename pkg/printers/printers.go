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
	"io/ioutil"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
)

var noPrinterMatchedErr = fmt.Errorf("unable to match a printer to handle current print options")

// GetStandardPrinter takes a format type, an optional format argument. It will return
// a printer or an error. The printer is agnostic to schema versions, so you must
// send arguments to PrintObj in the version you wish them to be shown using a
// VersionedPrinter (typically when generic is true).
func GetStandardPrinter(typer runtime.ObjectTyper, encoder runtime.Encoder, decoders []runtime.Decoder, options PrintOptions) (ResourcePrinter, error) {
	format, formatArgument, allowMissingTemplateKeys := options.OutputFormatType, options.OutputFormatArgument, options.AllowMissingKeys

	var printer ResourcePrinter
	switch format {

	case "json", "yaml":
		jsonYamlFlags := NewJSONYamlPrintFlags()
		p, matched, err := jsonYamlFlags.ToPrinter(format)
		if !matched {
			return nil, noPrinterMatchedErr
		}
		if err != nil {
			return nil, err
		}

		printer = p

	case "name":
		printer = &NamePrinter{
			Typer:    typer,
			Decoders: decoders,
		}

	case "templatefile", "go-template-file", "jsonpath-file":
		if len(formatArgument) == 0 {
			return nil, fmt.Errorf("%s format specified but no template file given", format)
		}
		data, err := ioutil.ReadFile(formatArgument)
		if err != nil {
			return nil, fmt.Errorf("error reading --template %s, %v\n", formatArgument, err)
		}

		formatArgument = string(data)
		fallthrough
	case "template", "go-template", "jsonpath":
		// TODO: construct and bind this separately (at the command level)

		kubeTemplateFlags := KubeTemplatePrintFlags{
			GoTemplatePrintFlags: &GoTemplatePrintFlags{
				AllowMissingKeys: &allowMissingTemplateKeys,
				TemplateArgument: &formatArgument,
			},
			JSONPathPrintFlags: &JSONPathPrintFlags{
				AllowMissingKeys: &allowMissingTemplateKeys,
				TemplateArgument: &formatArgument,
			},
		}

		kubeTemplatePrinter, matched, err := kubeTemplateFlags.ToPrinter(format)
		if !matched {
			return nil, noPrinterMatchedErr
		}
		if err != nil {
			return nil, err
		}

		printer = kubeTemplatePrinter

	case "custom-columns":
		var err error
		if printer, err = NewCustomColumnsPrinterFromSpec(formatArgument, decoders[0], options.NoHeaders); err != nil {
			return nil, err
		}

	case "custom-columns-file":
		file, err := os.Open(formatArgument)
		if err != nil {
			return nil, fmt.Errorf("error reading template %s, %v\n", formatArgument, err)
		}
		defer file.Close()
		if printer, err = NewCustomColumnsPrinterFromTemplate(file, decoders[0]); err != nil {
			return nil, err
		}

	case "wide":
		fallthrough
	case "":

		printer = NewHumanReadablePrinter(encoder, decoders[0], options)
	default:
		return nil, fmt.Errorf("output format %q not recognized", format)
	}
	return printer, nil
}
