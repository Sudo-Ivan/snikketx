FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/updater ./cmd/updater
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/updater ./cmd/updater

FROM docker:27-cli
RUN apk add --no-cache ca-certificates
COPY --from=build /out/updater /usr/local/bin/snikket-updater
USER root
EXPOSE 9191
ENTRYPOINT ["/usr/local/bin/snikket-updater"]
