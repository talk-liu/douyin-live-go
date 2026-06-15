# 使用 goproxy.cn 国内镜像运行
$env:GOPROXY = "https://goproxy.cn,direct"
$env:Path = "C:\Program Files\Go\bin;" + $env:Path

$room = "https://live.douyin.com/732174177525"
if ($args.Count -gt 0 -and $args[0]) {
    $room = $args[0]
} elseif ($env:DOUYIN_ROOM_URL) {
    $room = $env:DOUYIN_ROOM_URL
}

$extraArgs = @()
if ($args.Count -gt 1) {
    $extraArgs = $args[1..($args.Length - 1)]
}
$runArgs = @("-room", $room) + $extraArgs
if (Test-Path "gift-live.exe") {
    & .\gift-live.exe @runArgs
} else {
    & go run . @runArgs
}
