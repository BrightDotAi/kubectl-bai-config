# kubctl bai-config

A tool that generates a kubeconfig for access to BrightAI Kubernetes clusters via Okta OIDC.
Can be installed as a `kubectl` plugin via `krew`.

## Installation
You can download an archive file from [GitHub Releases](https://github.com/BrightDotAi/kubectl-bai-config/releases), then extract it and install a binary.

## Installation as kubectl plugin

You can also use ksort as kubectl plugin. The name as kubectl plugin is `bai-config`.

1. Install [krew](https://github.com/GoogleContainerTools/krew) that is a plugin manager for kubectl
2. Run:

        kubectl krew install bai-config

3. Try it out

        kubectl bai-config

## License

This software is released under the MIT License and includes the work that is distributed in the Apache License 2.0.