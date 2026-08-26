FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /envi-api ./cmd/api

FROM alpine:3.22
COPY --from=build /envi-api /usr/local/bin/envi-api
ENTRYPOINT ["envi-api"]
