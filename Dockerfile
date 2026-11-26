# Build the checked-out sources, never a host-provided binary or frontend dist.
# For byte-for-byte rebuilds, override these with approved image@sha256 digests.
ARG NODE_IMAGE=node:24-bookworm
ARG GO_IMAGE=golang:1.26

FROM ${NODE_IMAGE} AS frontend-deps
WORKDIR /src/frontend
RUN corepack enable
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

FROM frontend-deps AS frontend-build
COPY frontend/ ./
RUN pnpm run build

FROM ${GO_IMAGE} AS go-deps
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify

FROM go-deps AS builder
COPY . .
COPY --from=frontend-build /src/frontend/dist ./frontend/dist
ARG VERSION=develop
ARG COMMIT_SHA=unknown
RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/filebrowser/filebrowser/v2/version.Version=${VERSION} -X github.com/filebrowser/filebrowser/v2/version.CommitSHA=${COMMIT_SHA}" \
    -o /out/filebrowser .

## Multistage build: First stage fetches dependencies
FROM alpine:3.23 AS fetcher

# install and copy ca-certificates, mailcap, and tini-static; download JSON.sh
RUN apk update && \
    apk --no-cache add ca-certificates mailcap tini-static && \
    wget -O /JSON.sh https://raw.githubusercontent.com/dominictarr/JSON.sh/0d5e5c77365f63809bf6e77ef44a1f34b0e05840/JSON.sh

## Second stage: Use lightweight BusyBox image for final runtime environment
FROM busybox:1.37.0-musl

# Define non-root user UID and GID
ENV UID=1000
ENV GID=1000

# Create user group and user
RUN addgroup -g $GID user && \
    adduser -D -u $UID -G user user

# Copy binary, scripts, and configurations into image with proper ownership
COPY --from=builder --chown=user:user /out/filebrowser /bin/filebrowser
COPY --chown=user:user docker/common/ /
COPY --chown=user:user docker/alpine/ /
COPY --chown=user:user --from=fetcher /sbin/tini-static /bin/tini
COPY --from=fetcher /JSON.sh /JSON.sh
COPY --from=fetcher /etc/ca-certificates.conf /etc/ca-certificates.conf
COPY --from=fetcher /etc/ca-certificates /etc/ca-certificates
COPY --from=fetcher /etc/mime.types /etc/mime.types
COPY --from=fetcher /etc/ssl /etc/ssl

# Create data directories, set ownership, and ensure healthcheck script is executable
RUN mkdir -p /config /database /srv && \
    chown -R user:user /config /database /srv \
    && chmod +x /healthcheck.sh

# Define healthcheck script
HEALTHCHECK --start-period=10s --interval=5s --timeout=3s CMD /healthcheck.sh

# Set the user, volumes and exposed ports
USER user

VOLUME /srv /config /database

EXPOSE 80

ENTRYPOINT [ "tini", "--", "/init.sh" ]
