# meshyd — Meshy resource daemon for Linux servers.
#
# Build (from repo root):
#   docker build -f deploy/meshyd.Dockerfile -t meshyd .
#   docker build -f deploy/meshyd.Dockerfile --build-arg TAGS=netbird -t meshyd .   # with mesh listener
#
# Run:
#   docker run -d --name meshyd \
#     --device /dev/fuse --cap-add SYS_ADMIN \
#     --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
#     --security-opt systempaths=unconfined \
#     -v meshy-state:/var/lib/meshy -v /srv:/srv \
#     -e MESHY_NAME=db1.prod.acme.eth -e MESHY_ROOTS=/srv \
#     -e MESHY_SEPOLIA=wss://<sepolia-ws> \
#     -e MESHY_SETUP_KEY=<from provider> -e MESHY_MANAGEMENT_URL=https://<provider> \
#     -p 2222:2222 meshyd
#   docker logs meshyd        # first boot prints the join code for the dashboard
#   docker exec -it meshyd meshyd status -w
#
# Why these flags: sessions run in bubblewrap namespaces (needs seccomp/
# apparmor unconfined), folder access goes through FUSE (needs /dev/fuse +
# SYS_ADMIN), and a private /proc per session needs systempaths=unconfined
# (without it sessions get an empty /proc).

FROM golang:1.27-bookworm AS build
ARG TAGS=""
WORKDIR /src
COPY meshy/go.mod meshy/go.sum ./
RUN go mod download
COPY meshy/ ./
RUN CGO_ENABLED=0 go build -trimpath -tags "${TAGS}" -ldflags "-s -w" -o /out/meshyd ./cmd/meshyd

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      bubblewrap fuse3 ca-certificates procps coreutils curl python3 git \
    && rm -rf /var/lib/apt/lists/* \
    && sed -i 's/^#user_allow_other/user_allow_other/' /etc/fuse.conf
COPY --from=build /out/meshyd /usr/local/bin/meshyd
COPY deploy/meshyd-entrypoint.sh /usr/local/bin/meshyd-entrypoint
ENV MESHY_STATE=/var/lib/meshy MESHY_LISTEN=:2222 MESHY_CONTROL=/var/run/meshyd.sock
VOLUME /var/lib/meshy
EXPOSE 2222
HEALTHCHECK --interval=30s --timeout=5s CMD meshyd status >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/meshyd-entrypoint"]
