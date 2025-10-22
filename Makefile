APP=beauty-bot

.PHONY: build run lint test migrate-up migrate-down migrate-status

build:
	go build -o bin/$(APP) ./cmd/bot

run:
	go run ./cmd/bot

lint:
	golangci-lint run

test:
	go test ./...

migrate-up:
	goose -dir ./migrations -v postgres "$$DB_DSN" up

migrate-down:
	goose -dir ./migrations -v postgres "$$DB_DSN" down

migrate-status:
	goose -dir ./migrations -v postgres "$$DB_DSN" status
