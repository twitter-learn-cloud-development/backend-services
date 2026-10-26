# syntax=docker/dockerfile:1.7

# 1. Build Stage
ARG BUILDER_IMAGE=golang:1.25.5-alpine
ARG RUNTIME_IMAGE=alpine:latest
FROM ${BUILDER_IMAGE} AS builder
WORKDIR /app
COPY . .
RUN --mount=type=cache,id=twitter-clone-agent-go-build,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out \
    && CGO_ENABLED=0 go build -mod=vendor -trimpath -o /out/agent-service ./cmd/agent-service \
    && CGO_ENABLED=0 go build -mod=vendor -trimpath -o /out/agent-mcp-acceptance ./cmd/agent-mcp-acceptance \
    && CGO_ENABLED=0 go build -mod=vendor -trimpath -o /out/agent-mcp-conformance ./cmd/agent-mcp-conformance \
    && CGO_ENABLED=0 go build -mod=vendor -trimpath -o /out/agent-admission-admin ./cmd/agent-admission-admin
# 2. Run Stage
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /out/agent-service .
COPY --from=builder /out/agent-mcp-acceptance .
COPY --from=builder /out/agent-mcp-conformance .
COPY --from=builder /out/agent-admission-admin .
COPY --from=builder /app/configs ./configs
EXPOSE 9100
EXPOSE 9200
EXPOSE 9191
CMD ["./agent-service"]
