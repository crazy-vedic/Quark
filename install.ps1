# Quark install script for Windows (PowerShell)
# Usage: Invoke-RestMethod -Uri https://raw.githubusercontent.com/crazy-vedic/Quark/main/install.ps1 | Invoke-Expression
# Or with specific version: $env:QUARK_VERSION = "v1.0.0"; Invoke-RestMethod ...

$ErrorActionPreference = "Stop"

$repo = "crazy-vedic/Quark"
$binary = "quark"

function Add-BlockIfMissing {
    param(
        [Parameter(Mandatory = $true)][string]$TargetFile,
        [Parameter(Mandatory = $true)][string]$Marker,
        [Parameter(Mandatory = $true)][string]$Block
    )

    $parent = Split-Path -Parent $TargetFile
    if ($parent) {
        New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }
    if (-not (Test-Path $TargetFile)) {
        New-Item -ItemType File -Path $TargetFile -Force | Out-Null
    }

    $content = Get-Content -Path $TargetFile -Raw -ErrorAction SilentlyContinue
    if ($content -and $content.Contains($Marker)) {
        return $false
    }

    Add-Content -Path $TargetFile -Value "`r`n$Block`r`n"
    return $true
}

function Install-QuarkPowerShellCompletion {
    param(
        [Parameter(Mandatory = $true)][string]$BinaryPath
    )

    if (-not (Test-Path $BinaryPath)) {
        return
    }

    $completionDir = Join-Path $HOME "Documents\PowerShell\Completions"
    $completionPath = Join-Path $completionDir "quark.ps1"
    $profilePath = $PROFILE.CurrentUserAllHosts
    $marker = "# >>> quark completion >>>"

    New-Item -ItemType Directory -Path $completionDir -Force | Out-Null
    & $BinaryPath completion powershell | Out-File -FilePath $completionPath -Encoding utf8

    $block = @"
$marker
if (Test-Path "$completionPath") {
    . "$completionPath"
}
# <<< quark completion <<<
"@

    if (Add-BlockIfMissing -TargetFile $profilePath -Marker $marker -Block $block) {
        Write-Host "==> Enabled PowerShell completions in $profilePath" -ForegroundColor Green
    } else {
        Write-Host "==> Updated PowerShell completions at $completionPath" -ForegroundColor Green
    }
}

function Prompt-InstallQuarkPowerShellCompletion {
    param(
        [Parameter(Mandatory = $true)][string]$BinaryPath
    )

    try {
        $reply = Read-Host "Do you want to install auto-complete for quark? [Y/n] (Y)"
    } catch {
        Write-Host "==> Skipping PowerShell completion prompt (no interactive console available)" -ForegroundColor Yellow
        return
    }

    if ([string]::IsNullOrWhiteSpace($reply) -or $reply -match '^(?i:y|yes)$') {
        Install-QuarkPowerShellCompletion -BinaryPath $BinaryPath
    } else {
        Write-Host "==> Skipped PowerShell completion setup" -ForegroundColor Yellow
    }
}

# Allow version override via environment variable
$version = if ($env:QUARK_VERSION) { $env:QUARK_VERSION } else { "latest" }

# Allow platform override via environment variable
$platform = if ($env:QUARK_PLATFORM) { $env:QUARK_PLATFORM } else { "" }

# If platform is manually specified, validate it
if ($platform) {
    $valid = @("windows-amd64", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64")
    if ($valid -notcontains $platform) {
        Write-Host "Invalid platform: $platform" -ForegroundColor Red
        Write-Host "Supported: $($valid -join ', ')" -ForegroundColor Red
        exit 1
    }
} else {
    # Auto-detect
    $os = "windows"
    $arch = if ([System.Environment]::Is64BitOperatingSystem) { "amd64" } else { "amd64" }
    # Note: Windows ARM64 would require additional detection
    $platform = "$os-$arch"
}

# Windows is amd64 only for now
if ($platform -eq "windows-arm64") {
    Write-Host "Windows ARM64 not yet supported. Falling back to amd64." -ForegroundColor Yellow
    $platform = "windows-amd64"
}

$asset = "$binary-$platform.exe"

# Resolve download URL
if ($version -eq "latest") {
    $url = "https://github.com/$repo/releases/latest/download/$asset"
} else {
    $url = "https://github.com/$repo/releases/download/$version/$asset"
}

# Determine install directory
$installDir = if ($env:QUARK_INSTALL_DIR) {
    $env:QUARK_INSTALL_DIR
} else {
    # Prefer a directory that's likely on PATH
    $localBin = Join-Path $env:USERPROFILE ".local\bin"
    if (Test-Path $localBin) {
        $localBin
    } else {
        $binDir = Join-Path $env:USERPROFILE "bin"
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
        $binDir
    }
}

$installPath = Join-Path $installDir "$binary.exe"

Write-Host "==> Installing Quark $version for $platform" -ForegroundColor Cyan
Write-Host "==> Downloading $asset" -ForegroundColor Cyan

# Build headers with auth if available
$auth = if ($env:GITHUB_TOKEN) { $env:GITHUB_TOKEN } elseif ($env:GITHUB_ACCESS_TOKEN) { $env:GITHUB_ACCESS_TOKEN } else { $null }
$headers = @{}
if ($auth) { $headers["Authorization"] = "token $auth" }

try {
    Invoke-WebRequest -Uri $url -OutFile $installPath -UseBasicParsing -Headers $headers
} catch {
    Write-Host "Failed to download from $url" -ForegroundColor Red
    Write-Host "Error: $_" -ForegroundColor Red
    exit 1
}

Write-Host "==> Installed to $installPath" -ForegroundColor Green

# Check if install directory is on PATH
$pathDirs = $env:PATH -split ";"
$onPath = $pathDirs | Where-Object { $_ -eq $installDir }

if (-not $onPath) {
    Write-Host ""
    Write-Host "⚠️  $installDir is not on your PATH." -ForegroundColor Yellow
    Write-Host "   Add this directory to your PATH environment variable." -ForegroundColor Yellow
    Write-Host "   Or run:" -ForegroundColor Yellow
    Write-Host "   [Environment]::SetEnvironmentVariable('Path', `$env:Path + ';$installDir', 'User')" -ForegroundColor Yellow
}

Write-Host ""
Write-Host "✅ Quark $version installed!" -ForegroundColor Green
Write-Host "   Run: quark --help"
Write-Host ""
Prompt-InstallQuarkPowerShellCompletion -BinaryPath $installPath
