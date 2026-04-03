# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/weather-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /

COPY --from=builder --chown=65532:65532 /out/weather-api /weather-api

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/weather-api"]
