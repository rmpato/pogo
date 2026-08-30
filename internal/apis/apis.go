// Package apis works out which API a request belongs to, and which
// environment of it.
//
// The observation this is built on: one API is one registrable domain. The
// hosts api.acme.com, api.staging.acme.com and dev-api.acme.com are not three
// unrelated servers, they are one API in three environments — and a history
// that lists them as three separate hosts has thrown away the only fact that
// made it navigable.
//
// So pogo groups by registrable domain (acme.com) and reads the environment
// out of what is left (staging, dev). Both are inferences, and an inference
// that cannot be corrected is worse than none: every one of them is overridable
// through config.APIRegistry, and an override always wins.
//
// This package is pure: it takes a URL and a registry and returns data. It
// reads no files and knows nothing about terminals.
package apis

import (
	"sort"
	"strings"

	"golang.org/x/net/publicsuffix"

	"github.com/rmpato/pogo/internal/config"
	"github.com/rmpato/pogo/internal/history"
)

// The environment names pogo knows how to recognize. They are ordinary
// strings, not an enum: an environment pogo has never heard of is still a
// perfectly good environment, it just has to be pinned by hand once.
const (
	EnvProd    = "prod"
	EnvPreprod = "preprod"
	EnvStaging = "staging"
	EnvTest    = "test"
	EnvDev     = "dev"
	EnvSandbox = "sandbox"
	EnvLocal   = "local"
)

// markers maps the label fragments that appear in real hostnames to the
// environment they mean. Longest match wins, so "preprod" is not read as
// "prod" with a prefix.
var markers = []struct {
	fragment string
	env      string
}{
	{"preprod", EnvPreprod},
	{"pre-prod", EnvPreprod},
	{"staging", EnvStaging},
	{"stage", EnvStaging},
	{"stg", EnvStaging},
	{"sandbox", EnvSandbox},
	{"sbx", EnvSandbox},
	{"develop", EnvDev},
	{"dev", EnvDev},
	{"integration", EnvTest},
	{"testing", EnvTest},
	{"test", EnvTest},
	{"qa", EnvTest},
	{"uat", EnvTest},
	{"local", EnvLocal},
	{"production", EnvProd},
	{"prod", EnvProd},
	{"live", EnvProd},
}

// rank orders environments the way a deployment pipeline reads backwards:
// production first, because that is the one whose failures matter most.
var rank = map[string]int{
	EnvProd: 0, EnvPreprod: 1, EnvStaging: 2, EnvTest: 3,
	EnvDev: 4, EnvSandbox: 5, EnvLocal: 6,
}

// Ref is everything pogo knows about where one request went.
type Ref struct {
	Host   string // api.staging.acme.com:8443
	Domain string // acme.com — the key an API is grouped under
	Name   string // the API's display name, or its domain
	Env    string // staging
	// Pinned reports that the environment came from the user rather than from
	// a guess. The UI says so, because a guess the user has not checked and a
	// fact they stated are not the same thing.
	Pinned bool
	// Bound reports that Domain came from an explicit reassignment
	// (config.APIRegistry.Domains) rather than the registrable-domain rule. A
	// caller with its own record of which API a request ran under — the
	// stored history.Entry.API — should still prefer a bound Domain over that
	// record, because a bound host is a standing fact and a stored API is a
	// one-time observation.
	Bound bool
}

// Classify works out the API and environment for a URL.
//
// An empty or unparseable URL yields a zero Ref rather than an error: pogo
// records whatever curl was asked to do, including things that were never
// going to resolve, and none of that should stop history from loading.
func Classify(rawURL string, reg config.APIRegistry) Ref {
	host := strings.ToLower(history.HostOf(rawURL))
	if host == "" {
		return Ref{}
	}

	ref := Ref{Host: host}

	if domain, ok := reg.DomainFor(host); ok {
		ref.Domain, ref.Bound = strings.ToLower(domain), true
	} else {
		ref.Domain = Domain(host)
	}

	if env, ok := reg.EnvFor(host); ok {
		ref.Env, ref.Pinned = env, true
	} else {
		ref.Env = GuessEnv(host)
	}

	ref.Name = reg.Name(ref.Domain)
	return ref
}

// Domain returns the registrable domain of a host: the part that identifies
// one API. Ports, IP addresses and the local machine are their own domains —
// there is nothing above them to group by.
func Domain(host string) string {
	host = hostname(host)
	if host == "" {
		return ""
	}
	if isLoopback(host) || isIP(host) {
		return host
	}
	// A single label ("api", "gateway") is a name on some private network. It
	// has no registrable domain, and it is its own API.
	if !strings.Contains(host, ".") {
		return host
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || domain == "" {
		return host
	}
	return domain
}

// GuessEnv reads an environment out of a hostname.
//
// It looks only at the labels *above* the registrable domain, so that
// "staging.co.uk" — where "staging" is somebody's actual company — is not read
// as a staging environment, while "api.staging.acme.co.uk" is. Anything with no
// marker at all is production: that is what an unqualified api.acme.com means
// everywhere, and guessing otherwise would file most of a user's history under
// a caveat.
func GuessEnv(host string) string {
	name := hostname(host)
	if name == "" {
		return ""
	}
	if isLoopback(name) {
		return EnvLocal
	}
	if isIP(name) {
		// A private address is somebody's own machine or cluster, not the
		// public internet; anything else is unknowable from the number alone.
		if isPrivateIP(name) {
			return EnvDev
		}
		return EnvProd
	}

	prefix := name
	if domain := Domain(name); domain != "" && domain != name {
		prefix = strings.TrimSuffix(name, "."+domain)
	} else if domain == name {
		prefix = ""
	}

	// Hostnames say it three ways: api.staging.acme.com, dev-api.acme.com and
	// pre-prod.acme.com. Whole labels are matched before hyphen-separated
	// pieces, or "pre-prod" would come apart into "pre" and "prod" and be read
	// as production — the exact opposite of what it says.
	if env := matchLabels(prefix, "."); env != "" {
		return env
	}
	if env := matchLabels(prefix, ".-_"); env != "" {
		return env
	}
	// Then the looser test, for labels like "acme-staging2". Only the longer
	// markers are used here: "qa" inside "aqua" and "uat" inside "situate" are
	// coincidences, and a substring rule cannot tell them from an environment.
	for _, m := range markers {
		if len(m.fragment) >= 4 && strings.Contains(prefix, m.fragment) {
			return m.env
		}
	}
	return EnvProd
}

// API is one API and the environments it was reached through.
type API struct {
	Domain string
	Name   string
	Envs   []Env
	Count  int
	Hidden bool
}

// Env is one environment of one API.
type Env struct {
	Name  string
	Hosts []string
	Count int
}

// Summarize folds a run of refs into the API list the sidebar draws: APIs
// ordered by how much traffic they saw, environments ordered by how close to
// production they are, hosts named so a pin has something to point at.
func Summarize(refs []Ref, reg config.APIRegistry) []API {
	type bucket struct {
		api   *API
		envs  map[string]*Env
		hosts map[string]map[string]bool
	}
	buckets := map[string]*bucket{}
	var order []string

	for _, ref := range refs {
		if ref.Domain == "" {
			continue
		}
		b, ok := buckets[ref.Domain]
		if !ok {
			b = &bucket{
				api:   &API{Domain: ref.Domain, Name: reg.Name(ref.Domain), Hidden: reg.IsHidden(ref.Domain)},
				envs:  map[string]*Env{},
				hosts: map[string]map[string]bool{},
			}
			buckets[ref.Domain] = b
			order = append(order, ref.Domain)
		}
		b.api.Count++

		env, ok := b.envs[ref.Env]
		if !ok {
			env = &Env{Name: ref.Env}
			b.envs[ref.Env] = env
			b.hosts[ref.Env] = map[string]bool{}
		}
		env.Count++
		if ref.Host != "" && !b.hosts[ref.Env][ref.Host] {
			b.hosts[ref.Env][ref.Host] = true
			env.Hosts = append(env.Hosts, ref.Host)
		}
	}

	out := make([]API, 0, len(order))
	for _, domain := range order {
		b := buckets[domain]
		envs := make([]Env, 0, len(b.envs))
		for _, env := range b.envs {
			sort.Strings(env.Hosts)
			envs = append(envs, *env)
		}
		sort.Slice(envs, func(i, j int) bool {
			ri, oki := rank[envs[i].Name]
			rj, okj := rank[envs[j].Name]
			switch {
			case oki && okj && ri != rj:
				return ri < rj
			case oki != okj:
				return oki // known environments before invented ones
			default:
				return envs[i].Name < envs[j].Name
			}
		})
		api := *b.api
		api.Envs = envs
		out = append(out, api)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Domain < out[j].Domain
	})
	return out
}

// EnvRank exposes the pipeline ordering so the UI can sort a list of
// environment names the same way the sidebar does.
func EnvRank(env string) int {
	if r, ok := rank[env]; ok {
		return r
	}
	return len(rank)
}

// --- host shapes -----------------------------------------------------------

// hostname strips the port from a host, leaving IPv6 brackets intact enough to
// recognize. It is deliberately tolerant: this runs over whatever the user
// typed, not over something a parser has already blessed.
func hostname(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") {
		if end := strings.Index(host, "]"); end > 0 {
			return host[1:end]
		}
		return strings.TrimPrefix(host, "[")
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		if rest := host[i+1:]; rest == "" || isDigits(rest) {
			host = host[:i]
		}
	}
	return host
}

func isLoopback(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	// .local and .test are reserved for exactly this, and .localhost is the
	// spelling Docker Compose setups tend to use.
	return strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".test") ||
		strings.HasSuffix(host, ".localhost")
}

func isIP(host string) bool {
	if strings.Contains(host, ":") {
		return true // an IPv6 literal; hostname() has already unwrapped it
	}
	parts := strings.Split(host, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 || !isDigits(p) {
			return false
		}
	}
	return true
}

func isPrivateIP(host string) bool {
	return strings.HasPrefix(host, "10.") ||
		strings.HasPrefix(host, "192.168.") ||
		strings.HasPrefix(host, "172.16.") ||
		strings.HasPrefix(host, "172.17.") ||
		strings.HasPrefix(host, "172.18.") ||
		strings.HasPrefix(host, "172.19.") ||
		strings.HasPrefix(host, "172.2") ||
		strings.HasPrefix(host, "172.30.") ||
		strings.HasPrefix(host, "172.31.")
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// matchLabels splits a hostname prefix on the given separators and returns the
// environment of the first label that is exactly a known marker.
func matchLabels(prefix, separators string) string {
	for _, label := range strings.FieldsFunc(prefix, func(r rune) bool {
		return strings.ContainsRune(separators, r)
	}) {
		for _, m := range markers {
			if label == m.fragment {
				return m.env
			}
		}
	}
	return ""
}
