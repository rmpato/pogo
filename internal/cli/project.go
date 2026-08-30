package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/history"
	"github.com/rmpato/pogo/internal/store"
)

// splitProject peels a `pogo <project> …` prefix off the command line, before
// Cobra or the bare-URL shorthand see any of it.
//
// A leading word only becomes a project when it cannot be anything else pogo
// understands: not a flag, not a URL, not one of pogo's own subcommand names.
// Beyond that, an *existing* project accepts whatever follows it — a bare
// `pogo checkout`, `pogo checkout list`, `pogo checkout new` — because there
// is nothing left to be ambiguous about. A name pogo has never seen is held
// to a stricter test: it only counts when what follows can only be a request
// (curl, a URL, or `new`), so `pogo lst` (a typo) and `pogo mistype list` (a
// query, not a request) both fall through untouched, reporting "unknown
// command" instead of silently forking history into a new, empty project.
func splitProject(args []string, reg config.APIRegistry, reserved map[string]bool) (project string, rest []string, created bool) {
	if len(args) == 0 {
		return "", args, false
	}
	name := args[0]
	if reserved[name] || curlargs.LooksLikeURL(name) || !looksLikeProjectName(name) {
		return "", args, false
	}

	lower := strings.ToLower(name)
	_, exists := reg.Project(lower)
	rest = args[1:]
	if len(rest) == 0 {
		rest = nil
	}

	switch {
	case exists:
		return lower, rest, false
	case len(rest) > 0 && (rest[0] == "curl" || rest[0] == "new" || curlargs.LooksLikeURL(rest[0])):
		return lower, rest, true
	}
	return "", args, false
}

// looksLikeProjectName rejects anything that could not plausibly be a name
// someone chose on purpose: flags, paths, and the punctuation curl options use.
func looksLikeProjectName(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// reservedNames is every top-level name pogo already owns, so a project can
// never shadow a subcommand, or Cobra's own help and completion commands.
func reservedNames(root *cobra.Command) map[string]bool {
	reserved := map[string]bool{
		"help": true, "completion": true, "__complete": true, "__completeNoDesc": true,
	}
	for _, c := range root.Commands() {
		reserved[c.Name()] = true
		for _, a := range c.Aliases {
			reserved[a] = true
		}
	}
	return reserved
}

func newProjectCmd(app *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Declare and manage projects — named APIs for hosts pogo cannot tell apart on its own",
		Long: `A project is an API you name yourself instead of one pogo works out from a
host. It exists for the case pogo's grouping cannot handle on its own: several
services all running on localhost, one port apiece.

  pogo project add checkout http://localhost:3000
  pogo checkout curl /orders        files under checkout, resolves the path
  pogo checkout                     the UI, filtered to checkout

The first request run under a name pogo has not seen creates it automatically
— 'pogo project add' is for giving it a base URL up front, or naming one that
has not made a request yet.

A project used against a public host still groups exactly like any other
API — it is a label, not a redirect.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			return listProjects(app)
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:          "list",
			Short:        "List every project",
			Args:         cobra.NoArgs,
			SilenceUsage: true,
			RunE:         func(*cobra.Command, []string) error { return listProjects(app) },
		},
		&cobra.Command{
			Use:   "add <name> [base-url]",
			Short: "Declare a project, optionally with a base URL for relative paths",
			Long: `add makes a name usable as a prefix:

  pogo project add checkout http://localhost:3000

With a base URL, pogo can resolve a relative path against it:

  pogo checkout curl /orders   →   http://localhost:3000/orders

The base URL's host is also bound to the project, the same as
'pogo api move' would do by hand.`,
			Args:         cobra.RangeArgs(1, 2),
			SilenceUsage: true,
			RunE: func(_ *cobra.Command, args []string) error {
				name := strings.ToLower(args[0])
				base := ""
				if len(args) > 1 {
					base = args[1]
				}
				if err := app.store.Update(func(c *config.Config) {
					c.APIs.SetProject(name, base)
					if host := strings.ToLower(history.HostOf(base)); host != "" {
						c.APIs.SetDomain(host, name)
					}
				}); err != nil {
					return err
				}
				if base == "" {
					fmt.Printf("%s is now a project\n", name)
					return nil
				}
				fmt.Printf("%s is now a project · %s\n", name, base)
				return nil
			},
		},
		&cobra.Command{
			Use:   "rm <name>",
			Short: "Forget a project",
			Long: `rm removes the project and unbinds every host reassigned to it. History
already filed under it is untouched — it just goes back to being classified
from the host, the way it would have been without the project.`,
			Args:         cobra.ExactArgs(1),
			SilenceUsage: true,
			RunE: func(_ *cobra.Command, args []string) error {
				name := strings.ToLower(args[0])
				if err := app.store.Update(func(c *config.Config) { c.APIs.RemoveProject(name) }); err != nil {
					return err
				}
				fmt.Printf("%s removed\n", name)
				return nil
			},
		},
	)
	return cmd
}

func listProjects(app *app) error {
	cfg := app.cfg()
	names := cfg.APIs.ProjectNames()
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "no projects yet — run: pogo project add <name> [base-url]")
		return nil
	}

	st, err := store.Open(cfg)
	if err != nil {
		return err
	}
	res, err := st.Load()
	if err != nil {
		return err
	}

	counts := map[string]int{}
	for _, e := range res.Entries {
		ref := apis.Classify(e.Request.URL, cfg.APIs)
		trust := !ref.Bound && e.API != "" && e.API != ref.Domain
		if trust && ref.Domain != "" {
			_, trust = cfg.APIs.Project(e.API)
		}
		if trust {
			ref.Domain = e.API
		}
		counts[ref.Domain]++
	}

	hosts := map[string][]string{}
	for host, domain := range cfg.APIs.Domains {
		hosts[domain] = append(hosts[domain], host)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, name := range names {
		base, _ := cfg.APIs.Project(name)
		hs := hosts[name]
		sort.Strings(hs)
		fmt.Fprintf(w, "%s\t%s\t%s\t%d requests\n", name, base, strings.Join(hs, ", "), counts[name])
	}
	return w.Flush()
}
