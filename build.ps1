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

# gitLine 은 git 한 번을 돌리고 결과를 한 줄로 준다. 실패하면 빈 글이다.
# .git 이 없는 폴더에서도 빌드는 끝까지 가야 한다 — 실패를 여기서 다 먹는다.
function gitLine([string[]]$gitArgs) {
    try { $out = (& git -C $root @gitArgs) } catch { $out = '' }
    if ($LASTEXITCODE -ne 0) { $out = '' }
    $global:LASTEXITCODE = 0
    return ($out | Out-String).Trim()
}

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
    $stamp = (Get-Date).ToString('yyyy-MM-ddTHH:mm:ssK')

    # 어느 소스로 빌드했는지 박는다. `mem version` 이 이것을 찍어, 실행 파일이
    # 낡았는지 사람이 눈으로 본다. git 이 없거나 실패하면 빈 값이다.
    # 아직 커밋 안 한 소스로 빌드했으면 `-dirty` 를 붙인다. 해시만 보면 그
    # 커밋 그대로인 줄 아는데 사실은 손댄 판인 일이 잦다.
    $commit = ''
    if (Get-Command git -ErrorAction SilentlyContinue) {
        $commit = gitLine @('log', '-1', '--format=%h', '--', 'cmd', 'internal', 'go.mod')
        if ($commit -and (gitLine @('status', '--porcelain', '--', 'cmd', 'internal', 'go.mod'))) {
            $commit += '-dirty'
        }
    }

    $ldflags = "-s -w -X main.buildTime=$stamp -X main.buildCommit=$commit"
    Write-Host "빌드 : $exe"
    # -ldflags 와 값을 한 토큰으로 붙이면 PowerShell 5.1 이 변수를 안 푼다. 따로 넘긴다.
    & go build -trimpath '-ldflags' $ldflags -o $exe (Join-Path $root 'cmd\mem')
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
