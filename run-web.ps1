# 使用 goproxy.cn 国内镜像运行
$env:GOPROXY = "https://goproxy.cn,direct"
$env:Path = "C:\Program Files\Go\bin;" + $env:Path

Set-Location $PSScriptRoot
npm --prefix web run dev
