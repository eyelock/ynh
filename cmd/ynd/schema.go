package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed schema/plugin.schema.json
var pluginSchemaData []byte

//go:embed schema/marketplace.schema.json
var marketplaceSchemaData []byte

const (
	pluginSchemaID      = "https://eyelock.github.io/ynh/schema/plugin.schema.json"
	marketplaceSchemaID = "https://eyelock.github.io/ynh/schema/marketplace.schema.json"
)

var (
	compiledPluginSchema      *jsonschema.Schema
	compiledMarketplaceSchema *jsonschema.Schema
	schemaPrinter             = message.NewPrinter(language.English)
)

func init() {
	c := jsonschema.NewCompiler()

	pluginDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(pluginSchemaData))
	if err != nil {
		panic(fmt.Sprintf("parsing plugin schema: %v", err))
	}
	marketplaceDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(marketplaceSchemaData))
	if err != nil {
		panic(fmt.Sprintf("parsing marketplace schema: %v", err))
	}
	if err := c.AddResource(pluginSchemaID, pluginDoc); err != nil {
		panic(fmt.Sprintf("loading plugin schema: %v", err))
	}
	if err := c.AddResource(marketplaceSchemaID, marketplaceDoc); err != nil {
		panic(fmt.Sprintf("loading marketplace schema: %v", err))
	}
	if compiledPluginSchema, err = c.Compile(pluginSchemaID); err != nil {
		panic(fmt.Sprintf("compiling plugin schema: %v", err))
	}
	if compiledMarketplaceSchema, err = c.Compile(marketplaceSchemaID); err != nil {
		panic(fmt.Sprintf("compiling marketplace schema: %v", err))
	}
}

// schemaIssues validates data against schema and returns lint issues.
func schemaIssues(file string, data []byte, schema *jsonschema.Schema) []lintIssue {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return []lintIssue{{File: file, Message: fmt.Sprintf("invalid JSON: %v", err)}}
	}
	if err := schema.Validate(v); err != nil {
		return collectSchemaErrors(file, err)
	}
	return nil
}

// collectSchemaErrors walks a jsonschema validation error tree and returns flat lint issues.
func collectSchemaErrors(file string, err error) []lintIssue {
	ve, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return []lintIssue{{File: file, Message: err.Error()}}
	}
	var issues []lintIssue
	walkSchemaError(file, ve, &issues)
	return issues
}

func walkSchemaError(file string, ve *jsonschema.ValidationError, out *[]lintIssue) {
	if len(ve.Causes) == 0 {
		loc := strings.Join(ve.InstanceLocation, "/")
		msg := ve.ErrorKind.LocalizedString(schemaPrinter)
		if loc != "" {
			msg = loc + ": " + msg
		}
		*out = append(*out, lintIssue{File: file, Message: msg})
		return
	}
	for _, cause := range ve.Causes {
		walkSchemaError(file, cause, out)
	}
}
