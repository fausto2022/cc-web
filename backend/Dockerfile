FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/cc-web ./cmd/cc-web

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app && mkdir -p /data && chown app:app /data
COPY --from=build /out/cc-web /usr/local/bin/cc-web
ENV CC_WEB_LISTEN=0.0.0.0:8080
ENV CC_WEB_DB_PATH=/data/cc-web.db
USER app
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/cc-web"]
