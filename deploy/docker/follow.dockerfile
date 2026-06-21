# 1. Build Stage
#åå»ºæé éåï¼å¹¶å½åä¸ºbuilder
FROM golang:1.25-alpine AS builder
#è®¾ç½®å·¥ä½ç®å½
WORKDIR /app
#è®¾ç½®ä»£çå¯¹è±¡ç½ç«
#å¤å¶æºç å?appç®å½ä¸?
COPY . .
#æå»ºfollowå¯æ§è¡æä»?
RUN go build -mod=vendor -o follow-service ./cmd/follow-service/main.go

# 2. Run Stage
FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/follow-service .
COPY --from=builder /app/configs ./configs
#æ´é²å?093æå¡ç«¯å£ï¼æå³çå¤§å®¶å¯ä»¥éè¿è¿ä¸ªç«¯å£è®¿é®è¿ä¸ªæå¡
EXPOSE 9093

CMD ["./follow-service"]
