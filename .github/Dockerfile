# build stage
FROM golang:1-alpine AS build
RUN apk update && apk add git
ADD . /src
WORKDIR /src
ENV CGO_ENABLED 0
RUN go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --abbrev=0 --tags)" \
  -o /tmp/bin
# run stage
FROM alpine:latest
RUN apk update && apk add bash git curl htop tree ca-certificates && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=build /tmp/bin /app/bin
ENTRYPOINT ["/app/bin"]