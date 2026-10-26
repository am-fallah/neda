# syntax=docker/dockerfile:1

# Base images use multi-arch manifest-list digests (linux/amd64 and linux/arm64
# verified) resolved from Docker Hub/gcr.io on 2026-10-26. Update deliberately
# after reviewing the upstream release and rebuilding the full test suite.

# Frontend build stage: the SPA is embedded into the Go binary.
FROM node:24-alpine@sha256:06d7adf3e0ef61a13f1d28682726366f97a15aa1888c934d3ecec86c33918b53 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build

# Backend build stage: pure-Go SQLite keeps the runtime image small and CGO-free.
FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN mkdir -p /out/data && touch /out/data/.keep
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o /out/neda ./cmd/neda

# Runtime: distroless static, non-root, no shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
LABEL org.opencontainers.image.title="Neda" \
      org.opencontainers.image.description="Private train availability monitor" \
      org.opencontainers.image.licenses="UNLICENSED"
COPY --from=build /out/neda /neda
COPY --from=build --chown=nonroot:nonroot /out/data/ /data/
# Provider registration is deliberately not defaulted here: production must opt
# in explicitly so a bare image cannot start real provider monitoring.
ENV NEDA_ENV=production \
    NEDA_HTTP_ADDR=:8080 \
    NEDA_DB_PATH=/data/neda.db \
    NEDA_ENCRYPTION_KEY_FILE=/run/secrets/encryption_key \
    NEDA_LOG_FORMAT=json
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD ["/neda", "healthcheck"]
ENTRYPOINT ["/neda"]
CMD ["serve"]
