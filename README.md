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
./build.sh
./hallertau config.toml
```

## Deployment example
```sh
./build.sh
docker compose up -d --build
```
