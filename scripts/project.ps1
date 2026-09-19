[CmdletBinding()]
param(
    [Parameter(Mandatory = $true, Position = 0)]
    [ValidateSet('fmt', 'generate', 'test', 'vet', 'typecheck', 'build', 'check')]
    [string]$Target
)

$ErrorActionPreference = 'Stop'
Set-Location (Join-Path $PSScriptRoot '..')

function Invoke-Native {
    param(
        [Parameter(Mandatory = $true)]
        [string]$FilePath,
        [Parameter(Mandatory = $false)]
        [string[]]$Arguments = @()
    )

    & $FilePath @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$FilePath exited with code $LASTEXITCODE"
    }
}

function Invoke-AdminNpm {
    param(
        [Parameter(Mandatory = $true)]
        [string]$ScriptName
    )

    Push-Location 'web/admin'
    try {
        Invoke-Native 'npm' @('run', $ScriptName)
    }
    finally {
        Pop-Location
    }
}

function Invoke-Fmt {
    $files = @(Get-ChildItem -Path 'cmd', 'internal', 'web/public' -Filter '*.go' -Recurse -File | ForEach-Object { $_.FullName })
    if ($files.Count -gt 0) {
        Invoke-Native 'gofmt' (@('-w') + $files)
    }
}

function Invoke-Generate {
    Invoke-Native 'go' @('tool', 'sqlc', 'generate', '-f', 'db/sqlc.yaml')
    Invoke-AdminNpm 'generate:api'
}

function Invoke-Test {
    Invoke-Native 'go' @('test', './...')
    Invoke-AdminNpm 'test'
}

function Invoke-Vet {
    Invoke-Native 'go' @('vet', './...')
}

function Invoke-Typecheck {
    Invoke-AdminNpm 'typecheck'
}

function Invoke-Build {
    Invoke-Native 'go' @('build', './...')
    Invoke-AdminNpm 'build'
}

try {
    switch ($Target) {
        'fmt' { Invoke-Fmt }
        'generate' { Invoke-Generate }
        'test' { Invoke-Test }
        'vet' { Invoke-Vet }
        'typecheck' { Invoke-Typecheck }
        'build' { Invoke-Build }
        'check' {
            Invoke-Generate
            Invoke-Test
            Invoke-Vet
            Invoke-Typecheck
            Invoke-Build
        }
    }
}
catch {
    Write-Error $_
    exit 1
}
