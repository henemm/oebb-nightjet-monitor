FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o oebb-nightjet-monitor .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/oebb-nightjet-monitor .
ENTRYPOINT ["./oebb-nightjet-monitor", "-config", "/app/config.yaml"]
CMD []
