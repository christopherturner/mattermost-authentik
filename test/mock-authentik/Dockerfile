FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /authentik-mock main.go

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /authentik-mock /app/authentik-mock
EXPOSE 9000
CMD ["/app/authentik-mock"]
