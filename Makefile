.PHONY: all build-go build-swift bundle dev

all: build-go build-swift bundle

build-go:
	go build ./...

build-swift:
	swift build --package-path macos -c release

bundle:
	bash scripts/make-app-bundle.sh

dev:
	bash scripts/dev-run.sh
