#! /bin/sh

set -e

hash=$(git rev-parse HEAD)
CGO_ENABLED=0 go build -ldflags="-X cookbook/internal/core.Version=${hash}"
