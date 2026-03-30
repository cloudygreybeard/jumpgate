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
# One-time setup from a site pack (3 commands to connect)
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

### Setup

```
jumpgate init                        Bootstrap config from defaults
jumpgate init --from <dir>           Bootstrap from a site pack directory
jumpgate init --paste                Bootstrap remote from pasted payload
jumpgate setup                       Interactive first-time setup
jumpgate setup config                Create config dir + hooks
jumpgate setup ssh                   Generate SSH configs from templates
jumpgate setup credentials           Run setup-credentials hook
jumpgate setup remote-init [CTX]     Generate bootstrap payload for a remote
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

Setting up the remote end of a jumpgate connection is a two-phase process
using clipboard paste (e.g. via Windows App).

### Phase 1: Bootstrap (get the relay running)

On the **local** workstation, generate a bootstrap payload:

```bash
jumpgate setup remote-init myhost
```

This prints a compact base64 string (~400 chars) containing the minimal
remote-role config. Copy it to the clipboard.

On the **remote** (e.g. a WSL terminal via Windows App), install jumpgate
and paste the payload:

```bash
curl -sL https://github.com/cloudygreybeard/jumpgate/releases/latest/download/install.sh | sh
jumpgate init --paste
# paste the base64 string, press Enter
jumpgate connect
```

The remote now has a relay tunnel open through the gate.

### Phase 2: Full orchestration (push everything)

Once the relay is up and `jumpgate connect` succeeds on the local side,
push the complete config, hooks, and Windows integration:

```bash
jumpgate setup remote myhost
```

This pushes the full remote config, hooks, SSH snippets, and Windows
Terminal shortcuts over the SSH tunnel, then runs `jumpgate setup ssh`
on the remote.

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

## Development

```bash
make build    # Build the binary
make test     # Run tests
make lint     # Run linter
make clean    # Remove build artifacts
```

## License

Apache 2.0. See [LICENSE](LICENSE).
