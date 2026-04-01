# Jumpgate

SSH relay access manager. Connects a local workstation to a remote host
through a gate (jump host), with optional Kerberos authentication,
multi-context support, and hook-based platform abstraction.

```
    LOCAL                          GATE                     REMOTE
      :                             :                         :
      ╔═════════════════════════════╪══╗
      ║                          ┌──┼──╫──────────────────────┐
      ║                          │  :  ║                      │
 ··· ssh session ···················:··:·························> :22
  gate ───────────────────────────> : <─────────────────────── relay
      ║                          │  :  ║                      │
      ║                          └──┼──╫──────────────────────┘
      ╚═════════════════════════════╪══╝
      :                             :                         :

   ═══  gate session (SSH ControlMaster, local → gate)
   ───  relay tunnel  (reverse SSH tunnel, remote → gate)
   ···  user SSH session (ProxyJump through gate, local → remote:22)
```

## Installation

### Homebrew (macOS/Linux)

```bash
brew install cloudygreybeard/tap/jumpgate
```

### Quick install (Linux/macOS)

```bash
curl -sL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh
```

### Quick install (Windows PowerShell)

```powershell
irm https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.ps1 | iex
```

### Go install

```bash
go install github.com/cloudygreybeard/jumpgate@latest
```

### From source

```bash
git clone https://github.com/cloudygreybeard/jumpgate
cd jumpgate
make install
```

## Quick start

```bash
# Bootstrap a new remote (one command on each side)
jumpgate bootstrap               # local: payload + auth + wait + push config
jumpgate bootstrap               # remote: paste payload + embedded server + relay

# One-time setup from a site pack
jumpgate init --from ~/my-site-pack   # render config, hooks, SSH — all in one
jumpgate connect                      # gate + auth + wait for remote

# Or manual setup
jumpgate setup                    # Create config dir with example config
$EDITOR ~/.config/jumpgate/config.yaml
jumpgate setup ssh                # Generate SSH config from templates
jumpgate setup credentials        # Store credentials (interactive)
jumpgate connect

# Daily use (local)
jumpgate connect                  # Gate + auth + wait for remote
jumpgate connect --context lab    # Different context
jumpgate status                   # Show connection status
jumpgate disconnect               # Close gate + destroy ticket

# Daily use (remote)
jumpgate connect                  # Register relay with gate
jumpgate watch                    # Monitor with heartbeat
jumpgate disconnect               # Close relay
```

## Commands

### Connection

```
jumpgate connect [CONTEXT]                Gate + auth + poll for remote
jumpgate connect --relay-port PORT        Override relay port for this session
jumpgate disconnect [CONTEXT]             Close session, destroy ticket
jumpgate disconnect --all [CONTEXT]       Also tear down the remote relay first
jumpgate status [CONTEXT]                 Show gate / auth / remote status
jumpgate watch [CONTEXT]                  Monitor relay with heartbeat (remote)
jumpgate watch --relay-port PORT          Override relay port for this session
```

### Configuration

```
jumpgate config list                 List all contexts (* = default)
jumpgate config current              Show the current default context
jumpgate config use <CONTEXT>        Set the default context
jumpgate config create <CONTEXT>     Create a new context (--from to clone)
jumpgate config delete <CONTEXT>     Delete a context
jumpgate config rename <OLD> <NEW>   Rename a context
jumpgate config edit                 Open config.yaml in $EDITOR
jumpgate config view [CONTEXT]       Dump resolved configuration
jumpgate config import [FILE]        Import a context from JSON/YAML (or stdin)
jumpgate config export [CONTEXT]     Export a context as a site pack directory
jumpgate config migrate              Check config format, print guidance
```

### Bootstrap

```
jumpgate bootstrap [CONTEXT]         One-command setup (works on both local and remote)
jumpgate bootstrap --reinit          Re-prompt for bootstrap payload on remote
jumpgate bootstrap --server-only     Run embedded SSH server only (remote, no relay)
```

### Setup

```
jumpgate init                        Bootstrap config from defaults
jumpgate init --from <dir>           Bootstrap from a site pack directory
jumpgate init --paste                Bootstrap remote from pasted payload (alt path)
jumpgate setup                       Interactive first-time setup
jumpgate setup config                Create config dir + hooks
jumpgate setup ssh                   Generate SSH configs from templates
jumpgate setup credentials           Run setup-credentials hook
jumpgate setup remote-init [CTX]     Generate bootstrap payload for a remote (alt path)
jumpgate setup remote [CTX]          Push full config/hooks to remote over SSH
```

### Other

```
jumpgate askpass                     SSH_ASKPASS helper (internal use)
jumpgate version                     Print version info
```

## Global flags

```
    --context CONTEXT    Override default context
-o, --output FORMAT      Output format: text, json, yaml, wide (default: text)
-v, --verbose            Verbose output (-v info, -vv debug)
-c, --config PATH        Override config file path
```

The `-o` flag controls output for data-producing commands:

- **`text`** (default) -- human-readable sectioned output
- **`json`** -- indented JSON, suitable for `jq` and scripts
- **`yaml`** -- YAML output
- **`wide`** -- extended text with additional columns (e.g. role, ports in `config list`)

Supported by: `config view`, `config list`, `status`.

## Configuration

Config lives at `~/.config/jumpgate/config.yaml`
(or `$XDG_CONFIG_HOME/jumpgate/config.yaml`).

```yaml
default_context: work

contexts:
  work:
    role: local                # local (initiator) or remote (relay endpoint)
    gate:
      hostname: gateway.example.com
      port: 22
    auth:
      type: kerberos           # kerberos | key | none
      realm: EXAMPLE.COM
      user: alice
    remote:
      user: alice
      key: ~/.ssh/id_ed25519
    relay:
      remote_port: 0               # 0 = auto-generate on first connect
```

### Context UID

Each context is automatically assigned a unique identifier (`uid`) when
created. The UID is a UUIDv4 used for filesystem artifacts where uniqueness
and privacy matter -- Kerberos credential caches (`~/.cache/jumpgate/krb5cc_<uid>`)
and relay marker files on shared hosts (`~/.jumpgate/relay-<uid>.port`).
Existing configs without UIDs receive one automatically on first load.

The human-readable context name remains the primary interface for SSH aliases,
commands, display output, and hooks.

### Role

Each context declares a `role` that determines its behavior:

- **`local`** (default) -- the initiating end. Opens a gate session, authenticates,
  and polls for the remote host.
- **`remote`** -- the relay endpoint. Connects to the gate and registers a reverse
  tunnel, then monitors with `jumpgate watch`.

The same binary runs on both ends. The role is determined by config, not by
the operating system -- any platform can serve either role.

### Relay port

The relay port (`relay.remote_port`) is the port registered on the gate host
for the reverse tunnel. Set it to `0` in your config and jumpgate will
auto-generate a random port in the ephemeral range (49152--65535) on first
`connect` and persist it back to `config.yaml` so both sides agree.

To temporarily override the port without changing config:

```bash
jumpgate connect --relay-port 55000
```

Before opening the relay, jumpgate probes the gate host to check whether the
chosen port is already in use. If it is, the command fails with a suggestion
to use `--relay-port`.

#### Auto-discovery

When the remote opens a relay, jumpgate writes a marker file on the gate
(`~/.jumpgate/relay-<uid>.port`) containing the active port number. On
the local side, `jumpgate connect` reads this marker before probing, so the
local end automatically discovers port changes made at the remote end. If
the marker port differs from the local config, the local config and SSH
config are updated automatically. The marker file is cleaned up on
`jumpgate disconnect`. The marker uses the context's UID (not its name) so
that human-readable names are not exposed on shared hosts.

See [cmd/embed/config.yaml.example](cmd/embed/config.yaml.example) for all
options.

## Hooks

Platform-specific operations are delegated to executable hook scripts in
`~/.config/jumpgate/hooks/` (global) or `~/.config/jumpgate/contexts/<name>/hooks/`
(per-context). The CLI searches per-context hooks first, then global hooks.

Available hooks:

| Hook | When it runs |
|------|-------------|
| `check-credentials` | Before connect -- verify credentials exist |
| `setup-credentials` | During `setup credentials` -- interactive credential store |
| `get-gate-token` | Before gate SSH -- return auth token on stdout |
| `get-krb-password` | Before `kinit` -- return Kerberos password on stdout |
| `load-ssh-key` | Before remote connect -- load SSH key into agent |
| `pre-poll` | Once when remote polling starts |
| `on-poll-tick` | Each poll iteration while waiting for remote |

Each hook receives context configuration as environment variables
(`JUMPGATE_CONTEXT`, `GATE_HOSTNAME`, `AUTH_USER`, etc.).

## SSH config templates

Jumpgate generates SSH client configuration from Go templates in
`~/.config/jumpgate/ssh/snippets/`. Embedded defaults are provided;
user-supplied templates in the config dir overlay or extend them.

Template variables: `{{.Context}}`, `{{.Hostname}}`, `{{.User}}`, `{{.Port}}`,
`{{.SocketDir}}`, `{{.RemoteUser}}`, `{{.RelayPort}}`, `{{.RemoteKey}}`.

## Site packs

A site pack is a directory (typically a git repo) that bundles config
templates, hooks, SSH snippets, and a value schema for a particular
environment. Running `jumpgate init --from <dir>` renders the pack into
`~/.config/jumpgate/` and generates SSH config automatically.

```
my-site-pack/
  site.yaml              # pack metadata + value schema with defaults
  values.yaml            # your answers (flat key: value pairs, gitignored)
  values.example.yaml    # example for users to copy and fill in
  templates/             # Go text/template files (*.tpl → config dir)
  hooks/                 # shell scripts, copied and made executable
  snippets/              # SSH config fragments, overlaid on defaults
  windows/               # optional Windows integration scripts
```

The `site.yaml` defines the value schema. Each entry has a `key`, a `prompt`
for interactive mode, and an optional `default`. If `values.yaml` is missing,
`jumpgate init` prompts interactively.

Create a site pack for your org, share it internally, and users onboard with:

```bash
git clone <your-site-pack-repo> ~/jumpgate-config
cp ~/jumpgate-config/values.example.yaml ~/jumpgate-config/values.yaml
$EDITOR ~/jumpgate-config/values.yaml
jumpgate init --from ~/jumpgate-config
jumpgate connect
```

## Remote bootstrap

Setting up a new remote is a single command on each side. The local
generates a bootstrap payload, authenticates, and waits. The remote
pastes the payload, starts a temporary embedded SSH server, and opens
a relay tunnel. When the local detects the remote, it pushes the full
config automatically.

### On the local workstation

```bash
jumpgate bootstrap
```

This generates a compact base64 payload, prints install instructions for
the remote, authenticates (gate + Kerberos), and waits for the remote to
appear on the relay.

### On the remote

Install jumpgate and run `jumpgate bootstrap`. When prompted, paste the
payload string displayed on the local side.

```bash
# Linux / WSL
curl -sL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh
jumpgate bootstrap
```

```powershell
# Windows PowerShell (pre-WSL bootstrap)
irm https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.ps1 | iex
jumpgate bootstrap
```

The remote starts a minimal embedded SSH server on `localhost:2222`
(public key auth only, exec-only, loopback-bound) and opens a relay
tunnel through the gate. On Windows, the installer automatically adds
`$HOME\bin` to PATH.

### What happens next

The local detects the remote through the relay and pushes the full
configuration, hooks, SSH snippets, and Windows integration scripts as a
single compressed archive over a native Go SSH connection. The embedded
server extracts the bundle in-process.

The embedded server is a one-time bootstrap mechanism — once sshd is
installed on the remote, `jumpgate connect` is used instead.

### Alternate path (manual)

The `jumpgate bootstrap` flow combines several standalone commands that
can also be used individually:

```bash
# Local: generate payload only
jumpgate setup remote-init myhost

# Remote: paste payload + start relay separately
jumpgate init --paste
jumpgate connect          # if sshd is available
# or
jumpgate bootstrap        # if no sshd yet (embedded server)

# Local: push config only (after relay is up)
jumpgate setup remote myhost
```

## Config import and export

Contexts can be shared as structured data using the output format flag
and the import/export commands.

**Clone a context:**

```bash
jumpgate config view -o json work | jumpgate config import --context staging
```

**Export to a site pack:**

```bash
jumpgate config export --output-dir ~/my-site-pack
```

This generates a complete site pack directory (site.yaml, values, templates,
hooks, snippets) that others can use with `jumpgate init --from`.

**Import from a file:**

```bash
jumpgate config import --context lab context.json
cat context.yaml | jumpgate config import --context lab
```

Accepts the full `config view` envelope or a bare context object, in JSON
or YAML (auto-detected).

## Security

Jumpgate manages SSH sessions, Kerberos tickets, and relay tunnels. It
takes the following measures to handle credentials and connections
responsibly:

- **No credentials stored on disk.** Jumpgate does not write passwords,
  tokens, or private keys to its config. Kerberos passwords are passed
  to `kinit` via stdin pipe (never as command-line arguments).   Gate tokens are passed to `ssh` via environment variable and never
  written to disk. Helper scripts are written to a 0700 temp directory
  but contain no secrets.
- **Isolated host key management.** Relay connections use a dedicated
  known_hosts file (`~/.config/jumpgate/known_hosts`), separate from
  `~/.ssh/known_hosts`. Jumpgate never modifies the user's global SSH
  trust store.
- **Bootstrap server hardening.** The embedded SSH server used during
  remote bootstrap binds to `127.0.0.1` only (not network-accessible),
  authenticates with a single authorized public key, and enforces a
  command allowlist. It is a one-time initialisation mechanism, not a
  permanent service.
- **Input validation.** Context names and UIDs are validated against a
  strict character class at load time to prevent shell injection in
  remote commands.
- **Loopback-only port forwards.** All local port forwards (including
  Kerberos KDC tunnels) bind to `127.0.0.1`.

### Things to be aware of

- **Bootstrap payload is not signed.** The base64 string pasted during
  `jumpgate bootstrap` carries configuration data (gate hostname, auth
  user, relay port, public key) but has no cryptographic integrity
  protection. Transfer it through a trusted channel. Signing is on the
  roadmap.
- **Hook scripts run with your environment.** Hooks in
  `~/.config/jumpgate/hooks/` execute with the full parent process
  environment. Only use hooks from trusted sources.
- **Config files are world-readable.** `config.yaml` is created with
  mode 0644. It contains hostnames, usernames, and relay ports (no
  secrets). On shared systems, consider tightening permissions
  (`chmod 600 ~/.config/jumpgate/config.yaml`).
- **The embedded bootstrap server allows exec.** While gated by an
  allowlist and single-key auth, the bootstrap server can execute
  commands as the current user. Run `jumpgate bootstrap` only when
  actively bootstrapping, and stop it promptly afterward.

### Reporting vulnerabilities

If you discover a security issue, please report it privately via
[GitHub Security Advisories](https://github.com/cloudygreybeard/jumpgate/security/advisories)
or by email rather than opening a public issue.

## Roadmap

Near-term improvements planned or in progress:

- **Bootstrap payload signing** — cryptographic integrity protection
  for the bootstrap payload, with optional verification hash for
  out-of-band confirmation.
- **Config versioning** — `apiVersion` and `kind` fields in
  `config.yaml` (following the Kubernetes resource manifest pattern)
  for explicit version detection, structured migration, and forward
  compatibility.
- **Configurable command allowlist** — allow users to extend the
  embedded bootstrap server's command allowlist via config for
  advanced workflows.
- **Native Go SSH client for regular setup** — extend the native
  `golang.org/x/crypto/ssh` transport (currently used for bootstrap)
  to `jumpgate setup remote` for consistent performance when sshd is
  running.
- **WSL config sharing** — automatic config discovery between Windows
  and WSL on the same host, so both environments share a single
  jumpgate configuration.
- **Environment scrubbing** — minimal environment allowlist for
  commands executed by the embedded bootstrap server.

## Development

```bash
make build    # Build the binary
make test     # Run tests
make lint     # Run linter
make clean    # Remove build artifacts
```

## License

Apache 2.0. See [LICENSE](LICENSE).
