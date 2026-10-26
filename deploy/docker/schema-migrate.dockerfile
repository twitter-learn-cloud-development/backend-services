# Supply reviewed builder/runtime images; production requires immutable digests.
ARG BUILDER_IMAGE
ARG RUNTIME_IMAGE
FROM ${BUILDER_IMAGE} AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY internal ./internal
COPY api ./api
COPY pkg ./pkg
COPY cmd/schema-migrate ./cmd/schema-migrate
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -o /schema-migrate ./cmd/schema-migrate

FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /schema-migrate /app/schema-migrate
USER 65532:65532
ENTRYPOINT ["/app/schema-migrate"]
