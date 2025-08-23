# build stage
FROM golang:1-alpine AS build
RUN apk update && apk add git
ADD . /src
WORKDIR /src
ENV CGO_ENABLED 0
RUN go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --abbrev=0 --tags)" \
  -o /tmp/sshd-lite \
  ./cmd/sshd-lite
RUN go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --abbrev=0 --tags)" \
  -o /tmp/smux \
  ./cmd/smux
# run stage
FROM alpine:latest
RUN apk update && apk add bash git curl htop tree ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=build /tmp/sshd-lite /app/sshd-lite
COPY --from=build /tmp/smux /app/smux
# Default to sshd-lite, but both binaries are available
ENTRYPOINT ["/app/sshd-lite"]