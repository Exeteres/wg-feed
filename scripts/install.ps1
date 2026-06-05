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
    if (-not $arch) {
        throw "unsupported architecture: unknown"
    }
    $arch = $arch.Trim().ToUpperInvariant()
    if ($arch -match "ARM64") {
        return "arm64"
    }
    if ($arch -match "AMD64|X86_64|X64") {
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

function Convert-ContentToText {
    param($Content)
    if ($Content -is [byte[]]) {
        return [System.Text.Encoding]::UTF8.GetString($Content)
    }
    return [string]$Content
}

function Test-IsWindowsHost {
    if ($null -ne (Get-Variable -Name IsWindows -ErrorAction SilentlyContinue)) {
        return [bool]$IsWindows
    }
    return [System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT
}

function Invoke-Installer {
    param(
        [string]$InstallerPath,
        [string]$VersionTag,
        [string]$DaemonChecksum,
        [string[]]$Args
    )

    $env:WG_FEED_VERSION = $VersionTag
    $env:WG_FEED_DAEMON_CHECKSUM = $DaemonChecksum
    $allArgs = @()
    if ($null -ne $Args) {
        $allArgs += $Args
    }

    if (Test-IsAdmin) {
        if ($allArgs.Count -gt 0) {
            & $InstallerPath @allArgs
        }
        else {
            & $InstallerPath
        }
        if ($LASTEXITCODE -ne $null) {
            exit $LASTEXITCODE
        }
        return
    }
    try {
        if ($allArgs.Count -gt 0) {
            $proc = Start-Process -FilePath $InstallerPath -Verb RunAs -ArgumentList $allArgs -Wait -PassThru
        }
        else {
            $proc = Start-Process -FilePath $InstallerPath -Verb RunAs -Wait -PassThru
        }
        exit $proc.ExitCode
    }
    catch {
        $msg = $_.Exception.Message
        if ($_.Exception.InnerException -and $_.Exception.InnerException.Message) {
            $msg = "$msg | inner: $($_.Exception.InnerException.Message)"
        }
        throw "failed to start elevated installer: $msg"
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
$checksumsUrl = "$baseUrl/checksums.txt"

$checksumsText = Convert-ContentToText -Content ((Invoke-WebRequest -UseBasicParsing -Uri $checksumsUrl).Content)
$installerChecksum = Get-ChecksumForAsset -ChecksumsText $checksumsText -AssetName $installerAsset
$daemonChecksum = Get-ChecksumForAsset -ChecksumsText $checksumsText -AssetName $daemonAsset
$installerUrl = "$baseUrl/$installerAsset"

$tmpDir = Join-Path $env:TEMP "wg-feed-installer-cache"
$installerBin = Join-Path $tmpDir $installerAsset
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
