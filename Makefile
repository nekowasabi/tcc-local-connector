.PHONY: all build-go build-swift build-firefox-extension bundle dev build-go-windows win-build win-test win-release win-clean

ifeq ($(PC),wsl)
all: win-release
else
all: build-go build-swift bundle
endif

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

ifeq ($(OS),Windows_NT)
WINDOWS_BUILD := powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-windows.ps1

build-go-windows:
	$(WINDOWS_BUILD) build-go

win-build:
	$(WINDOWS_BUILD) build

win-test: win-build
	$(WINDOWS_BUILD) test

win-release: build-go-windows
	$(WINDOWS_BUILD) release

win-clean:
	$(WINDOWS_BUILD) clean
else
# Why: Run from local disk, not \\wsl.localhost — AV heuristics flag UNC-launched exes.
RELEASE_DIR := /mnt/c/takeda/tools/tcc-local-connector

build-go-windows:
	mkdir -p $(RELEASE_DIR)
	# Why: 起動中の exe はロックされ上書きできないため先に止める
	-taskkill.exe /IM TCCLocalConnector.exe /F >/dev/null 2>&1
	-taskkill.exe /IM tcc-local-connector-backend.exe /F >/dev/null 2>&1
	-taskkill.exe /IM tcc-firefox-native-host.exe /F >/dev/null 2>&1
	GOOS=windows GOARCH=amd64 go build -o $(RELEASE_DIR)/tcc-local-connector-backend.exe ./cmd/tcc-local-connector-backend
	GOOS=windows GOARCH=amd64 go build -o $(RELEASE_DIR)/tcc-firefox-native-host.exe ./cmd/tcc-firefox-native-host

win-build:
	$(DOTNET) build $(WIN_SLN) -c Release

win-test: win-build
	$(DOTNET) test $(WIN_SLN) -c Release --no-build

# Why: Framework-dependent folder publish, not self-extracting single-file — self-extraction looks like a dropper to AV.
win-release: build-go-windows
	$(DOTNET) publish $(WIN_APP) -c Release -r $(WIN_RID) --self-contained false \
		-p:PublishSingleFile=false \
		-p:DebugType=None \
		-p:CopyOutputSymbolsToPublishDirectory=false \
		-o $(RELEASE_DIR)
	ls -la $(RELEASE_DIR)
	cd $(RELEASE_DIR) && powershell.exe -NoProfile -Command "Start-Process -FilePath .\TCCLocalConnector.exe"

# Why: RELEASE_DIR は Windows 側の配置先なので消さない
win-clean:
	$(DOTNET) clean $(WIN_SLN) -c Release
endif
