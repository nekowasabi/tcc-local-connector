.PHONY: all build-go build-swift build-firefox-extension bundle dev

all: build-go build-swift bundle

build-go:
	go build ./...

build-swift:
	swift build --package-path macos -c release

build-firefox-extension:
	web-ext build --source-dir firefox-extension --artifacts-dir dist/firefox-extension --overwrite-dest

bundle:
	bash scripts/make-app-bundle.sh

dev: build-firefox-extension
	bash scripts/dev-run.sh
