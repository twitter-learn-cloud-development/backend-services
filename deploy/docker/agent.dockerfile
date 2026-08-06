# syntax=docker/dockerfile:1.7

# 1. Build Stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN --mount=type=cache,id=twitter-clone-agent-go-build,target=/root/.cache/go-build,sharing=locked \
    mkdir -p /out \
    && go build -mod=vendor -o /out/agent-service ./cmd/agent-service \
    && go build -mod=vendor -o /out/agent-mcp-acceptance ./cmd/agent-mcp-acceptance \
    && go build -mod=vendor -o /out/agent-mcp-conformance ./cmd/agent-mcp-conformance
# 2. Run Stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /out/agent-service .
COPY --from=builder /out/agent-mcp-acceptance .
COPY --from=builder /out/agent-mcp-conformance .
COPY --from=builder /app/configs ./configs
EXPOSE 9100
EXPOSE 9200
EXPOSE 9191
CMD ["./agent-service"]
