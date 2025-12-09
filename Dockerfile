FROM golang:1.24.4-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/bot

FROM gcr.io/distroless/base-debian12

WORKDIR /app

# бинарник
COPY --from=build /app ./bot

# конфигурация и миграции из контекста сборки
COPY config ./config
COPY migrations ./migrations

ENTRYPOINT ["/app/bot"]
