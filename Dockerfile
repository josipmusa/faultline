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
