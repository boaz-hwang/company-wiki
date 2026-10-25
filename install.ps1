# Install the company-wiki binary from GitHub Releases.
#   irm https://raw.githubusercontent.com/boaz-hwang/company-wiki/main/install.ps1 | iex
# $env:COMPANY_WIKI_VERSION = 'v0.1.0' pins a release.
$ErrorActionPreference = 'Stop'
$repo = 'boaz-hwang/company-wiki'
if (-not [Environment]::Is64BitOperatingSystem) { throw 'company-wiki needs 64-bit Windows' }
$cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
$arch = if ($cpu -eq 'ARM64') { 'arm64' } else { 'amd64' }
$asset = "company-wiki_windows_$arch.zip"
$version = if ($env:COMPANY_WIKI_VERSION) { $env:COMPANY_WIKI_VERSION } else { 'latest' }
$base = if ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" } else { "https://github.com/$repo/releases/download/$version" }
$dir = Join-Path $env:LOCALAPPDATA 'Programs\company-wiki'
$tmp = Join-Path ([IO.Path]::GetTempPath()) ('company-wiki-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile (Join-Path $tmp $asset)
    Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')
    $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match " $([regex]::Escape($asset))$" }
    if (-not $line) { throw "$asset is not listed in checksums.txt" }
    $expected = ($line -split '\s+')[0]
    $actual = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
    if ($expected -ne $actual) { throw "checksum mismatch for $asset" }
    New-Item -ItemType Directory -Path $dir -Force | Out-Null
    Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath $tmp -Force
    Copy-Item (Join-Path $tmp 'company-wiki.exe') (Join-Path $dir 'company-wiki.exe') -Force
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force
}
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $dir) {
    [Environment]::SetEnvironmentVariable('Path', ($userPath.TrimEnd(';') + ';' + $dir).TrimStart(';'), 'User')
    Write-Host "Added $dir to your user PATH. Open a new terminal to use it."
}
$env:Path = "$env:Path;$dir"
& (Join-Path $dir 'company-wiki.exe') version
