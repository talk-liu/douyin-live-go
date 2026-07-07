/**
 * 抖音礼物 → 前端游戏 接入示例
 *
 * 1. 启动 douyin-live-go（默认 http://127.0.0.1:8080）
 * 2. 在页面中引入本脚本，或复制到你的游戏前端项目
 *
 * SSE 推送格式：
 * {
 *   seq, UserName, GiftName, Count, TotalDiamond, RepeatEnd, ...
 *   say: "口播文案",
 *   action: { type: "spawn", params: { entity: "slime", amount: 3, count: 3 } },
 *   triggered: true   // 为 true 时才执行 action（连击礼物会等 RepeatEnd）
 * }
 */

const GIFT_API = 'http://127.0.0.1:8080';

/** 动作注册表：type → 处理函数 */
const actionHandlers = {
  spawn: (params, event) => {
    console.log('[spawn]', params.entity, 'x', params.amount, 'from', event.UserName);
    // game.spawnEnemy(params.entity, params.amount);
  },
  heal: (params, event) => {
    console.log('[heal]', params.hp, 'HP for', event.UserName);
  },
  buff: (params, event) => {
    console.log('[buff]', params.buff, params.duration, 's');
  },
  boss_rage: (params, event) => {
    console.log('[boss_rage]', params.duration, 's intensity', params.intensity);
  },
  toast: (params, event) => {
    console.log('[toast]', params.message);
  },
  join_team: (params, event) => {
    console.log('[join_team]', event.UserName, '→', params.team);
  },
};

function handleGift(payload) {
  if (!payload.triggered || !payload.action) return;

  const { type, params = {} } = payload.action;
  const handler = actionHandlers[type];
  if (handler) {
    handler(params, payload);
  } else {
    console.warn('[gift] 未知动作类型:', type, params);
  }
}

/** 启动：先拉历史，再订阅 SSE */
export async function startGiftListener(apiBase = GIFT_API) {
  let lastSeq = 0;

  try {
    const res = await fetch(`${apiBase}/api/gifts`, { cache: 'no-store' });
    const body = await res.json();
    lastSeq = body.seq || 0;
    for (const ev of body.events || []) {
      handleGift(ev);
    }
  } catch (err) {
    console.warn('[gift] 加载历史失败，等待 SSE...', err);
  }

  const es = new EventSource(`${apiBase}/api/gifts/stream`);
  es.onmessage = (e) => {
    try {
      const payload = JSON.parse(e.data);
      if (payload.seq > lastSeq) {
        lastSeq = payload.seq;
        handleGift(payload);
      }
    } catch (_) {}
  };
  es.onerror = () => console.warn('[gift] SSE 断开，浏览器会自动重连');

  return () => es.close();
}

// 浏览器直接打开时自动启动
if (typeof window !== 'undefined') {
  startGiftListener();
}
