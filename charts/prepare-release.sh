#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# This script prepares the Helm chart for a new release
# It updates the version and appVersion in Chart.yaml
version="$1"

if [[ -z "$version" ]]; then
  echo "Usage: $0 <new-version>"
  exit 1
fi

# If the version is a semver, use it as version and appVersion. Otherwise it needs to be in the form version-appVersion
if [[ "$version" =~ ^v?([0-9]+\.[0-9]+\.[0-9]+)(-.+)?$ ]]; then
  chart_version="${version#v}"
  app_version="${version#v}"
else
  # Get the latest released version from git tags
  latest_tag=$(git tag --list 'v*' --sort=-v:refname | head -n 1)
  if [[ -z "$latest_tag" ]]; then
	echo "No git tags found. Falling back to version 0.0.0"
	latest_tag="v0.0.0"
  fi
  chart_version="${latest_tag#v}-$version"
  app_version="$version"
fi

echo "Packing chart with version: $chart_version and appVersion: $app_version"
helm package ./ephemeral-envs --version "$chart_version" --app-version "$app_version"
