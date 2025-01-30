FROM scratch

WORKDIR cookbook

COPY cookbook cookbook
COPY static static
COPY templates templates

ARG UID=1000
ARG GID=1000
USER ${UID}:${GID}

ENTRYPOINT ["/cookbook/cookbook", "-c", "config.toml"]
