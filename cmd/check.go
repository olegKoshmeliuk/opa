// Copyright 2017 The OPA Authors.  All rights reserved.
// Use of this source code is governed by an Apache2
// license that can be found in the LICENSE file.

package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"

	"github.com/spf13/cobra"

	"github.com/open-policy-agent/opa/cmd/internal/env"
	pr "github.com/open-policy-agent/opa/internal/presentation"
	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/loader"
	"github.com/open-policy-agent/opa/v1/util"
)

type checkParams struct {
	format       *util.EnumFlag
	errLimit     int
	ignore       []string
	bundleMode   bool
	capabilities *capabilitiesFlag
	schema       *schemaFlags
	strict       bool
	regoV1       bool
	v0Compatible bool
	v1Compatible bool
}

func newCheckParams() checkParams {
	return checkParams{
		format: util.NewEnumFlag(checkFormatPretty, []string{
			checkFormatPretty, checkFormatJSON,
		}),
		capabilities: newcapabilitiesFlag(),
		schema:       &schemaFlags{},
	}
}

func (p *checkParams) regoVersion() ast.RegoVersion {
	// The '--rego-v1' flag takes precedence over the '--v1-compatible' flag.
	if p.regoV1 {
		return ast.RegoV0CompatV1
	}
	// The '--v0-compatible' flag takes precedence over the '--v1-compatible' flag.
	if p.v0Compatible {
		return ast.RegoV0
	}
	if p.v1Compatible {
		return ast.RegoV1
	}
	return ast.DefaultRegoVersion
}

const (
	checkFormatPretty = "pretty"
	checkFormatJSON   = "json"
)

func checkModules(params checkParams, args []string) error {

	modules := map[string]*ast.Module{}

	var capabilities *ast.Capabilities
	// if capabilities are not provided as a cmd flag,
	// then ast.CapabilitiesForThisVersion must be called
	// within checkModules to ensure custom builtins are properly captured
	if params.capabilities.C != nil {
		capabilities = params.capabilities.C
	} else {
		capabilities = ast.CapabilitiesForThisVersion(ast.CapabilitiesRegoVersion(params.regoVersion()))
	}

	ss, err := loader.Schemas(params.schema.path)
	if err != nil {
		return err
	}

	if params.bundleMode {
		for _, path := range args {
			b, err := loader.NewFileLoader().
				WithRegoVersion(params.regoVersion()).
				WithSkipBundleVerification(true).
				WithProcessAnnotation(true).
				WithCapabilities(capabilities).
				WithFilter(filterFromPaths(params.ignore)).
				AsBundle(path)
			if err != nil {
				return err
			}
			maps.Copy(modules, b.ParsedModules(path))
		}
	} else {
		f := loaderFilter{
			Ignore:   params.ignore,
			OnlyRego: true,
		}

		result, err := loader.NewFileLoader().
			WithRegoVersion(params.regoVersion()).
			WithProcessAnnotation(true).
			WithCapabilities(capabilities).
			Filtered(args, f.Apply)
		if err != nil {
			return err
		}

		for _, m := range result.Modules {
			modules[m.Name] = m.Parsed
		}
	}

	compiler := ast.NewCompiler().
		SetErrorLimit(params.errLimit).
		WithCapabilities(capabilities).
		WithSchemas(ss).
		WithEnablePrintStatements(true).
		WithStrict(params.strict).
		WithUseTypeCheckAnnotations(true)

	compiler.Compile(modules)
	if compiler.Failed() {
		return compiler.Errors
	}
	return nil
}

func filterFromPaths(paths []string) loader.Filter {
	return func(abspath string, info fs.FileInfo, depth int) bool {
		return loaderFilter{Ignore: paths}.Apply(abspath, info, depth)
	}
}

func outputErrors(format string, err error) {
	var out io.Writer
	if err != nil {
		out = os.Stderr
	} else {
		out = os.Stdout
	}

	switch format {
	case checkFormatJSON:
		result := pr.Output{
			Errors: pr.NewOutputErrors(err),
		}
		err := pr.JSON(out, result)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
		}
	default:
		fmt.Fprintln(out, err)
	}
}

func init() {
	checkParams := newCheckParams()

	checkCommand := &cobra.Command{
		Use:   "check <path> [path [...]]",
		Short: "Check Rego source files",
		Long: `Check Rego source files for parse and compilation errors.
	
If the 'check' command succeeds in parsing and compiling the source file(s), no output
is produced. If the parsing or compiling fails, 'check' will output the errors
and exit with a non-zero exit code.`,

		PreRunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("specify at least one file")
			}
			return env.CmdFlags.CheckEnvironmentVariables(cmd)
		},

		Run: func(_ *cobra.Command, args []string) {
			if err := checkModules(checkParams, args); err != nil {
				outputErrors(checkParams.format.String(), err)
				os.Exit(1)
			}
		},
	}

	addMaxErrorsFlag(checkCommand.Flags(), &checkParams.errLimit)
	addIgnoreFlag(checkCommand.Flags(), &checkParams.ignore)
	checkCommand.Flags().VarP(checkParams.format, "format", "f", "set output format")
	addBundleModeFlag(checkCommand.Flags(), &checkParams.bundleMode, false)
	addCapabilitiesFlag(checkCommand.Flags(), checkParams.capabilities)
	addSchemaFlags(checkCommand.Flags(), checkParams.schema)
	addStrictFlag(checkCommand.Flags(), &checkParams.strict, false)
	addRegoV0V1FlagWithDescription(checkCommand.Flags(), &checkParams.regoV1, false,
		"check for Rego v0 and v1 compatibility (policies must be compatible with both Rego versions)")
	addV0CompatibleFlag(checkCommand.Flags(), &checkParams.v0Compatible, false)
	addV1CompatibleFlag(checkCommand.Flags(), &checkParams.v1Compatible, false)
	RootCommand.AddCommand(checkCommand)
}
