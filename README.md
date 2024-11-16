# Hallertau

A simple recipe server using Go and SQLite.

## Requirements
- [go](https://go.dev/doc/install)
- [air](https://github.com/air-verse/air)
- [caddy](https://caddyserver.com/docs/install) (optional, for auth to function)

## Development

```sh
air
caddy run --config Caddyfile
```

## Production

```sh
go build --tags fts5
./hallertau hallertau.toml
```

## Deployment example
[push.sh](./push.sh)
- takes a server as an argument
- builds an amd64 Linux executable 
- builds a [Docker](./Dockerfile) image
- pushes the image to the specified server
```sh
./push.sh server.example.com
```

On the server [docker-compose](./docker-compose.yaml) can be used.
