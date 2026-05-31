APP     := memhogs
MODULE  := github.com/kjanat/memhogs-tui
VERSION := $(shell git describe --tags --always --dirty)
COMMIT  := $(shell git rev-parse --short HEAD)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
           -X main.version=$(VERSION) \
           -X main.commit=$(COMMIT) \
           -X main.date=$(DATE)

.PHONY: build run clean vet fmt

build:
	go build -ldflags '$(LDFLAGS)' -o $(APP) .

run:
	go run -ldflags '$(LDFLAGS)' .

vet:
	go vet ./...

fmt:
	@if command -v dprint >/dev/null 2>&1; then \
		dprint fmt; \
	elif command -v npx >/dev/null 2>&1; then \
		npx dprint fmt; \
	else \
		echo "dprint not found on PATH and npx unavailable" >&2; \
		exit 1; \
	fi

clean:
	rm -f $(APP)
