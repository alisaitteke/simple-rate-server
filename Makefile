BINARY  := rate-server
VERSION ?= dev
LDFLAGS := -s -w -X github.com/alisaitteke/simple-rate-server/internal/version.Version=$(VERSION)

.PHONY: build test race vet run docker clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/rate-server

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

run:
	go run ./cmd/rate-server

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(BINARY):$(VERSION) .

clean:
	rm -rf bin
