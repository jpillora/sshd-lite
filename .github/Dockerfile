# build stage
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
COPY . /src
WORKDIR /src
ENV CGO_ENABLED=0
RUN GOOS="$TARGETOS" GOARCH="$TARGETARCH" GOARM="${TARGETVARIANT#v}" go build \
  -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o /tmp/bin
# run stage
FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
RUN apk add --no-cache bash git curl htop tree ca-certificates
WORKDIR /app
COPY --from=build /tmp/bin /app/bin
ENTRYPOINT ["/app/bin"]
