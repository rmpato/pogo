package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmpato/pogo/internal/capture"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/home"
	"github.com/rmpato/pogo/internal/store"
	"github.com/rmpato/pogo/internal/ui"
)

// Options is everything the TUI needs from the outside world. It holds the
// config *store* rather than a config value because a preference changed on a
// screen persists on the keypress that changed it — there is no "unsaved
// settings" state to get wrong (whis SYSTEM_DESIGN.md §11).
type Options struct {
	Config   *config.Store[config.Config]
	Store    *store.Store
	Recorder *capture.Recorder

	// Project is the API a blank new request defaults to filing under, and
	// what Filter narrows the list to when neither Filter nor Compose is set
	// explicitly — `pogo checkout` opens on that project's history.
	Project string
	// Filter is a search expression applied to the list on the very first
	// screen, e.g. "api:checkout". Empty leaves the list unfiltered.
	Filter string
	// Compose opens directly on a blank request instead of the list —
	// `pogo new` and `pogo checkout new`.
	Compose bool
}

// exitTo says where the user was going when they left a screen.
type exitTo int

const (
	exitQuit exitTo = iota
	exitHome
)

// Run opens pogo's interactive face and blocks until the user quits.
//
// Requests are the landing screen, because they are the product: pogo is worth
// opening when it already knows something, and what it knows is what you have
// run. The home shell is a level *above* that rather than in front of it —
// press H — where the workspaces that are not the list live (SYSTEM_DESIGN.md
// §7.1 puts the shell first; pogo has one workspace people came for, so it
// puts that first and keeps the shell one key away).
func Run(opts Options) error {
	if !opts.Config.Current().OnboardingSeen {
		if err := runOnboarding(opts); err != nil {
			return err
		}
	}

	screen := screenList
	first := true
	for {
		m := New(opts)
		m.screen = screen

		// Filter and Compose describe how pogo was opened this run, not a
		// standing preference — they apply once, to the first screen shown,
		// and never again after a trip up to the home shell and back.
		if first {
			first = false
			switch {
			case opts.Compose:
				base, _ := m.cfg.APIs.Project(opts.Project)
				m.startNewRequest(opts.Project, base)
			case opts.Filter != "":
				m.query = ParseQuery(opts.Filter).WithRegistry(m.cfg.APIs)
				m.search.SetValue(opts.Filter)
			}
		}

		final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
		if err != nil {
			return err
		}

		done, ok := final.(*Model)
		if !ok || done.exit == exitQuit {
			return nil
		}

		screen, err = chooseFromHome(opts, done)
		if err != nil {
			return err
		}
		if screen < 0 {
			return nil // quit from the home shell
		}
	}
}

// chooseFromHome shows the shell and returns the screen to open next, or -1 to
// quit.
func chooseFromHome(opts Options, m *Model) (screen, error) {
	for {
		chosen, err := home.Choose(homeConfig(m))
		if err != nil {
			return -1, err
		}

		switch chosen {
		case "":
			return -1, nil
		case "requests":
			return screenList, nil
		case "apis":
			return screenAPIs, nil
		case "settings":
			return screenSettings, nil
		case "keys":
			return screenHelp, nil
		case "guide":
			if err := runOnboarding(opts); err != nil {
				return -1, err
			}
			// Back to the shell rather than into a workspace: the walkthrough
			// was opened to be read, not to go somewhere.
		default:
			return screenList, nil
		}
	}
}

// homeConfig describes pogo to the shell. The stats are the three facts worth
// knowing before choosing anything: how much history there is, how much of it
// is broken, and what a request would run against right now.
func homeConfig(m *Model) home.Config {
	failed := 0
	for _, e := range m.entries {
		if !e.OK() {
			failed++
		}
	}
	env := m.envSet.Active
	if env == "" {
		env = "none"
	}

	return home.Config{
		AppName:       "pogo",
		Tagline:       "curl, but it remembers",
		RecommendedID: "requests",
		Stats: []home.Stat{
			{Icon: "▤", Value: itoa(len(m.entries)), Label: "requests"},
			{Icon: "◈", Value: itoa(len(m.apiSummary())), Label: "APIs"},
			{Icon: "◇", Value: env, Label: "environment"},
		},
		Items: []home.Item{
			{ID: "requests", Icon: "▤", Title: "Requests",
				Description: describeRequests(len(m.entries), failed)},
			{ID: "apis", Icon: "◈", Title: "APIs and environments",
				Description: "What pogo worked out about your hosts, and how to correct it"},
			{ID: "settings", Icon: "◇", Title: "Settings",
				Description: "Theme, redaction, release checks, where things live"},
			{ID: "keys", Icon: "⌘", Title: "Keyboard reference",
				Description: "Every key, and the search syntax"},
			{ID: "guide", Icon: "?", Title: "How this works",
				Description: "The loop pogo is built around, in four steps"},
		},
		HelpLines: []string{
			"  pogo                     Open this shell's Requests workspace",
			"  pogo curl <args>         Make a request, exactly as curl would",
			"  pogo https://…           The same thing, without typing curl",
			"  pogo new                 A blank request, ready to fill in and run",
			"  pogo list --json         Print history for a script to read",
			"  pogo api                 Show and correct how requests are grouped",
			"  pogo project add <name>  Name an API pogo cannot tell apart on its own",
			"  pogo env                 Manage the environments {{variables}} come from",
			"  pogo import-har <file>   Bring a browser export into history",
		},
	}
}

func describeRequests(total, failed int) string {
	if total == 0 {
		return "Nothing recorded yet — run pogo curl against something"
	}
	if failed == 0 {
		return fmt.Sprintf("%s recorded, all of them fine", pluralize(total, "request"))
	}
	return fmt.Sprintf("%s recorded, %d of them failing", pluralize(total, "request"), failed)
}

// runOnboarding shows the walkthrough and records that it has been seen — the
// moment it is dismissed, not when it is finished, so a walkthrough somebody
// escaped out of does not come back tomorrow.
func runOnboarding(opts Options) error {
	_, err := home.ShowOnboarding(home.OnboardingConfig{
		AppName: "pogo",
		Intro:   "Four steps, and you are already doing the first one",
		Steps: []home.Step{
			{
				Title:   "Type pogo where you would have typed curl",
				Body:    "Every flag curl takes, pogo takes, and the output is curl's own. What pogo adds is a record: the response, the status, how long it took.",
				Compact: "pogo curl … runs curl and writes down what happened.",
			},
			{
				Title:   "Find it again by API",
				Body:    "Hosts that share a domain are one API, and the subdomain says which environment. api.acme.com and api.staging.acme.com are one thing in two places, not two things.",
				Compact: "History groups by API, with environments underneath.",
			},
			{
				Title:   "Change it and run it again",
				Body:    "Open a request as fields, edit any of them, and run it as a new entry. The original is never touched — history is append-only.",
				Compact: "e edits, ctrl+r runs it as a new entry.",
			},
			{
				Title:   "See what changed",
				Body:    "Mark a request with d, press d on another, and pogo diffs the two responses. After a replay the mark is already set for you.",
				Badge:   "tip",
				Compact: "d marks, d again compares.",
			},
		},
		SecondaryKey:   "",
		SecondaryLabel: "",
	})
	if err != nil {
		return err
	}

	if err := opts.Config.Update(func(c *config.Config) { c.OnboardingSeen = true }); err != nil {
		// Not worth stopping for: the worst case is being shown this again.
		ui.Warn("could not record that you have seen the walkthrough: " + err.Error())
	}
	return nil
}
