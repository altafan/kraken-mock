FROM golang:1.22.1 as builder

WORKDIR /app
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /krakenmock main.go

FROM alpine:3.12.0
COPY ./config.yaml /config.yaml
COPY --from=builder /krakenmock /krakenmock

ENTRYPOINT ["/krakenmock"]

