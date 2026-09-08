BINARY := gdayredis
BUILD_DIR := bin

.PHONY: all build run test lint clean

all: build

build:
	go build -trimpath -ldflags="-s -w" -o $(BUILD_DIR)/$(BINARY) ./cmd/gdayredis

run: build
	./$(BUILD_DIR)/$(BINARY)

test:
	go test ./...

lint:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)

docker-build:
	docker build -t $(BINARY):latest .

docker-up:
	docker compose up --build

docker-down:
	docker compose down
