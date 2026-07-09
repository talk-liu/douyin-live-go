# 使用 goproxy.cn 国内镜像运行（默认快手平台）
$env:GOPROXY = "https://goproxy.cn,direct"
$env:Path = "C:\Program Files\Go\bin;" + $env:Path

$room = ""
if ($args.Count -gt 0 -and $args[0]) {
    $room = $args[0]
} elseif ($env:KUAISHOU_ROOM_URL) {
    $room = $env:KUAISHOU_ROOM_URL
} elseif ($env:DOUYIN_ROOM_URL) {
    $room = $env:DOUYIN_ROOM_URL
}

$extraArgs = @("-platform", "kuaishou")
if ($room) {
    $extraArgs += @("-room", $room)
}
if ($args.Count -gt 1) {
    $extraArgs += $args[1..($args.Length - 1)]
}
if (Test-Path "gift-live.exe") {
    & .\gift-live.exe @extraArgs
} else {
    & go run . @extraArgs
}
