FROM scratch

WORKDIR hallertau

COPY hallertau hallertau
COPY static static
COPY templates templates

ARG UID=1000
ARG GID=1000
USER ${UID}:${GID}

ENTRYPOINT ["/hallertau/hallertau", "hallertau.toml"]
