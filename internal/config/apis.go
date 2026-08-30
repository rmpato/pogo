package config

import (
	"sort"
	"strings"
)

// APIRegistry is what the user has told pogo about the hosts in its history.
//
// pogo groups requests by API on its own: hosts that share a registrable
// domain are one API, and the subdomain usually says which environment a host
// is (see internal/apis). Those are guesses, and a guess that cannot be
// corrected is worse than no guess at all — so every inference has an override
// here, written the moment the user makes it and authoritative from then on.
//
// The maps are keyed by lowercase host or domain, which is how they arrive
// from a URL.
type APIRegistry struct {
	// Names gives an API a display name: "acme.com" → "Acme". Without one,
	// pogo shows the domain.
	Names map[string]string `yaml:"names,omitempty"`

	// Domains reassigns a host to an API it would not otherwise belong to —
	// the case the eTLD+1 rule cannot get right on its own, like a partner
	// host on a different domain, or a bare "localhost:3000" that is really
	// your own API running locally.
	Domains map[string]string `yaml:"domains,omitempty"`

	// Envs pins a host to an environment: "api-2.acme.com" → "staging".
	Envs map[string]string `yaml:"envs,omitempty"`

	// Hidden lists API domains kept out of the sidebar and the grouped list.
	// A CDN or a telemetry endpoint is noise once you have seen it once.
	Hidden []string `yaml:"hidden,omitempty"`

	// Projects are the APIs a user declared by name — "checkout" — rather than
	// the ones pogo inferred from a host. The value is a base URL, so
	// `pogo checkout curl /orders` has something to resolve against; it may be
	// empty when the project exists only as a label so far.
	Projects map[string]string `yaml:"projects,omitempty"`
}

// Name returns the display name for a domain, or the domain itself.
func (r APIRegistry) Name(domain string) string {
	if n, ok := r.Names[strings.ToLower(domain)]; ok && n != "" {
		return n
	}
	return domain
}

// DomainFor returns the API a host has been reassigned to, if any.
func (r APIRegistry) DomainFor(host string) (string, bool) {
	d, ok := r.Domains[strings.ToLower(host)]
	return d, ok && d != ""
}

// EnvFor returns the environment a host has been pinned to, if any.
func (r APIRegistry) EnvFor(host string) (string, bool) {
	e, ok := r.Envs[strings.ToLower(host)]
	return e, ok && e != ""
}

// Project returns the base URL a project was declared with, and whether the
// name is a project at all. The base may be empty for a project that exists
// only as a label.
func (r APIRegistry) Project(name string) (string, bool) {
	base, ok := r.Projects[strings.ToLower(name)]
	return base, ok
}

// ProjectNames lists every declared project, alphabetically.
func (r APIRegistry) ProjectNames() []string {
	names := make([]string, 0, len(r.Projects))
	for name := range r.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsHidden reports whether an API has been hidden from the sidebar.
func (r APIRegistry) IsHidden(domain string) bool {
	for _, d := range r.Hidden {
		if strings.EqualFold(d, domain) {
			return true
		}
	}
	return false
}

// SetName records a display name, or clears it when name is empty.
func (r *APIRegistry) SetName(domain, name string) {
	r.Names = assign(r.Names, strings.ToLower(domain), name)
}

// SetDomain reassigns a host to an API, or clears the override.
func (r *APIRegistry) SetDomain(host, domain string) {
	r.Domains = assign(r.Domains, strings.ToLower(host), domain)
}

// SetEnv pins a host to an environment, or clears the pin.
func (r *APIRegistry) SetEnv(host, env string) {
	r.Envs = assign(r.Envs, strings.ToLower(host), env)
}

// SetHidden hides or unhides an API.
func (r *APIRegistry) SetHidden(domain string, hidden bool) {
	kept := make([]string, 0, len(r.Hidden)+1)
	for _, d := range r.Hidden {
		if !strings.EqualFold(d, domain) {
			kept = append(kept, d)
		}
	}
	if hidden {
		kept = append(kept, strings.ToLower(domain))
	}
	if len(kept) == 0 {
		kept = nil
	}
	r.Hidden = kept
}

// SetProject declares a project, or updates its base URL. Unlike the other
// setters, an empty base is a value worth keeping rather than a signal to
// clear the entry: a project can exist as a bare label before any request has
// given it a host.
func (r *APIRegistry) SetProject(name, base string) {
	name = strings.ToLower(name)
	if r.Projects == nil {
		r.Projects = map[string]string{}
	}
	r.Projects[name] = base
}

// RemoveProject forgets a project and every host reassigned to it, so removal
// leaves no dangling `pogo api move` behind it.
func (r *APIRegistry) RemoveProject(name string) {
	name = strings.ToLower(name)
	if r.Projects != nil {
		delete(r.Projects, name)
		if len(r.Projects) == 0 {
			r.Projects = nil
		}
	}
	for host, domain := range r.Domains {
		if strings.EqualFold(domain, name) {
			delete(r.Domains, host)
		}
	}
	if len(r.Domains) == 0 {
		r.Domains = nil
	}
}

// assign sets a key, deleting it when the value is empty so that clearing an
// override leaves no trace in the file rather than an empty string that reads
// as a deliberate blank name.
func assign(m map[string]string, key, value string) map[string]string {
	if value == "" {
		delete(m, key)
		if len(m) == 0 {
			return nil
		}
		return m
	}
	if m == nil {
		m = map[string]string{}
	}
	m[key] = value
	return m
}
