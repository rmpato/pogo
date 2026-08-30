package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/rmpato/pogo/internal/apis"
	"github.com/rmpato/pogo/internal/capture"
	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
	"github.com/rmpato/pogo/internal/environment"
	"github.com/rmpato/pogo/internal/history"
	"github.com/rmpato/pogo/internal/runner"
	"github.com/rmpato/pogo/internal/selfupdate"
	"github.com/rmpato/pogo/internal/store"
	"github.com/rmpato/pogo/internal/version"
)

func newCurlCmd(app *app) *cobra.Command {
	return &cobra.Command{
		Use:   "curl [curl options] <url>",
		Short: "Make a request, exactly as curl would, and remember it",
		Long: `curl hands every argument to the real curl binary untouched, so anything
curl accepts, pogo accepts, and the output on your terminal is curl's own.
What pogo adds is a record of the exchange.

  pogo curl https://api.example.com/users
  pogo curl -X POST -H 'Content-Type: application/json' \
       -d '{"name":"Pato"}' https://api.example.com/users

pogo's own options are prefixed so that no curl option, present or future, can
collide with them:

  --pogo-no-capture      run the request without recording it
  --pogo-note <text>     attach a note to this entry
  --pogo-env <name>      resolve {{variables}} from this environment
  --pogo-api <domain>    file this request under an API, when its URL cannot say

Everything else belongs to curl, including --help: 'pogo curl --help' prints
curl's help, because at that point you are asking about curl.`,

		// Everything after `curl` is curl's. Cobra must not look at it, or a
		// -h meant for curl would print pogo's help instead.
		DisableFlagParsing: true,
		SilenceUsage:       true,

		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return app.runCurl(args)
		},
	}
}

// pogoFlags are pogo's own options on a wrapped request. They live behind a
// --pogo- prefix so that no curl option, present or future, can ever collide
// with them, and so that a reader of a shell history can tell at a glance which
// arguments were curl's.
type pogoFlags struct {
	noCapture bool
	note      string
	env       string
	api       string
}

// runCurl is the whole of pogo's wrapper behavior. It returns an exitCode
// error carrying curl's status, so a script that checks $? sees what it would
// have seen without pogo in the way.
func (a *app) runCurl(args []string) error {
	curlArgs, flags, err := extractFlags(args)
	if err != nil {
		return err
	}

	cfg := a.cfg()
	if flags.noCapture {
		cfg.Capture.Disabled = true
	}

	// Under a project prefix, a path is a request against that project's base
	// URL, and the host it resolves to is worth remembering — the whole reason
	// to type `pogo checkout` instead of the URL every time.
	if a.project != "" {
		if base, _ := cfg.APIs.Project(a.project); base != "" {
			curlArgs = resolveRelativeURL(curlArgs, base)
		}
	}

	var st *store.Store
	if !cfg.Capture.Disabled {
		if st, err = store.Open(cfg); err != nil {
			warn("history unavailable: %v", err)
			st = nil
		}
	}

	// Ctrl-C reaches curl directly through the terminal's process group. pogo
	// stays out of the way so that curl performs its own cleanup and reports
	// its own status, exactly as it would unwrapped.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT)

	dir, _ := os.Getwd()
	rec := capture.New(cfg, st)

	// Environments resolve {{variables}} on the way to curl. The command that
	// gets recorded keeps its braces, so the token never reaches history.
	rec, err = withEnvironment(rec, cfg, curlArgs, flags, a.project)
	if err != nil {
		return err
	}

	res, runErr := rec.Run(context.Background(), capture.Request{
		Args:        curlArgs,
		Source:      history.SourceRun,
		Dir:         dir,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Stdin:       os.Stdin,
		StdoutIsTTY: isatty.IsTerminal(os.Stdout.Fd()),
	})

	if runErr != nil {
		if res == nil || res.Run == nil || !res.Run.Started {
			if strings.Contains(runErr.Error(), runner.ErrCurlMissing.Error()) {
				fmt.Fprintf(os.Stderr, "pogo: %v\n", runErr)
				return exitCode(127) // conventional "command not found"
			}
			return runErr
		}
		// curl ran; only the recording failed. The user's request succeeded and
		// its exit status is what matters, so this is a warning, not a failure.
		warn("could not record request: %v", runErr)
	}

	// curl having started is the bar for "this project was actually pointed at
	// this host" — a missing binary should not mint a binding for a host
	// nothing ever reached.
	if a.project != "" {
		autoBindProject(a.store, a.project, curlArgs)
	}

	// A variable the environment does not define is left in the command
	// verbatim, so the request fails visibly rather than silently going
	// somewhere unintended. Say which one.
	if res != nil && len(res.MissingVars) > 0 {
		warn("undefined variable(s): {{%s}}", strings.Join(res.MissingVars, "}}, {{"))
	}

	if flags.note != "" && res != nil && res.Entry != nil && st != nil {
		if err := st.SetNote(res.Entry.ID, flags.note); err != nil {
			warn("could not save note: %v", err)
		}
	}

	// A release notice is printed after the request, never before it, and only
	// to an interactive terminal so scripts parsing stderr are unaffected.
	if isatty.IsTerminal(os.Stderr.Fd()) && os.Getenv("POGO_QUIET") == "" {
		selfupdate.Notice(cfg.Dir(), version.Version,
			cfg.Update.CheckInterval(), !cfg.Update.Disabled, os.Stderr)
	}

	if res == nil || res.Run == nil {
		return exitCode(2)
	}
	if res.Run.Exit != 0 {
		return exitCode(res.Run.Exit)
	}
	return nil
}

// withEnvironment binds the recorder to the environment and API this request
// should run under. project is the API this invocation was prefixed with
// (`pogo checkout curl …`), if any.
func withEnvironment(rec *capture.Recorder, cfg config.Config, curlArgs []string, flags pogoFlags, project string) (*capture.Recorder, error) {
	set, err := environment.Load(config.EnvFile())
	if err != nil {
		return nil, err
	}

	envName := firstNonEmpty(flags.env, os.Getenv("POGO_ENV"), set.Active)
	domain := resolveAPI(set, cfg, curlArgs, envName, flags, project)

	if domain != "" {
		rec = rec.WithAPI(domain)
	}
	if envName == "" {
		return rec, nil
	}
	vars := set.Vars(domain, envName)
	if vars == nil && !set.Has(envName) {
		return nil, fmt.Errorf("no environment named %q in %s", envName, config.EnvFile())
	}
	return rec.WithEnvironment(envName, vars), nil
}

// resolveAPI decides which API's variables a command should be resolved
// against.
//
// Usually the URL says so outright. When it does not — because the host is
// itself a variable, which is the whole reason `{{base}}/users` is worth
// writing — pogo asks, in order: what you said on the command line, the
// project it was prefixed with, what POGO_API says, and finally whether
// exactly one API defines every variable the command mentions. That last rule
// is the common case (one API owns `base` and `token`), and it is
// deliberately narrow: two candidates means pogo picks neither and leaves the
// braces in, which fails loudly instead of quietly calling the wrong
// environment.
//
// A project outranks the URL, not just the fallback: `pogo checkout curl
// https://api.stripe.com/…` files under checkout, the same as --pogo-api
// would, because the prefix is a deliberate label rather than a guess the URL
// could contradict.
func resolveAPI(set environment.Set, cfg config.Config, curlArgs []string, envName string, flags pogoFlags, project string) string {
	if flags.api != "" {
		return apis.Domain(flags.api)
	}
	if project != "" {
		return apis.Domain(project)
	}
	if v := os.Getenv("POGO_API"); v != "" {
		return apis.Domain(v)
	}

	spec := curlargs.Parse(curlArgs)
	if url := spec.URL(); url != "" && !strings.Contains(url, "{{") {
		return apis.Classify(url, cfg.APIs).Domain
	}

	refs := environment.References(curlArgs)
	if len(refs) == 0 || envName == "" {
		return ""
	}

	var found string
	for _, domain := range set.Domains() {
		vars := set.Vars(domain, envName)
		if vars == nil {
			continue
		}
		complete := true
		for _, ref := range refs {
			if _, ok := vars[ref]; !ok {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		if found != "" {
			return "" // ambiguous: say nothing rather than guess
		}
		found = domain
	}
	return found
}

// resolveRelativeURL turns a path-only operand into a full URL against base,
// so `pogo checkout curl /orders` reaches the project's host without anyone
// typing it out. curl itself has no use for a bare path — curlargs does not
// even recognize one as a URL, precisely because it never used to mean
// anything — so this only ever makes a command more capable, never less.
//
// It finds the first operand the same way curl would: skipping option flags
// and the values they consume, honoring `--`. Only that first operand is ever
// rewritten, matching pogo's own idea of "the" URL elsewhere (curlargs.Spec.URL).
func resolveRelativeURL(curlArgs []string, base string) []string {
	out := append([]string(nil), curlArgs...)
	afterOptions := false
	for i := 0; i < len(out); i++ {
		a := out[i]
		if !afterOptions {
			if a == "--" {
				afterOptions = true
				continue
			}
			if strings.HasPrefix(a, "-") {
				if curlargs.OptionTakesValue(a) {
					i++
				}
				continue
			}
		}
		if strings.HasPrefix(a, "/") {
			out[i] = strings.TrimSuffix(base, "/") + a
		}
		return out
	}
	return out
}

// autoBindProject remembers the host a project was just pointed at, the first
// time it sees it: `pogo checkout curl http://localhost:3000/x` means every
// later request to that host — prefixed or not — belongs to checkout too.
// Only loopback and private hosts are bound automatically; a public host
// pinned this way would silently swallow whatever the URL actually says.
func autoBindProject(cfgStore *config.Store[config.Config], project string, curlArgs []string) {
	spec := curlargs.Parse(curlArgs)
	url := spec.URL()
	if url == "" || strings.Contains(url, "{{") {
		return
	}
	host := strings.ToLower(history.HostOf(url))
	if host == "" {
		return
	}
	switch apis.GuessEnv(host) {
	case apis.EnvLocal, apis.EnvDev:
	default:
		return
	}
	if _, bound := cfgStore.Current().APIs.DomainFor(host); bound {
		return
	}

	if err := cfgStore.Update(func(c *config.Config) {
		c.APIs.SetDomain(host, project)
		if base, _ := c.APIs.Project(project); base == "" {
			if origin := urlOrigin(url); origin != "" {
				c.APIs.SetProject(project, origin)
			}
		}
	}); err != nil {
		return
	}
	warn("%s is now part of %s", host, project)
}

// urlOrigin is the scheme and host of a URL, with no path — a base a relative
// operand can be resolved against.
func urlOrigin(raw string) string {
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok {
		return ""
	}
	host := rest
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// extractFlags removes pogo's own options from the argument list. Everything
// else is passed through untouched and in its original order.
func extractFlags(args []string) ([]string, pogoFlags, error) {
	var (
		flags pogoFlags
		out   = make([]string, 0, len(args))
	)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--pogo-") {
			out = append(out, arg)
			continue
		}
		name, value, hasValue := strings.Cut(arg, "=")

		valued := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("option %s: requires parameter", name)
			}
			i++
			return args[i], nil
		}

		var err error
		switch name {
		case "--pogo-no-capture":
			flags.noCapture = true
		case "--pogo-env":
			flags.env, err = valued()
		case "--pogo-api":
			flags.api, err = valued()
		case "--pogo-note":
			flags.note, err = valued()
		default:
			err = fmt.Errorf("option %s: is unknown", name)
		}
		if err != nil {
			return nil, flags, err
		}
	}
	return out, flags, nil
}

// warn reports a pogo-level problem without pretending the request failed.
// POGO_QUIET silences these for scripts that parse curl's stderr.
func warn(format string, args ...any) {
	if os.Getenv("POGO_QUIET") != "" {
		return
	}
	fmt.Fprintf(os.Stderr, "pogo: "+format+"\n", args...)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
