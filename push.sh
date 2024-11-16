#! /bin/sh

set -e

server=$1

hash=$(git rev-parse HEAD)
env CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build --tags fts5 -ldflags="-X hallertau/internal/core.Version=${hash}"

docker build -t hallertau:${hash} .
docker save hallertau:${hash} | ssh -C ${server} docker load
