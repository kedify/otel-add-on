FROM --platform=${BUILDPLATFORM} golang:1.27.3 AS builder
WORKDIR /workspace
COPY go.* .
RUN go mod download
COPY . .
ARG VERSION=main
ARG GIT_COMMIT=HEAD
ARG TARGETOS
ARG TARGETARCH
RUN VERSION="${VERSION}" GIT_COMMIT="${GIT_COMMIT}" TARGET_OS="${TARGETOS}" ARCH="${TARGETARCH}" make build

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /workspace/bin/otel-add-on /sbin/init
ENTRYPOINT ["/sbin/init"]
