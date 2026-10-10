# GitHub Actions Dashboard

This simple project lets you track your CI jobs visually to see what jobs are passing, failing, or running.

It will show the past 12 hours of GitHub workflows that ran.

### Requirements

- A GitHub token to run authenticated requests (because it can run more API calls can be made), and if you need to access any private repo, you will need the `repo` permission.

### Roadmap and upcoming work

- [ ] Helm chart
- [ ] Example of how to deploy in Google Cloud Run

### Screenshots

![home](https://github.com/cpanato/github-actions-dashboard/blob/main/assets/home.png?raw=true)


![dashboard](https://github.com/cpanato/github-actions-dashboard/blob/main/assets/dashboard.png?raw=true)

### Development

Use the Go version declared in `go.mod` and golangci-lint v2.14.0.

- `make test` runs tests with the race detector.
- `make lint` checks Go code and formatting.
- `make verify` checks module consistency, vulnerabilities, and workflow syntax.
- `make ko-local` builds a local container image with ko v0.19.1.

CI runs these checks on pull requests and runs vulnerability checks weekly.
Release automation uses GoReleaser v2.18.3 and publishes `v*` tags. It builds
archives and container images, generates an SBOM, and signs the image digest
with cosign v3.1.3. Merge dependent maintenance PRs in stack order.
