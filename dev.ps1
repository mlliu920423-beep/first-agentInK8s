# Local dev bootstrap (PowerShell).
# Usage:
#   .\dev.ps1 web
#   .\dev.ps1 server   # requires $env:ARK_API_KEY and $env:ARK_MODEL_ID

param(
    [Parameter(Mandatory=$true, Position=0)]
    [ValidateSet('web', 'server')]
    [string]$Mode
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

switch ($Mode) {
    'web' {
        Set-Location web
        if (-not (Test-Path node_modules)) { npm install }
        npm run dev
    }
    'server' {
        if (-not $env:ARK_API_KEY -or -not $env:ARK_MODEL_ID) {
            Write-Error "Set both ARK_API_KEY and ARK_MODEL_ID env vars before running."
            exit 1
        }
        go run ./cmd/server
    }
}
