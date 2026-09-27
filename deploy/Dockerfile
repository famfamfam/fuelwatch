# Сборка из корня репозитория: docker build -f deploy/Dockerfile .
FROM node:24-alpine AS panel
WORKDIR /src/panel
COPY panel/package.json panel/package-lock.json ./
RUN npm ci
COPY panel/ ./
# vite кладёт сборку в ../server/internal/panelapi/dist
RUN mkdir -p ../server/internal/panelapi && npm run build

FROM golang:1.26-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
COPY --from=panel /src/server/internal/panelapi/dist ./internal/panelapi/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/fuelwatch ./cmd/fuelwatch

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && mkdir -p /data/frames && chown -R 65534:65534 /data
COPY --from=server /out/fuelwatch /usr/local/bin/fuelwatch
ENV FRAMES_DIR=/data/frames LISTEN_ADDR=:8080
VOLUME /data
EXPOSE 8080
USER 65534:65534
HEALTHCHECK --interval=30s --timeout=5s CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["fuelwatch"]
