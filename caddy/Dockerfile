FROM golang:1.27.4-alpine3.24@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src/caddy
COPY build/go.mod build/go.sum ./
RUN go mod download
COPY build/main.go ./
RUN go build -buildvcs=false -mod=readonly -trimpath -ldflags="-s -w" -o /out/caddy .
WORKDIR /src/launcher
COPY launcher/go.mod launcher/go.sum ./
RUN go mod download
COPY launcher/*.go ./
RUN go test -mod=readonly ./... && go build -buildvcs=false -mod=readonly -trimpath -ldflags="-s -w" -o /out/launcher .

FROM scratch
ARG BUILD_VERSION=0.1.0
LABEL io.hass.type="app" io.hass.arch="aarch64" io.hass.version="${BUILD_VERSION}" \
      org.opencontainers.image.title="Caddy Cloudflare mTLS" \
      org.opencontainers.image.licenses="Apache-2.0"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/caddy /usr/bin/caddy
COPY --from=build /out/launcher /usr/bin/launcher
EXPOSE 8443/tcp
STOPSIGNAL SIGTERM
# Initial root only prepares /data and immediately drops to UID/GID 65532.
USER 0:0
ENTRYPOINT ["/usr/bin/launcher"]
