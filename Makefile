.PHONY: all build-go build-swift build-firefox-extension bundle dev build-go-windows win-build win-test win-release win-clean

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

# Windows build (.NET 8 tray app + Go backend cross-compiled from WSL/Linux).
# Why: EnableWindowsTargeting in windows/Directory.Build.props lets dotnet publish
# net8.0-windows on Linux, and Go cross-compiles with GOOS=windows.
DOTNET      ?= dotnet
WIN_SLN     := windows/TCCLocalConnector.sln
WIN_APP     := windows/TCCLocalConnector/TCCLocalConnector.csproj
WIN_RID     ?= win-x64
RELEASE_DIR := release

build-go-windows:
	mkdir -p $(RELEASE_DIR)
	GOOS=windows GOARCH=amd64 go build -o $(RELEASE_DIR)/tcc-local-connector-backend.exe ./cmd/tcc-local-connector-backend
	GOOS=windows GOARCH=amd64 go build -o $(RELEASE_DIR)/tcc-firefox-native-host.exe ./cmd/tcc-firefox-native-host

win-build:
	$(DOTNET) build $(WIN_SLN) -c Release

win-test: win-build
	$(DOTNET) test $(WIN_SLN) -c Release --no-build

win-release: build-go-windows
	$(DOTNET) publish $(WIN_APP) -c Release -r $(WIN_RID) --self-contained true \
		-p:PublishSingleFile=true \
		-p:DebugType=None \
		-p:CopyOutputSymbolsToPublishDirectory=false \
		-o $(RELEASE_DIR)
	find $(RELEASE_DIR) -type f ! -name 'TCCLocalConnector.exe' ! -name 'tcc-local-connector-backend.exe' ! -name 'tcc-firefox-native-host.exe' -delete
	ls -la $(RELEASE_DIR)

win-clean:
	$(DOTNET) clean $(WIN_SLN) -c Release
	rm -rf $(RELEASE_DIR)
