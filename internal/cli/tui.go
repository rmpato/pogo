package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/rmpato/pogo/internal/capture"
	"github.com/rmpato/pogo/internal/store"
	"github.com/rmpato/pogo/internal/tui"
)

// runTUI opens the interactive face of pogo, over whatever history there is —
// or, under a project prefix, over that project's slice of it.
func (a *app) runTUI() error {
	return a.openTUI(tui.Options{})
}

// runTUICompose opens the interactive face straight onto a blank request,
// filed under project when it runs — `pogo new` and `pogo checkout new`.
func (a *app) runTUICompose(project string) error {
	return a.openTUI(tui.Options{Compose: true, Project: project})
}

// openTUI is the one place the TUI gets built, so the interactive and
// scripted paths open the same history with the same configuration — a
// replay from the UI is not a reconstruction of a request, it is the same
// code running the same argv (see docs/architecture.md).
func (a *app) openTUI(opts tui.Options) error {
	cfg := a.cfg()

	st, err := store.Open(cfg)
	if err != nil {
		return err
	}

	opts.Config = a.store
	opts.Store = st
	opts.Recorder = capture.New(cfg, st)

	if a.project != "" {
		if opts.Project == "" {
			opts.Project = a.project
		}
		if opts.Filter == "" && !opts.Compose {
			opts.Filter = "api:" + a.project
		}
	}
	return tui.Run(opts)
}

// newNewCmd opens straight onto a blank request — the same screen `n` opens
// from inside the list.
func newNewCmd(app *app) *cobra.Command {
	return &cobra.Command{
		Use:   "new [project]",
		Short: "Open a blank request in the UI, ready to fill in and run",
		Long: `new drops you onto a blank request instead of the list — the same screen
'n' opens from inside pogo.

  pogo new             nothing filed yet; the URL you type decides
  pogo new checkout    filed under checkout when it runs
  pogo checkout new    the same thing, the other way round`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			project := app.project
			if len(args) > 0 {
				project = strings.ToLower(args[0])
			}
			return app.runTUICompose(project)
		},
	}
}
