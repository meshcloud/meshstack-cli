# Runs on the build platform and cross-compiles for TARGETOS/TARGETARCH, so a
# multi-platform build needs no emulation. buildx sets those two args itself.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

WORKDIR /src

# Copied on their own so the module download layer survives any source change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
# .goreleaser.yml and flake.nix set the same ldflag, and all three have to agree. The
# linker ignores an -X whose path does not resolve and warns about nothing, so a stale
# path here is silent.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -trimpath \
      -ldflags "-s -w -X github.com/meshcloud/meshstack-cli/cmd/internal.Version=${VERSION}" \
      -o /out/meshstack ./cmd/meshstack

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/meshstack /usr/local/bin/meshstack

ENTRYPOINT ["/usr/local/bin/meshstack"]
