FROM debian:bookworm-slim

COPY hallertau /hallertau
COPY static /static
COPY templates /templates

CMD ["/hallertau", "hallertau.toml"]
