# 1. Build Stage
#åå»ºæé éåï¼å¹¶å½åä¸ºbuilder
FROM golang:1.25-alpine AS builder   
#è®¾ç½®å·¥ä½ç®å½
WORKDIR /app
#è®¾ç½®ä»£çå¯¹è±¡ç½ç«
#å¤å¶æºç å?appç®å½ä¸?
COPY . .
#æå»ºconsumerå¯æ§è¡æä»?
RUN go build -mod=vendor -o consumer ./cmd/consumer/main.go

# 2. Run Stage
#è·å¾è¿è¡ç¯å¢éå
FROM alpine:latest
#è®¾ç½®å·¥ä½ç®å½
WORKDIR /app
#å¤å¶builderä¸?app/consumerå°å½åç®å½?
COPY --from=builder /app/consumer .
#å¤å¶builderä¸?app/configså°å½åç®å½ä¸çconfigsç®å½
COPY --from=builder /app/configs ./configs
#è¿è¡consumer
CMD ["./consumer"]
