# 1. Build Stage
ARG BUILDER_IMAGE=golang:1.25.5-alpine
ARG RUNTIME_IMAGE=alpine:latest
FROM ${BUILDER_IMAGE} AS builder

WORKDIR /app

# è®¾ç½®ä»£çï¼å éä¸è½?



COPY . .

# ç¼è¯
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -o gateway ./cmd/gateway

# 2. Run Stage
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates

WORKDIR /app

# ä»?builder é¶æ®µå¤å¶äºè¿å¶æä»?
COPY --from=builder /app/gateway .
COPY --from=builder /app/configs ./configs
# å¦ææ?.envï¼docker-compose ä¼æ³¨å¥ç¯å¢åéï¼ä¸éè¦å¤å?

EXPOSE 9638

CMD ["./gateway"]
