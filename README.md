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

Use the Go version declared in `go.mod` and golangci-lint v2.14.

- `make test` runs tests with the race detector.
- `make lint` checks Go code and formatting.
- `make verify` checks module consistency, vulnerabilities, and workflow syntax.
- `make ko-local` builds a local container image with ko.

CI runs these checks on pull requests and pushes to main.
The vulnerability check uses the latest govulncheck release, and setup-ko
installs the latest ko release.
Release automation uses the latest GoReleaser release and publishes `v*` tags.
It builds archives and container images, generates an SBOM, and signs the image
digest with cosign. Crane and cosign use their installer defaults. Merge
dependent maintenance PRs in stack order.

### Running locally

Set `GITHUB_TOKEN` in your environment, then run `go run .` and open
`http://localhost:3000`. Templates default to `kodata`; set `KO_DATA_PATH` to
use a different data directory. Container builds set this path automatically.
Missing or invalid templates are reported at startup.

Requests to GitHub have a 30-second deadline. API failures return HTTP 502,
timeouts return HTTP 504, and rate limits return HTTP 503 with `Retry-After`
when GitHub provides a reset time. Failed fetches are not cached.
