# 1. Build Stage
ARG BUILDER_IMAGE=golang:1.25.5-alpine
ARG RUNTIME_IMAGE=alpine:latest
FROM ${BUILDER_IMAGE} AS builder

WORKDIR /app



COPY . .

RUN CGO_ENABLED=0 go build -mod=vendor -trimpath -o tweet-service ./cmd/tweet-service

# 2. Run Stage
FROM ${RUNTIME_IMAGE}
RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/tweet-service .
COPY --from=builder /app/configs ./configs

EXPOSE 9092

CMD ["./tweet-service"]
