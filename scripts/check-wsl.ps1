param(
    [ValidateSet('all', 'build', 'shared', 'backend', 'monitoring', 'firewall', 'themes', 'api')]
    [string]$Stage = 'all'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$linuxRoot = (& wsl -d OPSD-CI -u ci --exec wslpath -u $projectRoot).Trim()
if ($LASTEXITCODE -ne 0) { throw '无法访问 OPSD-CI 发行版' }

& wsl -d OPSD-CI -u ci --exec mkdir -p /home/ci/opsd
if ($LASTEXITCODE -ne 0) { throw '无法准备本地验收目录' }
& wsl -d OPSD-CI -u ci --exec rsync -rlt --delete --exclude=node_modules --exclude=artifacts --exclude=.data --exclude=.impeccable "$linuxRoot/" /home/ci/opsd/
if ($LASTEXITCODE -ne 0) { throw '源码同步失败' }

& wsl -d OPSD-CI -u ci --cd /home/ci/opsd --exec bash scripts/check-local.sh $Stage
$checkCode = $LASTEXITCODE
& wsl -d OPSD-CI -u root --exec rsync -rt --omit-dir-times --exclude=node_modules --exclude=local-console-*/ /home/ci/opsd/artifacts/ "$linuxRoot/artifacts/wsl-ci/"
if ($LASTEXITCODE -ne 0) { throw '验收产物复制失败' }
if ($checkCode -ne 0) { throw "本地验收失败：$Stage，查看 artifacts/wsl-ci/local-ci/$Stage.log" }
Write-Output "本地验收通过：$Stage；产物位于 artifacts/wsl-ci/"
