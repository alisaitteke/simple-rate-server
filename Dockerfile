# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/alisaitteke/simple-rate-server/internal/version.Version=${VERSION}" \
    -o /out/rate-server ./cmd/rate-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/rate-server /rate-server
EXPOSE 8080
ENV RATE_BIND=0.0.0.0 \
    RATE_PORT=8080
ENTRYPOINT ["/rate-server"]
