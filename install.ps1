# Install an official, checksum-verified release. Go is not required.
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12
$repo = 'Tiago-0liveira/bonsai'
$nativeArch = $env:PROCESSOR_ARCHITEW6432
if (!$nativeArch) { $nativeArch = $env:PROCESSOR_ARCHITECTURE }
switch ($nativeArch.ToUpperInvariant()) {
    'AMD64' { $arch = 'amd64' }
    'ARM64' { $arch = 'arm64' }
    default { throw "Unsupported architecture: $nativeArch" }
}
if ($env:PREFIX) { $binDir = Join-Path $env:PREFIX.Trim() 'bin' }
elseif ($env:GOBIN) { $binDir = $env:GOBIN }
elseif ($env:GOPATH) { $binDir = Join-Path ($env:GOPATH.Split(';')[0]) 'bin' }
else { $binDir = Join-Path $env:USERPROFILE 'go\bin' }
$temp = Join-Path ([IO.Path]::GetTempPath()) ('bonsai-install-' + [Guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
$staged = $null
try {
    $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
    $tag = $release.tag_name
    if ($tag -notmatch '^v\d+\.\d+\.\d+$') { throw 'No stable release found' }
    $asset = "bonsai_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/download/$tag"
    $archive = Join-Path $temp $asset
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile $archive
    $checksums = Join-Path $temp 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile $checksums
    $lines = @(Get-Content $checksums | Where-Object { $_ -match ('^[a-fA-F0-9]{64}\s+\*?' + [regex]::Escape($asset) + '$') })
    if ($lines.Count -ne 1) { throw 'Missing or invalid checksum' }
    $expected = ($lines[0] -split '\s+')[0]
    if ((Get-FileHash -Algorithm SHA256 $archive).Hash -ne $expected) { throw 'SHA256 checksum mismatch' }
    Expand-Archive $archive -DestinationPath $temp
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    $target = Join-Path $binDir 'bonsai.exe'
    $staged = Join-Path $binDir ('.bonsai-install-' + [Guid]::NewGuid() + '.exe')
    Copy-Item (Join-Path $temp 'bonsai.exe') $staged
    if (Test-Path $target) {
        $backup = "$target.old"
        if (Test-Path $backup) { Remove-Item $backup }
        Move-Item $target $backup
        try { Move-Item $staged $target } catch { Move-Item $backup $target; throw }
    } else { Move-Item $staged $target }
    Copy-Item (Join-Path $temp 'bcd.bat') (Join-Path $binDir 'bcd.bat') -Force
    Write-Host "Installed Bonsai $tag to $target"
    if ($env:PATH.Split(';') -notcontains $binDir) { Write-Host "Add $binDir to your user PATH." }
} finally {
    Remove-Item $temp -Recurse -Force
    if ($staged -and (Test-Path $staged)) { Remove-Item $staged -Force }
}
