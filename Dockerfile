FROM golang:1.27.2-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor ./vendor
COPY . .
RUN CGO_ENABLED=1 go build -mod=vendor -trimpath -o /out/inventory ./cmd/inventory

FROM debian:bookworm-slim
RUN mkdir -p /data && chown 1000:1000 /data
COPY --from=build /out/inventory /app/inventory
USER 1000:1000
WORKDIR /app
ENTRYPOINT ["/app/inventory"]
CMD ["serve", "--db", "/data/inventory.db", "--listen", "127.0.0.1:8080"]
