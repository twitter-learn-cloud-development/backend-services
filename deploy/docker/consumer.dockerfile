# 1. Build Stage
#åå»ºæé éåï¼å¹¶å½åä¸ºbuilder
ARG BUILDER_IMAGE=golang:1.25.5-alpine
ARG RUNTIME_IMAGE=alpine:latest
FROM ${BUILDER_IMAGE} AS builder
#è®¾ç½®å·¥ä½ç®å½
WORKDIR /app
#è®¾ç½®ä»£çå¯¹è±¡ç½ç«
#å¤å¶æºç å?appç®å½ä¸?
COPY . .
#æå»ºconsumerå¯æ§è¡æä»?
RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -o consumer ./cmd/consumer

# 2. Run Stage
#è·å¾è¿è¡ç¯å¢éå
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates
#è®¾ç½®å·¥ä½ç®å½
WORKDIR /app
#å¤å¶builderä¸?app/consumerå°å½åç®å½?
COPY --from=builder /app/consumer .
#å¤å¶builderä¸?app/configså°å½åç®å½ä¸çconfigsç®å½
COPY --from=builder /app/configs ./configs
#è¿è¡consumer
CMD ["./consumer"]
