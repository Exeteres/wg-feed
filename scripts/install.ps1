param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$InstallerArgs
)

$ErrorActionPreference = "Stop"

$RepoOwner = "exeteres"
$RepoName = "wg-feed"

function Test-IsAdmin {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($id)
    return $principal.IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
}

function Get-Arch {
    $arch = ($env:PROCESSOR_ARCHITEW6432, $env:PROCESSOR_ARCHITECTURE | Where-Object { $_ } | Select-Object -First 1)
    if ($arch -match "ARM64") {
        return "arm64"
    }
    if ($arch -match "AMD64|X86_64") {
        return "amd64"
    }
    throw "unsupported architecture: $arch"
}

function Get-LatestTag {
    $url = "https://api.github.com/repos/$RepoOwner/$RepoName/releases/latest"
    $release = Invoke-RestMethod -Uri $url
    if (-not $release.tag_name) {
        throw "failed to detect latest release tag"
    }
    return [string]$release.tag_name
}

function Get-ChecksumForAsset {
    param(
        [string]$ChecksumsText,
        [string]$AssetName
    )

    foreach ($line in ($ChecksumsText -split "`n")) {
        $parts = $line.Trim() -split "\s+"
        if ($parts.Length -ge 2 -and $parts[1] -eq $AssetName) {
            return $parts[0].ToLowerInvariant()
        }
    }

    throw "checksum for asset not found: $AssetName"
}

function Get-FileSha256 {
    param([string]$Path)
    return (Get-FileHash -Algorithm SHA256 -Path $Path).Hash.ToLowerInvariant()
}

function Test-IsWindowsHost {
    if ($null -ne (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue)) {
        return [bool]$IsWindows
    }
    return [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
}

function To-SingleQuotedLiteral {
    param([string]$Value)
    return "'" + ($Value -replace "'", "''") + "'"
}

function Invoke-Installer {
    param(
        [string]$InstallerPath,
        [string]$VersionTag,
        [string]$DaemonChecksum,
        [string[]]$Args
    )

    if (Test-IsAdmin) {
        $env:WG_FEED_VERSION = $VersionTag
        $env:WG_FEED_DAEMON_CHECKSUM = $DaemonChecksum
        & $InstallerPath @Args
        if ($LASTEXITCODE -ne $null) {
            exit $LASTEXITCODE
        }
        return
    }

    $installerLit = To-SingleQuotedLiteral $InstallerPath
    $tagLit = To-SingleQuotedLiteral $VersionTag
    $checksumLit = To-SingleQuotedLiteral $DaemonChecksum
    $argLits = @()
    foreach ($arg in $Args) {
        $argLits += To-SingleQuotedLiteral $arg
    }
    $argsExpr = if ($argLits.Count -gt 0) { "@(" + ($argLits -join ",") + ")" } else { "@()" }

    $command = "$env:WG_FEED_VERSION=$tagLit; $env:WG_FEED_DAEMON_CHECKSUM=$checksumLit; & $installerLit $argsExpr; exit `$LASTEXITCODE"
    $psExe = (Get-Process -Id $PID).Path
    try {
        $proc = Start-Process -FilePath $psExe -Verb RunAs -ArgumentList @("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", $command) -Wait -PassThru
        exit $proc.ExitCode
    }
    catch {
        throw "administrator rights are required to install service/files; elevation was declined or failed"
    }
}

if ($PSVersionTable.PSEdition -ne "Desktop" -and $PSVersionTable.PSEdition -ne "Core") {
    throw "PowerShell is required"
}

if (-not (Test-IsWindowsHost)) {
    throw "wg-feed installer script is only supported on Windows"
}

$arch = Get-Arch
$tag = Get-LatestTag
$tagVersion = $tag.TrimStart("v")
$installerAsset = "wg-feed-installer_${tagVersion}_windows_${arch}.exe"
$daemonAsset = "wg-feed-daemon_${tagVersion}_windows_${arch}.exe"
$baseUrl = "https://github.com/$RepoOwner/$RepoName/releases/download/$tag"
$installerUrl = "$baseUrl/$installerAsset"
$checksumsUrl = "$baseUrl/checksums.txt"

$checksumsText = (Invoke-WebRequest -UseBasicParsing -Uri $checksumsUrl).Content
$installerChecksum = Get-ChecksumForAsset -ChecksumsText $checksumsText -AssetName $installerAsset
$daemonChecksum = Get-ChecksumForAsset -ChecksumsText $checksumsText -AssetName $daemonAsset

$tmpDir = Join-Path $env:TEMP "wg-feed-installer-cache"
$installerBin = Join-Path $tmpDir "$installerAsset-$tag"
New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

$needDownload = $true
if (Test-Path -LiteralPath $installerBin) {
    $actualChecksum = Get-FileSha256 -Path $installerBin
    if ($actualChecksum -eq $installerChecksum) {
        $needDownload = $false
        Write-Host "Using cached installer $installerAsset ($tag)..."
    }
    else {
        Write-Host "Cached installer checksum mismatch, re-downloading..."
        Remove-Item -LiteralPath $installerBin -Force
    }
}

if ($needDownload) {
    Write-Host "Downloading installer $installerAsset ($tag)..."
    Invoke-WebRequest -UseBasicParsing -Uri $installerUrl -OutFile $installerBin
    $actualChecksum = Get-FileSha256 -Path $installerBin
    if ($actualChecksum -ne $installerChecksum) {
        throw "checksum mismatch for $installerBin (expected $installerChecksum, got $actualChecksum)"
    }
}

Invoke-Installer -InstallerPath $installerBin -VersionTag $tag -DaemonChecksum $daemonChecksum -Args $InstallerArgs
