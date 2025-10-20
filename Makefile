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
