// Package cli is pogo's command tree.
//
// pogo is one binary with two faces. Run bare in a terminal it opens the TUI
// over everything it has recorded; run with a request it wraps curl and writes
// down what happened. Everything else — listing, importing, environments,
// maintenance — is a subcommand, so the whole tool is scriptable and none of it
// hides behind the interactive mode (whis SYSTEM_DESIGN.md §10).
package cli

import (
	"context"
	"fmt"
	"image/color"
	"os"

	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/selfupdate"
	"github.com/rmpato/pogo/internal/ui"
	"github.com/rmpato/pogo/internal/version"
)

// Execute runs the command tree and returns the process exit code.
//
// The exit code matters more here than in most CLIs: when pogo is wrapping
// curl it must exit with curl's status, or every script that checks `$?` starts
// lying. `exitCode` carries that up from the curl path.
func Execute() int {
	// The detached update-check child. Not part of the user interface: pogo
	// spawns it so that checking for a release costs the user nothing.
	if len(os.Args) == 2 && os.Args[1] == selfupdate.RefreshFlag() {
		cfg, err := config.Load()
		if err != nil {
			return 1
		}
		return selfupdate.RefreshCache(cfg.Dir(), version.Version)
	}

	// A malformed config is worth saying out loud, but pogo still has to run:
	// the alternative is a tool that refuses to make a request because of a
	// typo in a preferences file.
	store, err := config.Open()
	if err != nil {
		ui.Warn(err.Error())
		store, _ = config.OpenAt(os.DevNull, config.Default())
	}
	ui.ApplyTheme(config.Normalize(store.Current().Theme, ui.Themes(), ui.ThemeDefault))

	app := &app{store: store}
	root := newRoot(app)

	// A leading `pogo <project> …` is settled here too, alongside the bare-URL
	// shorthand it composes with: `pogo checkout https://…` is both at once.
	// Peeling it off before anything else means every subcommand below —
	// curl, list, new, or nothing at all — just sees app.project already set.
	args := os.Args[1:]
	if name, rest, created := splitProject(args, app.cfg().APIs, reservedNames(root)); name != "" {
		if created {
			if err := app.store.Update(func(c *config.Config) { c.APIs.SetProject(name, "") }); err != nil {
				ui.Warn(err.Error())
			}
			fmt.Fprintf(os.Stderr, "pogo: created project %s\n", name)
		}
		app.project = name
		args = rest
	}

	// The bare-URL shorthand is settled before Cobra sees the command line, not
	// inside it. `pogo https://api.example.com -sS -o /dev/null` is a curl
	// command from its first argument onward, and Cobra would reject -sS as an
	// unknown shorthand of its own long before RunE could hand it over.
	//
	// Only a first argument that parses as a URL takes this path, so no
	// subcommand and no flag of pogo's can be swallowed by it.
	if len(args) > 0 && curlargs.LooksLikeURL(args[0]) {
		if err := app.runCurl(args); err != nil {
			if code, ok := err.(exitCode); ok {
				return int(code)
			}
			fmt.Fprintf(os.Stderr, "pogo: %v\n", err)
			return 2
		}
		return 0
	}

	root.SetArgs(args)
	if err := fang.Execute(context.Background(), root,
		fang.WithColorSchemeFunc(colorScheme),
		fang.WithVersion(version.Version),
	); err != nil {
		if code, ok := err.(exitCode); ok {
			return int(code)
		}
		return 1
	}
	return app.exit
}

// app is the state every subcommand shares: the config store, and the exit
// code a wrapped curl left behind.
type app struct {
	store *config.Store[config.Config]

	// project is the API this invocation was prefixed with (`pogo checkout
	// curl …`), when it was.
	project string

	exit int
}

// cfg is the configuration a command runs under: what the file says, with this
// invocation's POGO_* overrides on top. The store keeps the file's version, so
// saving a preference never writes an override back.
func (a *app) cfg() config.Config { return a.store.Current().WithEnv() }

// exitCode is an error that carries a process status. curl's exit codes are
// part of its interface, so pogo passes them straight through rather than
// flattening every failure to 1.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func newRoot(app *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "pogo",
		Short: "curl, but it remembers",
		Long: `pogo runs curl and writes down what happened, then gives you a terminal UI
over everything it has run: find a request, inspect it, replay it, change it
and run it again, compare two responses.

  pogo                          browse everything you have run
  pogo curl -sS https://…       make a request, exactly as curl would
  pogo https://api.example.com  the same thing, without typing curl

Requests are grouped by the API they belong to, and hosts that differ only by
subdomain — api.acme.com, api.staging.acme.com — are recognized as
environments of one API rather than three unrelated hosts. Several services on
localhost are not, so name them:

  pogo project add checkout localhost:3000
  pogo checkout curl /orders    files under checkout, resolves the path
  pogo checkout                 the UI, filtered to checkout
  pogo new                      a blank request to fill in and run`,

		// A URL is a valid first argument, and it is not a subcommand. Cobra
		// would otherwise reject it before RunE ever sees it.
		Args:               cobra.ArbitraryArgs,
		SilenceUsage:       true,
		SilenceErrors:      false,
		DisableFlagParsing: false,

		RunE: func(cmd *cobra.Command, args []string) error {
			// A URL never reaches here — Execute routes it straight to curl,
			// flags and all. Anything else unrecognized is a typo, and should
			// say so rather than be handed to curl, which would report it in
			// curl's words about pogo's mistake.
			if len(args) > 0 {
				return fmt.Errorf("unknown command %q for pogo\nrun 'pogo --help' for usage", args[0])
			}
			// No TTY means no TUI. A full-screen program in a pipe or in CI
			// would hang or spray escape codes; help is the useful answer.
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return cmd.Help()
			}
			return app.runTUI()
		},
	}

	root.AddCommand(
		newCurlCmd(app),
		newListCmd(app),
		newImportCmd(app),
		newAPICmd(app),
		newProjectCmd(app),
		newNewCmd(app),
		newEnvCmd(app),
		newConfigCmd(app),
		newCompactCmd(app),
		newUpdateCmd(app),
		newWhereCmd(app),
	)
	return root
}

// colorScheme wires Fang's styled --help/--version/error output to the same
// tokens the TUI uses, so CLI output and the TUI read as one product
// (SYSTEM_DESIGN.md §4.5).
func colorScheme(adaptive lipglossv2.LightDarkFunc) fang.ColorScheme {
	scheme := fang.DefaultColorScheme(adaptive)
	r, g, b := ui.PrimaryRGB()
	primary := color.RGBA{R: r, G: g, B: b, A: 0xFF}
	scheme.Title = primary
	scheme.Program = primary
	scheme.Command = primary
	scheme.Flag = primary
	scheme.Dash = primary
	return scheme
}
