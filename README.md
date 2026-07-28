# kubectl bai-config

A tool that generates a kubeconfig for access to BrightAI Kubernetes clusters via Okta OIDC.
Can be installed as a `kubectl` plugin via `krew`.

# Usage

Most people want every cluster in their kubeconfig, so this is the usual invocation — it skips
the interactive picker entirely and writes them all to `~/.kube/config`:

```shell
$ kubectl bai-config --write-all
```

Run it with no flags instead if you want the interactive picker and a hand-picked subset.

Full synopsis:

```shell
$ kubectl bai-config [--auth auto|api|browser] [--select-all] [--write-all] [--kubeconfig <path>] [--backup=false]
```

`--select-all` starts the interactive cluster list with every cluster pre-selected.

`--kubeconfig <path>` overrides where `--write-all` writes (default `~/.kube/config`).

An existing kubeconfig is backed up next to itself with a `__yyyy_mm_dd__hh_mm_ss` suffix before overwriting; disable with `--backup=false`.

`--auth` picks the Spacelift login: `api` uses your current `spacectl` profile (`spacectl profile login`), `browser` opens the web login, and `auto` (default) uses the profile when present and falls back to the browser.

On macOS the generated kubeconfig points kubelogin at `~/.kube/bai-browser-open` (written by this tool), which opens the hourly Okta OIDC login in the background (`open -g`) instead of stealing focus. If a login needs interaction (expired Okta session, MFA), check your browser for the background tab.

## Installation
You can download an archive file from [GitHub Releases](https://github.com/BrightDotAi/kubectl-bai-config/releases), then extract it and install a binary.

### Where the plugin archives are served from

The krew manifest points at `raw.githubusercontent.com`, on the custom ref
`refs/artifacts/<version-with-dashes>` under `artifacts/bai-config/<version>/`. Releases still
carry the same archives for direct download, but krew does not use them: GitHub returns 404 for
release assets on a private repository under every form of token auth, whereas
`raw.githubusercontent.com` honours a PAT. This keeps installs working unchanged if the
repository is ever made private.

The index itself moves only by pull request. `main` is PR-only under the org's SOC2 ruleset,
which has no bypass actors, so the release workflow cannot push `plugins/bai-config.yaml`
directly. It publishes the archives first, verifies every checksum over the network, and only
then opens a manifest PR — a release that fails midway leaves an unmerged PR rather than an
index pointing at archives that were never uploaded.

`refs/artifacts/*` deliberately sits outside `refs/heads/*`. Krew clones this repository as its
index with a plain `git clone`, which fetches every branch — so archives on a branch would land
on every user's disk on each `kubectl krew update`. A custom ref namespace is never fetched,
while `raw.githubusercontent.com` still serves it. Two constraints follow: the ref name must be
dot-free, or raw cannot tell where the ref ends and the path begins, and only the most recent
few refs are kept, since deleting one makes its blobs unreachable.

If the repository is private, generate a fine-grained PAT with read-only `contents` and
`metadata`, add it to `~/.netrc`, and pass `--enable-netrc`:

```text
machine raw.githubusercontent.com
  login token
  password <fine-grained github PAT>
```

```shell
$ kubectl krew install --enable-netrc bai-config/bai-config
```

The flag is needed on `kubectl krew upgrade` too. Note that git LFS is not an option here:
`raw.githubusercontent.com` serves the pointer file rather than the object, and krew has no way
to resolve it.

## Installation as kubectl plugin

You can also use kubectl-bai-config as kubectl plugin. The name as kubectl plugin is `bai-config`.

1. Install [krew](https://github.com/kubernetes-sigs/krew) that is a plugin manager for kubectl
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
Using spacectl profile credentials

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

Without a `spacectl` profile it falls back to the browser login and prints
`Opening browser to https://brightdotai.app.spacelift.io/cli_login?key=...` instead of the
first line. After confirming, it prompts for the kubeconfig path — `[tab]` fills the suggestion.

## Releasing

Releases are driven by merging to `main`; there is no tag to push by hand.

1. **Merge a PR into `main`.** Its labels choose the version bump — `major`, or `minor` /
   `enhancement`, otherwise patch. Label it `no-release` to skip publishing entirely.
2. **`auto-release.yaml` runs on the merge commit** and does, in order:
   - release-drafter publishes the GitHub release and its tag
   - GoReleaser builds the five platform archives, uploads them to the release, and generates
     the krew manifest **without publishing it**
   - the archives are pushed to `refs/artifacts/<tag-with-dashes>`
   - every URI in the generated manifest is re-downloaded and its sha256 compared
   - a `chore/krew-manifest-<tag>` PR is opened against `main`
   - artifact refs are pruned to the most recent three
3. **Merge the manifest PR.** This is the moment the new version becomes installable — until
   then the index still points at the previous release.
4. Users pick it up with `kubectl krew update && kubectl krew upgrade bai-config`.

The split exists so a half-finished release cannot break installs: the index only moves after
the archives are proven fetchable. If the workflow fails at any step before the manifest PR,
`main` is untouched and users stay on the previous version — **do not merge a manifest PR from
a failed run**. Re-running the job is safe and idempotent; the artifact ref is force-updated,
so a retry repairs rather than conflicts.

`main` is PR-only under the org's SOC2 ruleset, which is why step 3 is a merge and not an
automatic push.

## Development: Build and Run

```shell
$ goreleaser build --single-target --snapshot --clean
$ ./dist/kubectl-bai-config_darwin_arm64_v8.0/kubectl-bai-config
```

Requires GoReleaser v2 (`--rm-dist` was removed in v2 in favour of `--clean`). To check the
release config or produce all five platform archives locally without publishing:

```shell
$ goreleaser check
$ goreleaser release --snapshot --clean
```
