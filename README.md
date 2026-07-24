# kubctl bai-config

A tool that generates a kubeconfig for access to BrightAI Kubernetes clusters via Okta OIDC.
Can be installed as a `kubectl` plugin via `krew`.

# Usage

```shell
$ kubectl bai-config [--auth auto|api|browser] [--select-all] [--write-all] [--kubeconfig <path>] [--backup=false]
```

`--select-all` starts the cluster list with every cluster pre-selected.

`--write-all` skips the interactive UI entirely and writes a kubeconfig with all clusters; `--kubeconfig <path>` overrides the destination (default `~/.kube/config`).

An existing kubeconfig is backed up next to itself with a `__yyyy_mm_dd__hh_mm` suffix before overwriting; disable with `--backup=false`.

`--auth` picks the Spacelift login: `api` uses your current `spacectl` profile (`spacectl profile login`), `browser` opens the web login, and `auto` (default) uses the profile when present and falls back to the browser.

On macOS the generated kubeconfig points kubelogin at `~/.kube/bai-browser-open` (written by this tool), which opens the hourly Okta OIDC login in the background (`open -g`) instead of stealing focus. If a login needs interaction (expired Okta session, MFA), check your browser for the background tab.

## Installation
You can download an archive file from [GitHub Releases](https://github.com/BrightDotAi/kubectl-bai-config/releases), then extract it and install a binary.

## Installation as kubectl plugin

You can also use kubectl-bai-config as kubectl plugin. The name as kubectl plugin is `bai-config`.

1. Install [krew](https://github.com/GoogleContainerTools/krew) that is a plugin manager for kubectl
2. Add this repository as a custom plugin index
```shell
$ kubectl krew index add bai-config https://github.com/BrightDotAi/kubectl-bai-config.git
$ kubectl krew index list
```
3. To install the plugin, run:
```shell
$ kubectl krew install bai-config/bai-config
```
4. Try it out
```shell
$ kubectl bai-config
Opening browser to https://brightdotai.app.spacelift.io/cli_login?key=<REDACTED>

Waiting for login...
Done!

OIDC Authentication Details:
app_oauth_client_id: <REDACTED>
auth_server_issuer_url: <REDACTED>

Use the right arrow key or spacebar to select clusters to add to the kubeconfig:
  [ ] cluster-0
  [x] cluster-1
> [x] cluster-2
  [ ] cluster-3

Press [enter] to confirm.

Press [q] to quit.
```

## Development: Build and Run

```shell
$ goreleaser build --single-target --snapshot --rm-dist
$ ./dist/kubectl-bai-config_darwin_arm64/kubectl-bai-config
```
