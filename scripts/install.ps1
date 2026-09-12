param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$Repo = "shellhaki/envi"
$Binary = "envi"

function Fail($msg) {
    Write-Error "envi: $msg"
    exit 1
}

$archRaw = $env:PROCESSOR_ARCHITECTURE
switch ($archRaw) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    default { Fail "unsupported architecture: $archRaw" }
}

if ([string]::IsNullOrEmpty($Version)) {
    Write-Host "envi: looking up the latest release..."
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $tag = $release.tag_name
} else {
    if ($Version.StartsWith("v")) { $tag = $Version } else { $tag = "v$Version" }
}
$ver = $tag.TrimStart("v")

$archive = "${Binary}_${ver}_windows_${arch}.zip"
$baseUrl = "https://github.com/$Repo/releases/download/$tag"

if ($env:ENVI_INSTALL_DIR) {
    $installDir = $env:ENVI_INSTALL_DIR
} else {
    $installDir = Join-Path $env:LOCALAPPDATA "envi\bin"
}
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

$work = Join-Path $env:TEMP "envi-install-$(Get-Random)"
New-Item -ItemType Directory -Path $work | Out-Null
try {
    $archivePath = Join-Path $work $archive
    $checksumsPath = Join-Path $work "checksums.txt"

    Write-Host "envi: downloading $archive ($tag)..."
    try {
        Invoke-WebRequest -Uri "$baseUrl/$archive" -OutFile $archivePath
    } catch {
        Fail "download failed - check that $tag exists and has a windows/$arch build"
    }
    try {
        Invoke-WebRequest -Uri "$baseUrl/checksums.txt" -OutFile $checksumsPath
    } catch {
        Fail "couldn't download checksums.txt for $tag"
    }

    Write-Host "envi: verifying checksum..."
    $line = Select-String -Path $checksumsPath -Pattern ([regex]::Escape($archive)) | Select-Object -First 1
    if (-not $line) { Fail "no checksum entry found for $archive" }
    $expected = ($line.Line -split '\s+')[0]
    $actual = (Get-FileHash -Path $archivePath -Algorithm SHA256).Hash.ToLower()
    if ($expected -ne $actual) { Fail "checksum mismatch for $archive - expected $expected, got $actual" }

    Write-Host "envi: installing to $installDir..."
    Expand-Archive -Path $archivePath -DestinationPath $work -Force
    Move-Item -Path (Join-Path $work "$Binary.exe") -Destination (Join-Path $installDir "$Binary.exe") -Force
} finally {
    Remove-Item -Recurse -Force $work -ErrorAction SilentlyContinue
}

Write-Host "envi: installed $tag to $installDir\$Binary.exe"

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "envi: added $installDir to your user PATH - restart your terminal for it to take effect"
}

& (Join-Path $installDir "$Binary.exe") version
