import './video.css'

type GiftPayload = {
  seq: number
  UserName?: string
  GiftName: string
  RepeatEnd: boolean
}

/** 无礼物时的待机视频（循环） */
const IDLE_VIDEO = '日常.mp4'

/** 礼物名 → 视频文件名（播完一次后回到待机） */
const GIFT_VIDEOS: Record<string, string> = {
  小心心: '吃鸡腿.mp4',
}

const players = [
  document.getElementById('player-a') as HTMLVideoElement,
  document.getElementById('player-b') as HTMLVideoElement,
]
const hint = document.getElementById('hint') as HTMLDivElement
const badge = document.getElementById('badge') as HTMLDivElement

let activeIdx = 0
let lastSeq = 0
let mode: 'idle' | 'gift' = 'idle'
let switching = false

function activePlayer(): HTMLVideoElement {
  return players[activeIdx]
}

function backPlayer(): HTMLVideoElement {
  return players[1 - activeIdx]
}

function setVisible(idx: number) {
  players.forEach((player, i) => {
    player.classList.toggle('visible', i === idx)
  })
  activeIdx = idx
}

function videoURL(fileName: string): string {
  return `/videos/crocodile/${encodeURIComponent(fileName)}`
}

function showHint(message: string) {
  hint.innerHTML = message
  hint.classList.remove('hidden')
}

function hideHint() {
  hint.classList.add('hidden')
}

function showBadge(text: string) {
  badge.textContent = text
  badge.classList.remove('hidden')
}

function hideBadge() {
  badge.classList.add('hidden')
}

function onAutoplayBlocked() {
  showHint('自动播放被浏览器拦截，请点击页面后开始播放')
  const resume = () => {
    void activePlayer().play()
    hideHint()
    window.removeEventListener('pointerdown', resume)
  }
  window.addEventListener('pointerdown', resume, { once: true })
}

function waitForCanPlay(player: HTMLVideoElement): Promise<void> {
  if (player.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
    return Promise.resolve()
  }

  return new Promise((resolve, reject) => {
    const onReady = () => {
      cleanup()
      resolve()
    }
    const onError = () => {
      cleanup()
      reject(new Error('video load failed'))
    }
    const cleanup = () => {
      player.removeEventListener('canplay', onReady)
      player.removeEventListener('error', onError)
    }
    player.addEventListener('canplay', onReady)
    player.addEventListener('error', onError)
  })
}

async function switchToVideo(url: string, loop: boolean): Promise<void> {
  if (switching) {
    return
  }
  switching = true

  const current = activePlayer()
  const next = backPlayer()

  try {
    next.loop = loop
    if (next.getAttribute('src') !== url) {
      next.src = url
    }
    await waitForCanPlay(next)
    next.currentTime = 0
    await next.play().catch(onAutoplayBlocked)

    setVisible(1 - activeIdx)
    current.pause()
  } finally {
    switching = false
  }
}

async function playIdle() {
  mode = 'idle'
  hideBadge()
  await switchToVideo(videoURL(IDLE_VIDEO), true)
}

async function playGift(fileName: string, label?: string) {
  mode = 'gift'
  if (label) {
    showBadge(label)
  }
  await switchToVideo(videoURL(fileName), false)
}

function handleGift(gift: GiftPayload) {
  const file = GIFT_VIDEOS[gift.GiftName]
  if (!file) {
    return
  }

  if (mode !== 'idle') {
    return
  }

  const label = `${gift.UserName || '观众'} · ${gift.GiftName}`
  void playGift(file, label)
}

function onGift(gift: GiftPayload) {
  if (!gift.RepeatEnd) {
    return
  }
  handleGift(gift)
}

function onVideoEnded(event: Event) {
  if (event.target !== activePlayer() || mode !== 'gift') {
    return
  }
  void playIdle()
}

function onVideoError(event: Event) {
  if (event.target !== activePlayer()) {
    return
  }
  if (mode === 'gift') {
    void playIdle()
    return
  }
  showHint('视频加载失败，请确认 <code>assets/videos/crocodile/</code> 中有对应 mp4')
}

async function startGiftStream() {
  try {
    const res = await fetch('/api/gifts', { cache: 'no-store' })
    if (res.ok) {
      const body = (await res.json()) as { seq?: number }
      lastSeq = body.seq ?? 0
    }
  } catch {
    showHint('无法连接礼物服务，请先启动 <code>run.ps1</code>')
  }

  const es = new EventSource('/api/gifts/stream')
  es.onmessage = (event) => {
    try {
      const gift = JSON.parse(event.data) as GiftPayload
      if (!gift.seq || gift.seq <= lastSeq) {
        return
      }
      lastSeq = gift.seq
      onGift(gift)
    } catch {
      // ignore malformed payloads
    }
  }
  es.onopen = () => hideHint()
  es.onerror = () => {
    if (mode === 'idle' && !activePlayer().src) {
      showHint('礼物连接断开，正在重连…')
    }
  }
}

for (const player of players) {
  player.addEventListener('ended', onVideoEnded)
  player.addEventListener('error', onVideoError)
}

void (async () => {
  document.title = '鳄鱼互动'
  await playIdle()
  await startGiftStream()
})()
