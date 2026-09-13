# syntax=docker/dockerfile:1

# The UI is built from source rather than taken from the checkout, so the image
# never depends on whether someone ran `make ui` first.
FROM node:22-alpine AS ui
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The ui build tag is what embeds the export; without it the binary would come
# up telling the operator the UI is missing.
COPY --from=ui /src/web/out ./web/out
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -tags ui -ldflags "-s -w -X main.version=${VERSION}" -o /out/faultline ./cmd/faultline
# The runtime has no shell to create directories with, so they are made here
# and copied in owned by the user that will write to them.
RUN mkdir -p /state/faultline /work

# The transparent-mode image. Redirecting traffic means writing iptables rules,
# which needs the iptables binary and a shell's worth of userspace that the
# default image deliberately does not have. It is a separate target rather than
# a change to the image everybody else gets: nobody should carry a package
# manager inside the proxy that sees their traffic in order to not use it.
#
#   docker build --target transparent -t faultline:transparent .
FROM alpine:3.22 AS transparent
RUN apk add --no-cache ca-certificates iptables libcap

# The same uid the default image runs as, and the uid the redirect rules
# exempt. Everything sharing this network namespace must avoid it, or its
# traffic is mistaken for Faultline's own and passes through unseen.
RUN addgroup -g 65532 -S nonroot && adduser -u 65532 -S -G nonroot nonroot

# File capabilities are what let a non-root Faultline install the rules. The
# container still has to be given NET_ADMIN - a capability not in the
# container's bounding set is not granted by a file that asks for it - so this
# widens nothing on its own, it only avoids running the proxy as root.
# Every iptables command is a symlink to this one binary, so it is the only
# one that has to carry them.
RUN setcap cap_net_admin,cap_net_raw+ep /usr/sbin/xtables-nft-multi

ENV XDG_CONFIG_HOME=/var/lib
COPY --from=build --chown=nonroot:nonroot /state/faultline /var/lib/faultline
VOLUME /var/lib/faultline
COPY --from=build --chown=nonroot:nonroot /work /work
WORKDIR /work

COPY --from=build /out/faultline /usr/local/bin/faultline

# admin and UI, forward proxy, the two transparent listeners, first route.
EXPOSE 9000 9001 9002 9003 9100

USER nonroot
ENTRYPOINT ["/usr/local/bin/faultline"]
CMD ["serve", "--bind", "0.0.0.0", "--transparent"]

# distroless static carries the root certificates Faultline needs to verify
# real upstreams, and nothing else: no shell, no package manager, non-root.
FROM gcr.io/distroless/static-debian12:nonroot

# XDG_CONFIG_HOME is where the CA lands, so pointing it at /var/lib puts the CA
# directory at /var/lib/faultline: the conventional place for container state,
# and the volume below keeps it across `docker run`s so clients that trusted the
# CA once keep trusting it.
ENV XDG_CONFIG_HOME=/var/lib
COPY --from=build --chown=nonroot:nonroot /state/faultline /var/lib/faultline
VOLUME /var/lib/faultline

# faultline.yaml is read from the working directory, so mounting a project at
# /work is all it takes to run with its rules and scenarios.
COPY --from=build --chown=nonroot:nonroot /work /work
WORKDIR /work

COPY --from=build /out/faultline /usr/local/bin/faultline

# admin and UI, forward proxy, first explicit route.
EXPOSE 9000 9001 9100

ENTRYPOINT ["/usr/local/bin/faultline"]
# Localhost would make every port published from this container a dead end, so
# the image asks for all interfaces explicitly rather than changing the default
# the CLI has everywhere else.
CMD ["serve", "--bind", "0.0.0.0"]
