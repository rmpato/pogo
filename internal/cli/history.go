package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/harimport"
	"github.com/rmpato/pogo/internal/store"
	"github.com/rmpato/pogo/internal/tui"
)

func newListCmd(app *app) *cobra.Command {
	var (
		limit  int
		filter string
		asJSON bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Print history as text instead of opening the UI",
		Long: `list is pogo's non-interactive face, so history composes with the rest of
your shell:

  pogo list -n 50 --filter 'status:5xx api:acme.com'
  pogo list --json -n 100 | jq 'select(.duration > 500)'

Search expressions are the same ones the UI's / key takes:

  users/42               free text, matched against method, URL and status
  method:POST            filter by method
  status:4xx             filter by status class, or status:404 for one code
  api:acme.com           filter by API
  env:staging            filter by environment
  host:api.example.com   filter by host
  collection:auth        filter by collection
  is:starred             only starred requests
  is:failed              only failures`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			cfg := app.cfg()
			st, err := store.Open(cfg)
			if err != nil {
				return err
			}
			if app.project != "" {
				filter = strings.TrimSpace("api:" + app.project + " " + filter)
			}
			return printList(cfg, st, filter, limit, asJSON)
		},
	}

	cmd.Flags().IntVarP(&limit, "number", "n", 20, "how many entries to print")
	cmd.Flags().StringVar(&filter, "filter", "", "a search expression")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit one JSON object per line")
	return cmd
}

func printList(cfg config.Config, st *store.Store, filter string, limit int, asJSON bool) error {
	res, err := st.Load()
	if err != nil {
		return err
	}

	query := tui.ParseQuery(filter).WithRegistry(cfg.APIs)
	entries := res.Entries[:0:0]
	for _, e := range res.Entries {
		if query.Empty() || query.Match(e) {
			entries = append(entries, e)
		}
	}
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		for _, e := range entries {
			if err := enc.Encode(e); err != nil {
				return err
			}
		}
		return nil
	}

	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, "no requests recorded yet — run: pogo curl https://example.com")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, e := range entries {
		status := "-"
		if s := e.Status(); s > 0 {
			status = fmt.Sprintf("%d", s)
		} else if e.Exit != 0 {
			status = fmt.Sprintf("exit %d", e.Exit)
		}
		star := " "
		if e.Favorite {
			star = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			star,
			e.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			e.Request.Method,
			status,
			e.Duration.String(),
			e.Request.URL)
	}
	return w.Flush()
}

// newImportCmd brings a browser's export into history.
//
// Imported entries are recorded as such and are never presented as something
// pogo ran; what they gain is everything else pogo does — grouping, search,
// inspection, editing, replay and diffing.
func newImportCmd(app *app) *cobra.Command {
	var collection string

	cmd := &cobra.Command{
		Use:   "import-har <file>",
		Short: "Import a browser HAR export into history",
		Long: `import-har reads a HAR file — devtools → Network → "Save all as HAR" — and
files every request in it as history. Nothing is run: the responses are the
ones the browser already received.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			st, err := store.Open(app.cfg())
			if err != nil {
				return err
			}
			return runImportHAR(st, args[0], collection)
		},
	}

	cmd.Flags().StringVar(&collection, "collection", "", "file the imported requests under this name")
	return cmd
}

func runImportHAR(st *store.Store, path, collection string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if collection == "" {
		collection = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	res, err := harimport.Parse(f, harimport.Options{Collection: collection})
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	for _, e := range res.Entries {
		// Payloads arrive inline from the parser; hand them to the store so the
		// large ones end up as blobs like any other capture.
		if ref := e.Request.Body; ref != nil {
			if stored, err := st.PutBody(e.ID, store.KindRequest, []byte(ref.Inline), ref.Size, "har"); err == nil {
				e.Request.Body = stored
			}
		}
		if e.Response != nil && e.Response.Body != nil {
			ref := e.Response.Body
			if stored, err := st.PutBody(e.ID, store.KindResponse, []byte(ref.Inline), ref.Size, "har"); err == nil {
				e.Response.Body = stored
			}
		}
		if err := st.Append(e); err != nil {
			return err
		}
	}

	fmt.Printf("imported %d requests into collection %q\n", len(res.Entries), collection)
	if res.Skipped > 0 {
		fmt.Printf("skipped %d entries with no URL\n", res.Skipped)
	}
	fmt.Println("browse them with: pogo")
	return nil
}

func newCompactCmd(app *app) *cobra.Command {
	return &cobra.Command{
		Use:   "compact",
		Short: "Rewrite the history log, dropping deleted entries",
		Long: `compact rewrites history.jsonl with only the entries that are still live,
applies the entry cap from your config, and sweeps blobs no entry references
any more. Starred requests are never dropped.`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			st, err := store.Open(app.cfg())
			if err != nil {
				return err
			}
			stats, err := st.Compact()
			if err != nil {
				return err
			}
			fmt.Printf("compacted to %d entries\n", stats.Live)
			return nil
		},
	}
}
