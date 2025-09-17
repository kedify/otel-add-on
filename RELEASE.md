## How to release KEDA OTel Add-on

This document outlines the steps to release a new versions of the [otel-add-on](https://github.com/kedify/otel-add-on/pkgs/container/otel-add-on) container image and [otel-add-on](https://github.com/kedify/otel-add-on/pkgs/container/charts%2Fotel-add-on) helm chart.

Versions of container image and helm chart are bound together and released together. Although it is possible to mix and match the versions, it is not recommended nor supported.

Steps:
1. Use the GitHub UI - https://github.com/kedify/otel-add-on/releases
1. Click on "Draft a new release".
1. Pick a new tag, follow the semantic versioning conventions, e.g. v1.2.3
1. Click on "Generate release notes" to auto-generate the title and release notes based on merged PRs. Add more description/diagram if needed.
1. Click on "Publish release".
1. This will trigger a GitHub action workflow for building and publishing container image and opens a PR for bumping the helm chart version.
1. Review and merge the PR for helm chart version bump. The title should be "Update chart.yaml" and it should look like this [one](https://github.com/kedify/otel-add-on/pull/160). This will trigger the release process for helm chart and at the end pushes the helm chart to OCI repo.
1. Subsequent PRs will be eventually opened by Renovate. These just update the version in docs and are not required for the release process itself.
