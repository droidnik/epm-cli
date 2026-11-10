package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/droidnik/epm-cli/internal/apperr"
	"github.com/droidnik/epm-cli/internal/output"
	"github.com/droidnik/epm-cli/internal/spec"
)

// Resource groups for `--help`.
var resourceGroups = []struct {
	ID, Title string
	Names     []string
}{
	{"endpoints", "Endpoints:", []string{"computers", "groups", "tasks"}},
	{"policies", "Policies:", []string{"policies", "policy-editor"}},
	{"requests", "Requests & Events:", []string{"admin-access-requests", "authorization-requests",
		"authorization-request-audits", "events", "activity-audits"}},
	{"access", "Users & Access:", []string{"users", "roles", "global-roles", "api-accounts", "accepted-domains",
		"authentication-provider"}},
	{"scim", "SCIM:", []string{"scim-users", "scim-roles", "scim-schemas", "scim-resource-types",
		"scim-service-provider-config"}},
	{"tenant", "Tenant:", []string{"about", "api-definition", "app-switcher"}},
}

func NewRootCmd(a *App) *cobra.Command {
	root := &cobra.Command{
		Use:   "epm-cli",
		Short: "Command-line interface for BeyondTrust EPM for Windows and Mac",
		Long: `epm-cli is a command-line interface for BeyondTrust Endpoint Privilege Management
for Windows and Mac (PM Cloud Management API v3).

Get started:
  epm-cli config add prod        # tenant URL, client ID and secret (stored in the Keychain)
  epm-cli doctor                 # verify connectivity and credentials
  epm-cli computers list --all   # every computer, all pages

Set EPM_CLI_ARGS to prepend default flags to every invocation, e.g.
  export EPM_CLI_ARGS='--profile sandbox -o json'
For CI, set EPM_URL, EPM_CLIENT_ID and EPM_CLIENT_SECRET instead of a profile.`,
		Version:       Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return a.validateGlobals(cmd)
		},
	}
	root.SetVersionTemplate("epm-cli {{.Version}}\n")
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return apperr.Usagef("%v (see '%s --help')", err, c.CommandPath())
	})

	pf := root.PersistentFlags()
	pf.StringVarP(&a.Profile, "profile", "p", "", "config profile to use (or EPM_PROFILE)")
	pf.StringVarP(&a.Output, "output", "o", "", "output format: "+strings.Join(output.Formats, ", ")+" (default table)")
	pf.StringVar(&a.Field, "field", "", "extract one (dotted) field from each result, e.g. --field id")
	pf.StringVar(&a.Columns, "columns", "", "table/csv/plain: comma-separated columns (dotted paths allowed)")
	pf.BoolVar(&a.Wide, "wide", false, "table: show all columns, don't truncate")
	pf.StringVar(&a.OutFile, "out-file", "", "write output to a file instead of stdout")
	pf.BoolVarP(&a.DryRun, "dry-run", "n", false, "print the request that would be sent; send nothing")
	pf.BoolVarP(&a.Yes, "yes", "y", false, "confirm destructive operations without prompting")
	pf.BoolVar(&a.NoInput, "no-input", false, "never prompt; fail if input would be required")
	pf.BoolVar(&a.NoColor, "no-color", false, "disable colored output")
	pf.BoolVarP(&a.Quiet, "quiet", "q", false, "suppress hints and progress on stderr")
	pf.BoolVarP(&a.Verbose, "verbose", "v", false, "log HTTP requests to stderr")
	_ = root.RegisterFlagCompletionFunc("output", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return output.Formats, cobra.ShellCompDirectiveNoFileComp
	})
	_ = root.RegisterFlagCompletionFunc("profile", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		cfg, err := a.Config()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return cfg.Names(), cobra.ShellCompDirectiveNoFileComp
	})

	root.AddGroup(&cobra.Group{ID: "core", Title: "Core Commands:"})
	for _, g := range resourceGroups {
		root.AddGroup(&cobra.Group{ID: g.ID, Title: g.Title})
	}
	root.AddGroup(&cobra.Group{ID: "other", Title: "Other API Resources:"})
	root.SetHelpCommandGroupID("core")
	root.SetCompletionCommandGroupID("core")

	addBuiltins(root, a)

	groupOf := map[string]string{}
	for _, g := range resourceGroups {
		for _, n := range g.Names {
			groupOf[n] = g.ID
		}
	}
	for _, res := range spec.Load().Resources {
		rc := resourceCmd(a, res)
		rc.GroupID = groupOf[res.Name]
		if rc.GroupID == "" {
			rc.GroupID = "other"
		}
		root.AddCommand(rc)
	}
	return root
}

// usageArgs validates the exact positional arguments of an operation.
func usageArgs(names []string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == len(names) {
			return nil
		}
		if len(names) == 0 {
			return apperr.Usagef("%s takes no arguments, got %q", cmd.CommandPath(), args)
		}
		return apperr.Usagef("%s requires %d argument(s): %s", cmd.CommandPath(), len(names),
			strings.ToUpper(strings.Join(names, " ")))
	}
}

// parentRunE makes resource/group commands fail on unknown subcommands instead of
// silently printing help with exit code 0.
func parentRunE(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	msg := fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath())
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		msg += "; did you mean " + strings.Join(s, ", ") + "?"
	}
	return apperr.Usagef("%s", msg)
}
