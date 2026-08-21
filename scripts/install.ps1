[CmdletBinding()]
param(
    [string]$Ref = "",
    [string]$BinDir = "",
    [switch]$ToolsOnly
)

$ErrorActionPreference = "Stop"
$module = "github.com/ravinsharma7/missis"
if ([string]::IsNullOrWhiteSpace($Ref)) {
    $Ref = if ($env:MISSIS_REF) { $env:MISSIS_REF } else { "latest" }
}

$goos = (& go env GOOS).Trim()
$goexe = (& go env GOEXE).Trim()
if ($goos -ne "windows" -or $goexe -ne ".exe") {
    throw "PowerShell installer requires a native Windows Go target; got GOOS=$goos GOEXE=$goexe. Use scripts/install.sh inside WSL/Linux instead."
}

if ([string]::IsNullOrWhiteSpace($BinDir)) {
    if ($env:MISSIS_BIN_DIR) {
        $BinDir = $env:MISSIS_BIN_DIR
    } elseif ($env:GOBIN) {
        $BinDir = $env:GOBIN
    } else {
        $gopath = (& go env GOPATH).Trim()
        $BinDir = Join-Path $gopath "bin"
    }
}

$BinDir = [System.IO.Path]::GetFullPath($BinDir)
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
$pathEntries = @()
foreach ($entry in ($env:Path -split ';' | Where-Object { $_ })) {
    try {
        $pathEntries += [System.IO.Path]::GetFullPath($entry)
    } catch {
        # WSL can append POSIX entries to the environment inherited by a
        # native PowerShell process. They are not Windows paths, so ignore
        # them while checking the native install directory.
    }
}
if ($pathEntries -notcontains $BinDir) {
    throw "install directory is not on PATH: $BinDir; set `$env:Path = `"$BinDir;`$env:Path`" and rerun"
}

$oldGobin = $env:GOBIN
$env:GOBIN = $BinDir
try {
    if (-not $ToolsOnly) {
        Write-Host "installing $module/cmd/missis@$Ref to $BinDir"
        & go install "$module/cmd/missis@$Ref"
        if ($LASTEXITCODE -ne 0) { throw "go install missis failed with exit code $LASTEXITCODE" }
    }
    Write-Host "installing $module/tools/missis-tools@$Ref to $BinDir"
    & go install "$module/tools/missis-tools@$Ref"
    if ($LASTEXITCODE -ne 0) { throw "go install missis-tools failed with exit code $LASTEXITCODE" }
} finally {
    if ($null -eq $oldGobin) { Remove-Item Env:GOBIN -ErrorAction SilentlyContinue } else { $env:GOBIN = $oldGobin }
}

function Assert-NativeWindowsBinary([string]$Name) {
    $path = Join-Path $BinDir "$Name.exe"
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "installed binary is missing: $path"
    }
    $bytes = Get-Content -LiteralPath $path -Encoding Byte -TotalCount 2
    if ($bytes.Count -lt 2 -or $bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a) {
        throw "installed binary is not a native PE executable: $path"
    }
    $resolved = (Get-Command $Name -CommandType Application | Select-Object -First 1).Source
    if ([System.IO.Path]::GetFullPath($resolved) -ne [System.IO.Path]::GetFullPath($path)) {
        throw "command resolution selected $resolved instead of $path"
    }
}

if (-not $ToolsOnly) { Assert-NativeWindowsBinary "missis" }
Assert-NativeWindowsBinary "missis-tools"

Write-Host "installed native Windows Missis binaries in $BinDir"
if (-not $ToolsOnly) { Write-Host "missis: $((Get-Command missis -CommandType Application | Select-Object -First 1).Source)" }
Write-Host "missis-tools: $((Get-Command missis-tools -CommandType Application | Select-Object -First 1).Source)"
Write-Host "ref: $Ref"
