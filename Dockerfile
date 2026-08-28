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
# .goreleaser.yml sets the same -X main.Version ldflag, and the two have to agree.
# Nothing fails when one is missing: that binary reports `dev`.
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
      -trimpath \
      -ldflags "-s -w -X main.Version=${VERSION}" \
      -o /out/meshstack ./cmd/meshstack

# distroless static: no shell and no package manager, which is all a single static
# binary needs. 'nonroot' runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot

# The image is named after the repository, meshstack-cli, while the binary it carries
# is meshstack. So `docker run ghcr.io/meshcloud/meshstack-cli buildingblock list`
# reads the same as the local `meshstack buildingblock list`.
COPY --from=build /out/meshstack /usr/local/bin/meshstack

ENTRYPOINT ["/usr/local/bin/meshstack"]
