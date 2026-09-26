param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("build-go", "build", "test", "release", "clean")]
    [string]$Target
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
Set-Location $repoRoot
$solution = "windows/TCCLocalConnector.sln"
$app = "windows/TCCLocalConnector/TCCLocalConnector.csproj"
$release = Join-Path $repoRoot "release"

function Invoke-Checked {
    param([scriptblock]$Command)

    & $Command
    if ($LASTEXITCODE -ne 0) {
        exit $LASTEXITCODE
    }
}

switch ($Target) {
    "build-go" {
        New-Item -ItemType Directory -Force -Path $release | Out-Null
        $env:GOOS = "windows"
        $env:GOARCH = "amd64"
        Invoke-Checked { go build -o (Join-Path $release "tcc-local-connector-backend.exe") ./cmd/tcc-local-connector-backend }
        Invoke-Checked { go build -o (Join-Path $release "tcc-firefox-native-host.exe") ./cmd/tcc-firefox-native-host }
    }
    "build" {
        Invoke-Checked { dotnet build $solution -c Release }
    }
    "test" {
        Invoke-Checked { dotnet test $solution -c Release --no-build }
    }
    "release" {
        Invoke-Checked { dotnet publish $app -c Release -r win-x64 --self-contained true -p:PublishSingleFile=true -p:DebugType=None -p:CopyOutputSymbolsToPublishDirectory=false -o $release }
        $keep = "TCCLocalConnector.exe", "tcc-local-connector-backend.exe", "tcc-firefox-native-host.exe"
        Get-ChildItem -Path $release -File | Where-Object { $_.Name -notin $keep } | Remove-Item
        $missing = $keep | Where-Object { -not (Test-Path (Join-Path $release $_)) }
        if ($missing) {
            throw "Release artifacts are missing: $($missing -join ', ')"
        }
        Get-ChildItem -Path $release -File
    }
    "clean" {
        Invoke-Checked { dotnet clean $solution -c Release }
        Remove-Item -Recurse -Force $release -ErrorAction SilentlyContinue
    }
}
