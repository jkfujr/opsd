ARG SOURCE_DATE_EPOCH=1795402865
FROM --platform=$BUILDPLATFORM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build

COPY --from=upstream go-module.zip /tmp/seaweedfs.zip
RUN printf '%s\n' '173201eacd10178fa8e7d33de068bf333b2e4f72331bea638203b6e0f8586ff6  /tmp/seaweedfs.zip' | sha256sum -c - \
    && python3 -m zipfile -e /tmp/seaweedfs.zip /src
WORKDIR /src/github.com/seaweedfs/seaweedfs@v0.0.0-20261123030105-a98c9e27185c
COPY managed-ec.patch /tmp/managed-ec.patch
RUN git apply /tmp/managed-ec.patch
COPY opsd_ec_command.go weed/command/opsd_ec.go
COPY opsd_ec_worker.go weed/worker/tasks/erasure_coding/opsd_ec.go
COPY opsd_ec_transitions.go weed/worker/tasks/erasure_coding/opsd_ec_transitions.go
COPY opsd_ec_repair.go weed/worker/tasks/erasure_coding/opsd_ec_repair.go
COPY opsd_ec_server.go weed/server/volume_opsd_ec.go
ARG TARGETOS
ARG TARGETARCH
ENV GOTOOLCHAIN=local
ENV GOPROXY=https://goproxy.cn|https://goproxy.io|https://proxy.golang.org|direct
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -buildvcs=false \
    -ldflags='-s -w -buildid= -X github.com/seaweedfs/seaweedfs/weed/util/version.COMMIT=a98c9e271' \
    -o /out/weed ./weed

FROM chrislusf/seaweedfs:4.55@sha256:7ca46aeae2f223b37e5d91593c17ab37738eec6df6ab69b48a0487f7643342b9
LABEL org.opencontainers.image.version="4.55-opsd.1" \
      org.opencontainers.image.revision="a98c9e27185cf1003eca095761a39973c60734db"
COPY --from=build /out/weed /usr/bin/weed
