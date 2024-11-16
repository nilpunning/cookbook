FROM scratch

ARG VERSION

COPY hallertau /hallertau
COPY static /static
COPY templates /templates

ENTRYPOINT ["/hallertau", "hallertau.toml"]
