# mem 빌드 스크립트 (Windows PowerShell 5.1)
#
#   .\build.ps1                     bin\mem.exe 만 만든다
#   .\build.ps1 -Install            만들고 mem install --apply 까지 (모델·DLL 도 받는다)
#   .\build.ps1 -Install -Bundle D:\mem-pack   인터넷 없이 꾸러미 폴더에서
#   .\build.ps1 -Install -NoPath    설치하되 사용자 PATH 는 안 건드린다 (시험용)
#
# CGO 는 끈다 — C 컴파일러 없이 빌드되는 것이 이 툴의 전제다.

[CmdletBinding()]
param(
    [switch]$Install,
    [switch]$NoPath,
    [string]$Bundle
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

# go build 는 **지금 폴더**의 모듈을 본다. 다른 Go 모듈 안에서 이 스크립트를
# 부르면 `outside main module` 로 죽는다 — 그래서 제 폴더로 옮겨 간다
# (실데이터 시험 D10 / T20).
Push-Location $root
try {

    # 1. Go 가 있나
    $go = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $go) {
        Write-Host "go 를 찾을 수 없다. https://go.dev/dl/ 에서 Go 1.26 이상을 깔고 새 터미널을 연다." -ForegroundColor Red
        exit 1
    }
    Write-Host (& go version)

    # 2. 빌드
    $exe = Join-Path $root 'bin\mem.exe'
    $env:CGO_ENABLED = '0'
    Write-Host "빌드 : $exe"
    & go build -trimpath -ldflags="-s -w" -o $exe (Join-Path $root 'cmd\mem')
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    $size = [math]::Round((Get-Item $exe).Length / 1MB, 1)
    Write-Host "됐다. $exe ($size MB)" -ForegroundColor Green

    # 3. -Install 을 줬을 때만 설치까지. 안 주면 exe 만 만들고 끝난다
    if (-not $Install) {
        Write-Host "설치까지 하려면 : .\build.ps1 -Install"
        exit 0
    }

    $installArgs = @('install', '--apply')
    # -NoPath 는 사용자 PATH(HKCU)를 안 건드린다. 시험 HOME 으로 돌릴 때 반드시
    # 준다 — 안 주면 시험 경로가 진짜 레지스트리에 박힌다 (실데이터 시험 D1).
    if ($NoPath) { $installArgs += '--no-path' }
    if ($Bundle) { $installArgs += @('--bundle', $Bundle) }
    Write-Host "설치 : mem $($installArgs -join ' ')"
    & $exe @installArgs
    exit $LASTEXITCODE
}
finally { Pop-Location }
