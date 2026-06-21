# 1. Build Stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# è®¾ç½®ä»£çï¼å éä¸è½?



COPY . .

# ç¼è¯
RUN go build -mod=vendor -o gateway ./cmd/gateway/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

# ä»?builder é¶æ®µå¤å¶äºè¿å¶æä»?
COPY --from=builder /app/gateway .
COPY --from=builder /app/configs ./configs
# å¦ææ?.envï¼docker-compose ä¼æ³¨å¥ç¯å¢åéï¼ä¸éè¦å¤å?

EXPOSE 9638

CMD ["./gateway"]
