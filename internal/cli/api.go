package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/store"
)

func newAPICmd(app *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api",
		Short: "Show and correct how requests are grouped into APIs",
		Long: `pogo groups history by API: hosts that share a registrable domain are one
API, and the subdomain usually says which environment a host is.

  api.acme.com            acme.com  ·  prod
  api.staging.acme.com    acme.com  ·  staging
  dev-api.acme.com        acme.com  ·  dev
  localhost:3000          localhost ·  local

Both of those are guesses. Everything below corrects one, permanently — the
correction is written to the config file and wins from then on.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return listAPIs(app)
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:          "list",
			Short:        "List every API in history, with its environments and hosts",
			Args:         cobra.NoArgs,
			SilenceUsage: true,
			RunE:         func(*cobra.Command, []string) error { return listAPIs(app) },
		},
		&cobra.Command{
			Use:   "name <domain> <name>",
			Short: "Give an API a display name",
			Long: `name replaces the domain shown in the sidebar with something you would
actually call it:

  pogo api name acme.com Acme

Pass an empty name to go back to showing the domain.`,
			Args:         cobra.RangeArgs(1, 2),
			SilenceUsage: true,
			RunE: func(_ *cobra.Command, args []string) error {
				name := ""
				if len(args) > 1 {
					name = args[1]
				}
				domain := apis.Domain(args[0])
				if err := app.store.Update(func(c *config.Config) { c.APIs.SetName(domain, name) }); err != nil {
					return err
				}
				if name == "" {
					fmt.Printf("%s is shown by domain again\n", domain)
					return nil
				}
				fmt.Printf("%s is now shown as %q\n", domain, name)
				return nil
			},
		},
		&cobra.Command{
			Use:   "pin <host> <env>",
			Short: "Pin a host to an environment",
			Long: `pin overrides what pogo read out of a hostname:

  pogo api pin api-2.acme.com staging
  pogo api pin localhost:3000 dev

Pass an empty environment to go back to the guess.`,
			Args:         cobra.RangeArgs(1, 2),
			SilenceUsage: true,
			RunE: func(_ *cobra.Command, args []string) error {
				host := strings.ToLower(args[0])
				env := ""
				if len(args) > 1 {
					env = args[1]
				}
				if err := app.store.Update(func(c *config.Config) { c.APIs.SetEnv(host, env) }); err != nil {
					return err
				}
				if env == "" {
					fmt.Printf("%s goes back to %s (guessed)\n", host, apis.GuessEnv(host))
					return nil
				}
				fmt.Printf("%s is %s\n", host, env)
				return nil
			},
		},
		&cobra.Command{
			Use:   "move <host> <domain>",
			Short: "File a host under a different API",
			Long: `move is for the hosts the domain rule cannot get right on its own: a
partner API on someone else's domain, or a bare localhost that is really your
own service running locally.

  pogo api move localhost:3000 acme.com

Pass an empty domain to go back to the registrable domain.`,
			Args:         cobra.RangeArgs(1, 2),
			SilenceUsage: true,
			RunE: func(_ *cobra.Command, args []string) error {
				host := strings.ToLower(args[0])
				domain := ""
				if len(args) > 1 {
					domain = apis.Domain(args[1])
				}
				if err := app.store.Update(func(c *config.Config) { c.APIs.SetDomain(host, domain) }); err != nil {
					return err
				}
				if domain == "" {
					fmt.Printf("%s goes back to %s\n", host, apis.Domain(host))
					return nil
				}
				fmt.Printf("%s is part of %s\n", host, domain)
				return nil
			},
		},
		hideCmd(app, "hide", true, "Keep an API out of the sidebar and the grouped list"),
		hideCmd(app, "show", false, "Show an API that was hidden"),
	)
	return cmd
}

func hideCmd(app *app, use string, hidden bool, short string) *cobra.Command {
	return &cobra.Command{
		Use:          use + " <domain>",
		Short:        short,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			domain := apis.Domain(args[0])
			if err := app.store.Update(func(c *config.Config) { c.APIs.SetHidden(domain, hidden) }); err != nil {
				return err
			}
			fmt.Printf("%s %s\n", domain, map[bool]string{true: "hidden", false: "shown"}[hidden])
			return nil
		},
	}
}

func listAPIs(app *app) error {
	cfg := app.cfg()
	st, err := store.Open(cfg)
	if err != nil {
		return err
	}
	res, err := st.Load()
	if err != nil {
		return err
	}

	refs := make([]apis.Ref, 0, len(res.Entries))
	for _, e := range res.Entries {
		ref := apis.Classify(e.Request.URL, cfg.APIs)
		trust := !ref.Bound && e.API != "" && e.API != ref.Domain
		if trust && ref.Domain != "" {
			// The URL resolves on its own; only a project still on file is
			// worth trusting over that — once removed, its old entries should
			// read the way they would have without it.
			_, trust = cfg.APIs.Project(e.API)
		}
		if trust {
			ref.Domain = e.API
			ref.Name = cfg.APIs.Name(e.API)
			if ref.Env == "" {
				ref.Env = e.Env
			}
		}
		refs = append(refs, ref)
	}

	summary := apis.Summarize(refs, cfg.APIs)
	if len(summary) == 0 {
		fmt.Fprintln(os.Stderr, "no requests recorded yet — run: pogo curl https://example.com")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, api := range summary {
		label := api.Name
		if api.Name != api.Domain {
			label = fmt.Sprintf("%s (%s)", api.Name, api.Domain)
		}
		if api.Hidden {
			label += "  [hidden]"
		}
		fmt.Fprintf(w, "%s\t\t%d requests\n", label, api.Count)
		for _, env := range api.Envs {
			fmt.Fprintf(w, "  %s\t%s\t%d\n", env.Name, strings.Join(env.Hosts, ", "), env.Count)
		}
	}
	return w.Flush()
}
