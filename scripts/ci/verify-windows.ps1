[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repo = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot "..\.."))
Set-Location $repo

$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile(
    (Join-Path $repo "scripts\install.ps1"),
    [ref]$tokens,
    [ref]$errors
) | Out-Null
if ($errors.Count -gt 0) {
    $errors | Format-List | Out-String | Write-Error
    exit 1
}

$bin = Join-Path $env:RUNNER_TEMP "missis-local-pair"
New-Item -ItemType Directory -Force $bin | Out-Null
& go build -o (Join-Path $bin "missis.exe") .\cmd\missis
if ($LASTEXITCODE -ne 0) { throw "missis build failed" }
& go build -o (Join-Path $bin "missis-tools.exe") .\tools\missis-tools
if ($LASTEXITCODE -ne 0) { throw "missis-tools build failed" }

$missis = (& (Join-Path $bin "missis.exe") --version --json | ConvertFrom-Json)
$tools = (& (Join-Path $bin "missis-tools.exe") --version --json | ConvertFrom-Json)
if ($missis.version -ne $tools.version -or
    $missis.display_version -ne $tools.display_version -or
    $missis.commit -ne $tools.commit -or
    $missis.dirty -ne $tools.dirty -or
    $missis.store_format_revision -ne 6 -or
    $tools.store_format_revision -ne 6 -or
    $missis.normal_open_format -ne 6 -or
    $tools.normal_open_format -ne 6 -or
    $missis.migration_set_digest -ne $tools.migration_set_digest) {
    throw "local binary identities do not match: missis=$($missis | ConvertTo-Json -Compress) tools=$($tools | ConvertTo-Json -Compress)"
}

& (Join-Path $bin "missis.exe") --help | Out-Null
if ($LASTEXITCODE -ne 0) { throw "missis command surface failed" }
& (Join-Path $bin "missis-tools.exe") --help | Out-Null
if ($LASTEXITCODE -ne 0) { throw "missis-tools command surface failed" }

Write-Host "Missis native Windows verification passed"
