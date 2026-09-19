# Build the server, then ship only the binary.
#
# The result is a scratch image holding a static binary, CA certificates and
# nothing else: no shell, no package manager, no Go toolchain. There is nothing
# in it to exploit because there is nothing in it.

FROM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies are copied and downloaded first so this layer is cached until
# go.mod or go.sum actually changes, rather than on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 is what makes the binary runnable on scratch: with cgo, it would
# link against the build image's libc, which is not there at runtime.
# -trimpath keeps build machine paths out of the binary, and the ldflags drop the
# symbol table and DWARF data.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/server ./cmd/server

FROM scratch

# TLS roots, needed to reach MongoDB Atlas and a TLS Redis. Without these the
# binary cannot verify any certificate and every connection fails with an error
# that does not obviously mean "no CA bundle".
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=build /out/server /server

# An unprivileged, non-existent UID. scratch has no /etc/passwd to name, and a
# numeric user is what Kubernetes' runAsNonRoot check wants anyway.
USER 65532:65532

EXPOSE 8080

# No HEALTHCHECK: there is no shell or curl in this image to run one with. The
# platform should check GET /health over HTTP, which reports MongoDB and Redis
# separately — see docs/deployment.md.

ENTRYPOINT ["/server"]
