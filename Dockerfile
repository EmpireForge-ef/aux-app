# --- frontend build ---------------------------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# --- backend build -----------------------------------------------------------
FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/aux ./cmd/server

# --- runtime -----------------------------------------------------------------
FROM alpine:3.21

# Fixed non-root UID/GID so Kubernetes runAsUser/runAsNonRoot can pin them and
# a mounted volume's fsGroup lines up. The app never needs root.
ARG UID=10001
ARG GID=10001
RUN apk add --no-cache ca-certificates \
 && addgroup -S -g ${GID} aux \
 && adduser -S -u ${UID} -G aux aux \
 && mkdir -p /data && chown ${UID}:${GID} /data

COPY --from=backend /out/aux /usr/local/bin/aux
COPY --from=frontend /app/dist /srv/frontend

ENV AUX_STATIC_DIR=/srv/frontend \
    AUX_TOKEN_FILE=/data/spotify-token.json \
    AUX_SETTINGS_FILE=/data/aux-settings.json \
    AUX_CHATS_DIR=/data/chats \
    AUX_ADDR=:8080

# Numeric UID so orchestrators can enforce runAsNonRoot without resolving names.
USER ${UID}:${GID}
WORKDIR /data
VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/aux"]
CMD ["serve"]
