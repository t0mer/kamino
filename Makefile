VERSION ?= dev
LDFLAGS := -s -w -X github.com/t0mer/kamino/internal/version.Version=$(VERSION)

.PHONY: build test vet lint
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/kamino ./cmd/kamino

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run
