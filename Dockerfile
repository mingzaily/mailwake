FROM node:24.18.1-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
COPY internal/i18n/locales/ /src/internal/i18n/locales/
COPY internal/i18n/languages.json /src/internal/i18n/languages.json
COPY internal/httpapi/testdata/ /src/internal/httpapi/testdata/
RUN npm run build

FROM golang:1.25.14-alpine AS build
ARG VERSION=devel
ARG REVISION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/httpapi/webdist ./internal/httpapi/webdist
RUN CGO_ENABLED=0 go build -tags nomsgpack -trimpath \
    -ldflags="-s -w -X github.com/mingzaily/mailwake/internal/buildinfo.Version=${VERSION} -X github.com/mingzaily/mailwake/internal/buildinfo.Revision=${REVISION}" \
    -o /out/mailwake ./cmd/mailwake

FROM alpine:3.22
ARG VERSION=devel
LABEL org.opencontainers.image.title="Mailwake Core" \
      org.opencontainers.image.description="Folder-level new-mail notifications for IMAP mailboxes, self-deployed." \
      org.opencontainers.image.source="https://github.com/mingzaily/mailwake" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.version="${VERSION}"
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 mailwake \
    && adduser -S -D -H -u 10001 -G mailwake mailwake \
    && mkdir /data \
    && chown mailwake:mailwake /data
WORKDIR /app
COPY --from=build /out/mailwake /app/mailwake
COPY --from=build /src/LICENSE /src/COPYRIGHT /usr/share/licenses/mailwake/
COPY --from=web /src/internal/httpapi/webdist/third-party-notices.txt /usr/share/licenses/mailwake/
USER mailwake
EXPOSE 8080
ENTRYPOINT ["/app/mailwake"]
ENV MAILWAKE_LISTEN=0.0.0.0:8080 MAILWAKE_DATA_DIR=/data
