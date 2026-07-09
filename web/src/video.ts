import './video.css'

const player = document.getElementById('player') as HTMLVideoElement
const hint = document.getElementById('hint') as HTMLDivElement

type VideoAssets = {
  videos?: string[]
  groups?: Record<string, string[]>
}

let playlist: string[] = []
let index = 0

function shuffle<T>(items: T[]): T[] {
  const list = [...items]
  for (let i = list.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[list[i], list[j]] = [list[j], list[i]]
  }
  return list
}

function pickRandomIndex(): number {
  if (playlist.length <= 1) {
    return 0
  }
  let next = index
  while (next === index) {
    next = Math.floor(Math.random() * playlist.length)
  }
  return next
}

function showHint(message: string) {
  hint.innerHTML = message
  hint.classList.remove('hidden')
}

function hideHint() {
  hint.classList.add('hidden')
}

function getDirParam(): string | null {
  const dir = new URLSearchParams(location.search).get('dir')?.trim()
  return dir || null
}

async function loadPlaylist(): Promise<string[]> {
  const res = await fetch('/api/assets/videos', { cache: 'no-store' })
  if (!res.ok) {
    throw new Error(`加载视频列表失败: ${res.status}`)
  }

  const body = (await res.json()) as VideoAssets
  const dir = getDirParam()

  if (dir) {
    const group = body.groups?.[dir]
    if (!group?.length) {
      const available = Object.keys(body.groups ?? {}).join('、') || '无'
      throw new Error(`目录 "${dir}" 下没有视频。可用目录：${available}`)
    }
    document.title = `视频循环 - ${dir}`
    return group
  }

  if (body.videos?.length) {
    return body.videos
  }

  return []
}

function playCurrent() {
  if (playlist.length === 0) {
    return
  }

  const src = playlist[index]
  if (player.getAttribute('src') !== src) {
    player.src = src
  }
  void player.play().catch(() => {
    showHint('自动播放被浏览器拦截，请点击页面后开始播放')
    const resume = () => {
      void player.play()
      hideHint()
      window.removeEventListener('pointerdown', resume)
    }
    window.addEventListener('pointerdown', resume, { once: true })
  })
}

function playNext() {
  if (playlist.length === 0) {
    return
  }
  index = pickRandomIndex()
  playCurrent()
}

player.addEventListener('ended', playNext)
player.addEventListener('error', () => {
  if (playlist.length <= 1) {
    showHint('视频加载失败，请检查 <code>assets/videos/</code> 中的文件格式')
    return
  }
  playNext()
})

async function init() {
  const dir = getDirParam()

  try {
    playlist = await loadPlaylist()
  } catch (err) {
    const message = err instanceof Error ? err.message : '加载视频列表失败'
    if (message.includes('可用目录')) {
      showHint(message)
    } else {
      showHint('无法连接后端，请先启动 <code>run.ps1</code>，再打开本页面')
    }
    return
  }

  if (playlist.length === 0) {
    showHint(
      '暂无视频。请将 mp4 / webm 放入 <code>assets/videos/boy</code> 或 <code>assets/videos/girl</code> 后刷新页面',
    )
    return
  }

  hideHint()
  playlist = shuffle(playlist)
  index = Math.floor(Math.random() * playlist.length)
  playCurrent()

  if (dir) {
    console.info(`[video] 播放目录: ${dir}`, playlist)
  }
}

void init()
