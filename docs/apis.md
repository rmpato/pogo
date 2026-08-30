# APIs, environments and variables

<sub>[Docs](README.md) · [Keys](keybindings.md) · **APIs** · [Security](security.md) · [Architecture](architecture.md)</sub>

## One API is one domain

`api.acme.com`, `api.staging.acme.com` and `dev-api.acme.com` are one API in
three environments. pogo reads that off the hostname and groups history
accordingly, with no setup at all:

```
$ pogo api
acme.com                        9 requests
  prod      api.acme.com          3
  staging   api.staging.acme.com  4
  dev       dev-api.acme.com      2
stripe.com                      2 requests
  prod      api.stripe.com        2
localhost                       1 requests
  local     localhost:3000        1
```

The API is the **registrable domain** — the part you could buy — resolved
against the public suffix list, so `api.acme.co.uk` is `acme.co.uk` rather than
`co.uk`, and `you.github.io` is its own API rather than everyone's. IP
addresses, `localhost` and single-label hostnames are their own APIs; there is
nothing above them to group by.

The environment is read from what is left over:

| in the hostname | environment |
|---|---|
| `staging`, `stage`, `stg` | `staging` |
| `preprod`, `pre-prod` | `preprod` |
| `dev`, `develop` | `dev` |
| `test`, `testing`, `qa`, `uat`, `integration` | `test` |
| `sandbox`, `sbx` | `sandbox` |
| `localhost`, `127.0.0.1`, `*.local`, `*.test`, a private IP | `local` |
| nothing at all, or `prod`, `production`, `live`, `api`, `www` | `prod` |

Both separators are read, so `api.staging.acme.com` and `dev-api.acme.com` are
both understood. Only the labels **above** the registrable domain are examined,
so a company that is actually called Staging is not mistaken for an environment:
`staging.co.uk` is production.

## Correcting it

Every line above is a guess, and a guess you cannot argue with is a bug you have
to live with. All of them are overridable, from the UI (`A`) or the command
line, and an override wins from then on — including backwards, over history
already recorded:

```bash
pogo api pin api-2.acme.com staging    # it is staging; stop guessing
pogo api move localhost:3000 acme.com  # this is our API, running locally
pogo api name acme.com Acme            # call it what you call it
pogo api hide cdn.acme.com             # keep it out of the sidebar
```

Each has an empty form that goes back to the guess (`pogo api pin host` with no
environment). Overrides live in `config.yaml`, so they travel with your machine
rather than with your history.

In the UI, `A` opens the same thing with the keys beside it: `p` pins every host
in an environment, `n` names an API, `x` hides one, `⏎` shows its requests.

## Projects: naming an API before pogo can guess it

The one API-per-domain rule breaks down on `localhost`: three services on
three ports are one guessed API, `localhost`, not three. A project is an API
you declare instead of one pogo works out, so a prefix says which is which:

```bash
pogo project add checkout localhost:3000
pogo checkout curl /orders    # → http://localhost:3000/orders, filed under checkout
pogo checkout list            # history, filtered to checkout
pogo checkout                 # the UI, filtered to checkout
pogo new                      # a blank request to fill in and run
```

The first request run under a name pogo has not seen creates the project on
the spot — `pogo project add` is only needed to give one a base URL up front,
or to declare one before its first request. A project run against a loopback
or private host binds that host to it permanently, the same as `pogo api move`
would by hand, so an unprefixed `pogo curl http://localhost:3000/…` keeps
landing in the same place afterward. A project run against a public host is
just a label — `pogo checkout curl https://api.stripe.com/…` groups under
`checkout`, and stripe.com is never touched.

`pogo project list` shows every declared project, its base URL, the hosts
bound to it, and how many requests it has. `pogo project rm <name>` forgets
it and unbinds its hosts; the history already filed under it stays exactly
where it is.

## Searching by it

```
api:acme.com        one API, however many hosts it has
env:staging         one environment of it, across every API
api:acme env:prod   both
```

Both match on substrings, so `api:acme` finds `acme.com` without the suffix.
`t` cycles grouping: by API, chronological, by host, by collection.

## Variables

A request written with variables runs against real values and is **stored with
the braces intact**:

```bash
pogo curl -H "Authorization: Bearer {{token}}" '{{base}}/users/42'
```

curl receives `Authorization: Bearer sk-live-…` and
`https://api.acme.com/users/42`. Your history file records:

```json
{"method":"GET","url":"{{base}}/users/42",
 "headers":[{"name":"Authorization","value":"Bearer {{token}}"}]}
```

Two problems solved at once. The token never lands in `history.jsonl` — the
thing [docs/security.md](security.md) warns about most. And a replay six weeks
later resolves the variable *again*, so it uses the token you have now instead
of the expired one you captured.

### A global name, per-API values

An environment **name** is global: "staging" means the same word everywhere.
What it points at belongs to one API. So `{{base}}` is acme's staging host for
an acme request and the payments team's for a payments one, and neither of them
has to be called `acme_staging_base`.

```bash
pogo env set staging --api acme.com base=https://api.staging.acme.com
pogo env set staging --api acme.com token=sk_test_9f2b1c
pogo env set prod    --api acme.com base=https://api.acme.com
pogo env set staging ua=pogo/1.0        # no --api: shared by everything
pogo env use staging
```

Values resolve by layering: the shared set for that environment first, then the
API's own on top, because the more specific statement is the one you meant.

```yaml
# ~/.config/pogo/environments.yaml — mode 0600; it holds what history does not
active: staging
shared:
  staging:
    ua: pogo/1.0
apis:
  acme.com:
    prod:
      base: https://api.acme.com
      token: sk_live_…
    staging:
      base: https://api.staging.acme.com
      token: sk_test_…
```

`POGO_ENV_FILE` moves the file; useful for keeping work and personal
environments apart. `pogo env list` prints it with the values masked, and
`--reveal` prints them in full.

### Which API's values?

Usually the URL says: pogo reads the host and looks up that API. When the URL is
itself a variable — `{{base}}/users`, which is the whole reason to write one —
there is no host to read, so pogo asks in order:

1. `--pogo-api acme.com` on the command,
2. `POGO_API` in the environment,
3. whether **exactly one** API defines every variable the command mentions.

That last rule covers the common case, where one API owns `base` and `token`. It
is deliberately narrow: two candidates means pogo picks neither and leaves the
braces in, so the request fails loudly instead of quietly calling the wrong
environment.

### Choosing an environment

```bash
pogo curl --pogo-env prod '{{base}}/users'   # this command only
POGO_ENV=prod pogo curl '{{base}}/users'     # this shell
pogo env use prod                            # from now on
```

In the UI, press `E`; the choice is written back to the file, so the next
`pogo curl` in your terminal uses it too. The active environment appears in the
header.

### Replaying across environments

This is the reason to bother:

```
E → staging → r
```

Select a request you made against production, switch environment, replay. Same
request, different target, and both are in your history side by side — press `d`
on the two to see exactly how the environments differ.

### Undefined variables

A reference with no value is left exactly as written:

```
$ pogo curl '{{nosuchvar}}/x'
pogo: undefined variable(s): {{nosuchvar}}
curl: (3) URL rejected: Bad hostname
```

The alternative — substituting an empty string — turns `{{base}}/users` into
`/users` and produces a failure with no visible cause. In the UI, the editor
lists every variable a request references and marks the ones the active
environment cannot supply, before you run it.

### Syntax

`{{name}}`, with optional inner whitespace (`{{ name }}`). Names may contain
letters, digits, `_`, `.` and `-`.

Substitution is textual and applies to every argument, so variables work in
URLs, headers, bodies and any other flag value. There is no recursion: a value
containing `{{...}}` is inserted literally.

A command with no `{{` in it is passed through byte for byte, so enabling
environments cannot change what an existing command does.

## Collections

APIs are worked out for you. Collections are the thing you decide: stars answer
"this one matters", collections answer "these ones go together".

```
c                  file the selected request under a name
collection:auth    filter to it
t                  cycle grouping until it groups by collection
```

A collection is a plain name, not a hierarchy. The aim is finding things again,
not modelling a filing cabinet. Imports get one automatically.

## Importing from a browser

Devtools → Network → *Save all as HAR*, then:

```bash
pogo import-har ~/Downloads/api.acme.com.har
pogo import-har ~/Downloads/api.acme.com.har --collection checkout
```

Every request arrives with its headers and body, rendered as a curl command you
can inspect, edit and replay — and grouped under the API it belongs to like
anything else. This is the short path from "it works in the browser but not from
my terminal" to a diff of the two.

Imported entries are marked `import`, so they are never mistaken for something
pogo ran. Browser pseudo-headers (`:authority`, `:method`) and hop-by-hop fields
curl sets itself are dropped; everything else is kept, including the
`Authorization` header — which lands in your history like any other captured
credential.
