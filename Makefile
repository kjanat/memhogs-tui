APP     := memhogs
MODULE  := github.com/kjanat/memhogs-tui
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
           -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.date=$(DATE)

.PHONY: build run clean vet

build:
	go build -ldflags '$(LDFLAGS)' -o $(APP) .

run:
	go run -ldflags '$(LDFLAGS)' .

vet:
	go vet ./...

clean:
	rm -f $(APP)
