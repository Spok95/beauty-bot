APP=beauty-bot

.PHONY: build run lint test
build:
	go build -o bin/$(APP).exe ./cmd/bot

run:
	go run ./cmd/bot

lint:
	@echo "lint later"

test:
	go test ./...

migrate-up:
	@echo "migrations later"

migrate-up:   goose -dir ./migrations -v postgres "$$DB_DSN" up
migrate-down: goose -dir ./migrations -v postgres "$$DB_DSN" down
migrate-status: goose -dir ./migrations -v postgres "$$DB_DSN" status