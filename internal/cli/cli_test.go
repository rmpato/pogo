package cli

import (
	"reflect"
	"testing"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/curlargs"
)

// The wrapper's whole promise is that curl sees exactly what you typed. A curl
// command line is full of things that look like flags to another parser —
// -sSL, --data-raw, a bare --, arguments that begin with a dash — and every one
// of them has to survive untouched.
func TestCurlArgumentsSurviveUnchanged(t *testing.T) {
	args := []string{
		"-sSL", "--compressed", "-X", "POST",
		"-H", "Content-Type: application/json",
		"-H", "Authorization: Bearer sk-live-xyz",
		"--data-raw", `{"note":"--not-a-flag"}`,
		"-o", "-", "--", "https://api.acme.com/v1/orders",
	}
	want := append([]string(nil), args...)

	got, flags, err := extractFlags(args)
	if err != nil {
		t.Fatalf("extractFlags: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("curl args were rewritten:\n got %q\nwant %q", got, want)
	}
	if flags != (pogoFlags{}) {
		t.Errorf("flags = %+v, want none of pogo's own", flags)
	}
}

func TestPogoFlagsAreExtracted(t *testing.T) {
	args := []string{
		"--pogo-env", "staging",
		"-sS",
		"--pogo-note=first login",
		"--pogo-no-capture",
		"--pogo-api", "acme.com",
		"https://api.acme.com/",
	}

	got, flags, err := extractFlags(args)
	if err != nil {
		t.Fatalf("extractFlags: %v", err)
	}
	if want := []string{"-sS", "https://api.acme.com/"}; !reflect.DeepEqual(got, want) {
		t.Errorf("curl args = %q, want %q", got, want)
	}
	want := pogoFlags{noCapture: true, note: "first login", env: "staging", api: "acme.com"}
	if flags != want {
		t.Errorf("flags = %+v, want %+v", flags, want)
	}
}

func TestPogoFlagErrors(t *testing.T) {
	if _, _, err := extractFlags([]string{"--pogo-env"}); err == nil {
		t.Error("a flag with no value should be reported, not silently dropped")
	}
	if _, _, err := extractFlags([]string{"--pogo-nope", "x"}); err == nil {
		t.Error("an unknown --pogo- flag should be reported rather than passed to curl")
	}
}

// The bare-URL shorthand is decided by LooksLikeURL before Cobra runs, so it
// must never claim a subcommand name or one of pogo's own flags.
func TestShorthandNeverSwallowsACommand(t *testing.T) {
	for _, name := range []string{
		"curl", "list", "import-har", "api", "env", "config",
		"compact", "update", "where", "help", "completion",
		"project", "new", "--help", "-h", "--version", "lst",
	} {
		if curlargs.LooksLikeURL(name) {
			t.Errorf("%q would be treated as a URL and handed to curl", name)
		}
	}

	for _, url := range []string{
		"https://api.acme.com/v1", "http://localhost:3000/health",
		"localhost:8080/x", "api.acme.com/v1", "{{base}}/users",
	} {
		if !curlargs.LooksLikeURL(url) {
			t.Errorf("%q should be recognized as a request", url)
		}
	}
}

// splitProject decides whether `pogo <word> …` is a project prefix. Getting
// this wrong in either direction is bad: too eager, and a typo silently forks
// history into a new project; too strict, and the whole point of the prefix
// — not having to spell out --pogo-api — is lost.
func TestSplitProject(t *testing.T) {
	reserved := map[string]bool{"curl": true, "list": true, "new": true, "project": true, "help": true}
	known := config.APIRegistry{Projects: map[string]string{"checkout": "http://localhost:3000"}}

	cases := []struct {
		name        string
		args        []string
		reg         config.APIRegistry
		wantProject string
		wantRest    []string
		wantCreated bool
	}{
		{"known project, curl", []string{"checkout", "curl", "/orders"}, known, "checkout", []string{"curl", "/orders"}, false},
		{"known project, bare", []string{"checkout"}, known, "checkout", nil, false},
		{"known project, url shorthand", []string{"checkout", "http://localhost:3000/x"}, known, "checkout", []string{"http://localhost:3000/x"}, false},
		{"known project, list", []string{"checkout", "list", "-n", "5"}, known, "checkout", []string{"list", "-n", "5"}, false},
		{"known project, new", []string{"checkout", "new"}, known, "checkout", []string{"new"}, false},
		{"unknown + curl creates", []string{"scratch", "curl", "http://localhost:8080/x"}, known, "scratch", []string{"curl", "http://localhost:8080/x"}, true},
		{"unknown + url creates", []string{"scratch", "http://localhost:8080/x"}, known, "scratch", []string{"http://localhost:8080/x"}, true},
		{"unknown + new creates", []string{"scratch", "new"}, known, "scratch", []string{"new"}, true},
		{"unknown bare does not create", []string{"scratch"}, known, "", []string{"scratch"}, false},
		{"unknown + list does not create", []string{"scratch", "list"}, known, "", []string{"scratch", "list"}, false},
		{"a real subcommand is never a project", []string{"list", "-n", "5"}, known, "", []string{"list", "-n", "5"}, false},
		{"a URL is never a project", []string{"http://localhost:3000/x"}, known, "", []string{"http://localhost:3000/x"}, false},
		{"a flag is never a project", []string{"-sS", "checkout"}, known, "", []string{"-sS", "checkout"}, false},
		{"no args", nil, known, "", nil, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotProject, gotRest, gotCreated := splitProject(c.args, c.reg, reserved)
			if gotProject != c.wantProject || !reflect.DeepEqual(gotRest, c.wantRest) || gotCreated != c.wantCreated {
				t.Errorf("splitProject(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.args, gotProject, gotRest, gotCreated, c.wantProject, c.wantRest, c.wantCreated)
			}
		})
	}
}

func TestResolveRelativeURL(t *testing.T) {
	cases := []struct {
		name string
		args []string
		base string
		want []string
	}{
		{
			name: "relative path resolves against the base",
			args: []string{"-X", "POST", "/orders"},
			base: "http://localhost:3000",
			want: []string{"-X", "POST", "http://localhost:3000/orders"},
		},
		{
			name: "a trailing slash on the base is not doubled",
			args: []string{"/orders"},
			base: "http://localhost:3000/",
			want: []string{"http://localhost:3000/orders"},
		},
		{
			name: "an absolute URL is left alone",
			args: []string{"https://api.stripe.com/v1/charges"},
			base: "http://localhost:3000",
			want: []string{"https://api.stripe.com/v1/charges"},
		},
		{
			name: "a path that is an option's value is left alone",
			args: []string{"-o", "/orders", "http://localhost:3000/health"},
			base: "http://localhost:3000",
			want: []string{"-o", "/orders", "http://localhost:3000/health"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resolveRelativeURL(c.args, c.base); !reflect.DeepEqual(got, c.want) {
				t.Errorf("resolveRelativeURL(%q, %q) = %q, want %q", c.args, c.base, got, c.want)
			}
		})
	}
}
