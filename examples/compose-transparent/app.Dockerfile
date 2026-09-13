# syntax=docker/dockerfile:1

# The Go example, packaged as an ordinary service image. It knows nothing about
# Faultline: no proxy variables, no helper on the entrypoint, no Faultline code
# compiled in. That is the whole point of transparent mode.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /out/go-client ./examples/go-client

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/go-client /usr/local/bin/go-client
ENTRYPOINT ["/usr/local/bin/go-client"]
