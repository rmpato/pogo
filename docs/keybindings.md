# Keybindings

<sub>[Docs](README.md) · **Keys** · [APIs](apis.md) · [Security](security.md) · [Architecture](architecture.md)</sub>

You should not need this page. `ctrl+k` opens a command palette that searches
everything pogo can do and shows each command's key beside it, so the shortcut
is learned by using it. `?` opens the same list as a reference, generated from
the same registry — it cannot go stale.

<img src="img/pogo-help.svg" alt="the pogo help screen" width="100%">

## Everywhere

| Key | Action |
|---|---|
| `ctrl+k` | Command palette — search every action by name |
| `H` | Home — the shell above the list |
| `q`, `ctrl+c` | Quit |
| `esc` | Back — leave a screen, close an overlay, or clear the search |
| `?` | Help |
| `S` | Reveal masked secrets (toggle) |

## The list

| Key | Action |
|---|---|
| `↑` `k` / `↓` `j` | Move |
| `tab` | Move between the sidebar and the list |
| `\` | Show or hide the sidebar |
| `p` | Show or hide the preview panel |
| `g` / `G` | Top / bottom |
| `ctrl+u` / `ctrl+d` | Half page up / down |
| `⏎` | Inspect |
| `/` | Search |
| `t` | Cycle grouping: by API → chronological → by host → by collection |
| `space` | Fold the group you are in; on a closed one, open it |
| `z` | Fold every group, or open every group |
| `A` | APIs and environments |
| `r` | Replay |
| `e` | Edit, then run |
| `n` | New request — a blank one, ready to fill in and run |
| `y` | Copy menu |
| `s` | Star / unstar |
| `c` | File under a collection |
| `E` | Switch environment |
| `u` | Install an available update |
| `x` | Delete |
| `d` | Mark for comparison; press `d` on a second request to diff |

## The sidebar

On terminals at least 108 columns wide, a sidebar lists what is in your
history: filters (all, starred, failed), the APIs you have called with their
environments nested underneath, and your collections — each with a count. `tab`
focuses it, `⏎` applies the row as a search — and the search box then shows the
query it ran, which is how the filter syntax below gets learned without reading
this page.

## The preview

Wider than 132 columns, a third pane down the right previews the request under
the cursor: where it went, the headers that carry meaning, the first of each
body, and a short list of things worth knowing before you open it — why it
failed in the API's own words, how often this endpoint has been called and how
many of those failed, where the time went, and what credential it is carrying.

It is deliberately not the inspector in miniature. `p` hides it.

## Search

Free text matches method, URL, status, note and error. Filters can be combined,
and repeating a filter ORs its values.

| Token | Matches |
|---|---|
| `users/42` | anything containing the text |
| `api:acme.com` | one API, however many hosts it has |
| `env:staging` | one environment, across every API |
| `method:POST` | one method |
| `status:404` | one status code |
| `status:4xx` | a status class |
| `host:api.example.com` | a host (substring) |
| `is:starred` | starred requests |
| `collection:auth` | filter by collection |
| `is:failed` | requests that failed or returned ≥ 400 |

Example: `method:POST status:5xx api:acme.com` — failing writes to one API,
in any of its environments.

Header *values* are deliberately not searched, so a search for `token` does not
match every authenticated request.

## Inspecting

| Key | Action |
|---|---|
| `tab` / `shift+tab` | Next / previous pane (also `l` / `h`) |
| `1` – `5` | Overview, Request, Response, Timing, Raw |
| `v` | Cycle the body view: tree → pretty → raw |
| `↑` `↓` | Move the JSON cursor in tree view, otherwise scroll |
| `space` | Fold or unfold the JSON node under the cursor |
| `g` / `G` | Top / bottom |

## Editing

`e` opens the request as fields: method, URL, query parameters, headers and
body.

| Key | Action |
|---|---|
| `↑` `↓` | Move between fields |
| `⏎` | Edit the focused field (or open the body) |
| `← →` | Change the method without typing |
| `ctrl+d` | Remove the focused header or query parameter |
| `ctrl+t` | Switch between fields and the raw curl command |
| `ctrl+e` | Hand the command to `$EDITOR` |
| `ctrl+r` (or `ctrl+⏎`) | Run it |
| `esc` | Cancel, or leave the field being edited |

Edits are applied to the **original command**, not used to regenerate one. A
request carrying `--cacert`, `--resolve`, `-k` or anything else pogo does not
model keeps every one of those options when you change a header. `ctrl+t` shows
the exact command your edits produce.

Running an edited command records a **new** entry pointing back at the one it
came from. The original is never modified.

Values are shown unmasked while editing — you cannot edit what you cannot see —
and the editor lists any `{{variables}}` the command references, marking the
ones the active environment cannot supply.

> `ctrl+⏎` only reaches an application in terminals that can distinguish it from
> `⏎`; `ctrl+r` works everywhere, which is why it is the documented binding.

## APIs and environments

`A` opens the workspace where pogo shows what it worked out about your hosts.

| Key | Action |
|---|---|
| `↑` `↓` | Move between APIs and their environments |
| `⏎` | Show that API's requests (or that one environment's) |
| `p` | Pin every host in the selected environment — stop guessing |
| `n` | Name the API |
| `x` | Hide or show the API |
| `esc` | Back to the list |

The same corrections are available from the command line as `pogo api pin`,
`pogo api move`, `pogo api name` and `pogo api hide`. See [APIs](apis.md).

## Home and settings

`H` leaves the list for the shell above it: Requests, APIs and environments,
Settings, the keyboard reference, and the walkthrough shown on a first run.

Settings holds the handful of decisions that are genuinely personal — theme,
what gets redacted, whether to check for releases — plus where every file lives.
Each change is written on the keypress that makes it; there is no save key,
because there is no unsaved state to get wrong.

## Copying

`y` opens the menu; press the letter, or move and press `⏎`.

| Key | Copies |
|---|---|
| `c` | The curl command |
| `C` | The curl command with secrets masked — safe to paste into an issue |
| `u` | The URL |
| `h` / `H` | Request / response headers |
| `b` / `r` | Request / response body |
| `j` | The whole entry as JSON |

Copying uses `pbcopy`, `wl-copy`, `xclip` or `xsel`, and falls back to an
OSC 52 escape so it also works over SSH and inside tmux.
