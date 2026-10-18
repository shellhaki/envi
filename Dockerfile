# The API server (cmd/api).
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /envi-api ./cmd/api

FROM alpine:3.22
# Outbound TLS (Resend, Let's Encrypt-fronted proxies) needs root certificates.
RUN apk add --no-cache ca-certificates
COPY --from=build /envi-api /usr/local/bin/envi-api
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["envi-api"]
