#! /bin/sh

set -e

hash=$(git rev-parse HEAD)
go build --tags fts5 -ldflags="-X hallertau/internal/core.Version=${hash}"
