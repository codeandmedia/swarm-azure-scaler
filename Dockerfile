FROM golang:1.16-alpine AS builder

RUN mkdir /scaler
ADD . /scaler
WORKDIR /scaler
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o main ./...

FROM alpine:latest AS production
COPY --from=builder /scaler /home
VOLUME /var/run/docker.sock
CMD ["./home/main"]