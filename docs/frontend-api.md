# 抖音礼物 API — 前端接入文档

本文档描述 `douyin-live-go` 对外提供的 HTTP 接口，供前端 / 游戏客户端接入直播间礼物事件。

---

## 基础信息

| 项 | 说明 |
|---|---|
| 默认地址 | `http://127.0.0.1:8080` |
| 启动参数 | `-overlay 127.0.0.1:8080`（留空则关闭 HTTP 服务） |
| 协议 | HTTP / SSE（Server-Sent Events） |
| 跨域 | 所有 API 响应头含 `Access-Control-Allow-Origin: *` |
| 认证 | 无，仅本地/内网使用，勿暴露公网 |

---

## 推荐接入方式

```
1. GET /api/gifts          → 拉取最近历史 + 当前 seq
2. EventSource /api/gifts/stream  → 订阅实时礼物
3. 收到事件后，若 triggered === true，执行 action
```

**实时推送请优先使用 SSE**，轮询 `/api/gifts` 仅作兜底或初始化。

参考示例代码：`examples/gift-listener.js`  
也可通过 HTTP 获取：`GET /examples/gift-listener.js`

---

## 接口列表

### 1. 实时礼物流（SSE）— 主推

```
GET /api/gifts/stream
```

**Content-Type:** `text/event-stream`

建立长连接后，每收到一条礼物推送一行 SSE 消息：

```
data: {"seq":1,"UserName":"张三",...}

```

| 特性 | 说明 |
|---|---|
| 连接保活 | 建立连接后会先发送 `: ok` 注释行 |
| 断线重连 | 浏览器 `EventSource` 会自动重连；重连后只收到**新**礼物，需自行决定是否补拉历史 |
| 推送内容 | 每条 `data` 为完整 `GiftPayload` JSON（见下文数据模型） |

**示例：**

```javascript
const es = new EventSource('http://127.0.0.1:8080/api/gifts/stream');

es.onmessage = (e) => {
  const gift = JSON.parse(e.data);
  console.log(gift);
};

es.onerror = () => {
  console.warn('SSE 断开，浏览器会自动重连');
};
```

---

### 2. 礼物历史（轮询）

```
GET /api/gifts
```

**响应示例：**

```json
{
  "seq": 12,
  "events": [
    {
      "seq": 11,
      "UserName": "张三",
      "UserID": 123456789,
      "GiftName": "小心心",
      "GiftID": 463,
      "Count": 3,
      "DiamondCount": 1,
      "TotalDiamond": 3,
      "RepeatEnd": true,
      "say": "谢谢 张三 的小心心~",
      "action": {
        "type": "spawn",
        "params": {
          "entity": "slime",
          "amount": 3,
          "count": 3,
          "diamond": 3
        }
      },
      "triggered": true
    }
  ]
}
```

| 字段 | 说明 |
|---|---|
| `seq` | 当前最新事件的单调递增序号 |
| `events` | 最近 **50 条**礼物，按到达顺序排列（旧 → 新） |

**去重新事件：**

```javascript
let lastSeq = 0;

function onGift(payload) {
  if (payload.seq <= lastSeq) return;
  lastSeq = payload.seq;
  // 处理礼物...
}
```

---

### 3. 礼物配置（只读）

```
GET /api/config/gifts
```

返回服务端 `config/gifts.json` 的完整内容，供前端预览映射关系或调试。

**响应结构：**

```json
{
  "default": {
    "say": "感谢 {user} 送出的 {gift} x{count}！",
    "game": {
      "type": "toast",
      "params": { "message": "感谢 {user} 的 {gift}" }
    }
  },
  "gifts": {
    "小心心": {
      "say": "谢谢 {user} 的小心心~",
      "game": {
        "type": "spawn",
        "scale_by_count": true,
        "params": { "entity": "slime", "amount": 1 }
      }
    }
  }
}
```

> 礼物 → 动作的映射由服务端维护，前端**通常不需要**读此接口；事件里已附带解析好的 `action`。

---

### 4. 健康检查

```
GET /health
```

**响应：** `200 OK`，Body 为纯文本 `ok`

---

### 5. 礼物监控面板（调试）

```
GET /
```

内置 HTML 页面，可视化展示最近礼物及解析出的动作类型。

---

## 数据模型

### GiftPayload（礼物事件）

SSE 与 `/api/gifts` 返回的单条事件结构。

| 字段 | 类型 | 说明 |
|---|---|---|
| `seq` | number | 事件序号，单调递增，用于去重 |
| `UserName` | string | 送礼用户昵称 |
| `UserID` | number | 送礼用户 ID |
| `GiftName` | string | 礼物名称（如「小心心」「跑车」） |
| `GiftID` | number | 抖音礼物 ID |
| `Count` | number | 礼物数量（含连击累计） |
| `DiamondCount` | number | 单个礼物钻石价值 |
| `TotalDiamond` | number | 总钻石 = DiamondCount × Count |
| `RepeatEnd` | boolean | 连击是否结束；`true` 表示本轮连击已结束 |
| `say` | string | 口播/字幕文案（服务端已替换模板变量） |
| `action` | GameAction \| null | 解析后的游戏动作；无配置时为 `null` |
| `triggered` | boolean | **`true` 时前端应执行 `action`** |

### GameAction（游戏动作）

| 字段 | 类型 | 说明 |
|---|---|---|
| `type` | string | 动作类型，由配置定义，如 `spawn`、`heal`、`buff` |
| `params` | object | 动作参数；服务端已注入 `count`、`diamond`，并完成模板替换 |

**params 中常见字段（因配置而异）：**

| 字段 | 说明 |
|---|---|
| `count` | 礼物数量（始终注入） |
| `diamond` | 总钻石数（始终注入） |
| `amount` | 若配置了 `scale_by_count: true`，已乘以 Count |
| 其他 | 来自 `config/gifts.json` 的 `game.params` |

---

## 前端处理逻辑

### 必须遵守的规则

1. **只在 `triggered === true` 时执行游戏逻辑**  
   连击礼物（如一次送 99 个小心心）会推送多条事件，默认等 `RepeatEnd === true` 才触发，避免重复执行。

2. **用 `seq` 去重**  
   初始化拉历史 + SSE 实时推送可能 overlap，用 `seq` 跳过已处理事件。

3. **按 `action.type` 分发**  
   前端维护动作注册表，`type` → 处理函数。

### 参考实现

```javascript
const actionHandlers = {
  spawn: (params, event) => {
    // game.spawnEnemy(params.entity, params.amount);
  },
  heal: (params, event) => {
    // game.heal(params.hp);
  },
  toast: (params, event) => {
    // showToast(params.message);
  },
};

function handleGift(payload) {
  if (!payload.triggered || !payload.action) return;

  const { type, params = {} } = payload.action;
  const fn = actionHandlers[type];
  if (fn) fn(params, payload);
  else console.warn('未知动作:', type, params);
}

// 启动
async function start() {
  let lastSeq = 0;

  const res = await fetch('http://127.0.0.1:8080/api/gifts');
  const { seq, events = [] } = await res.json();
  lastSeq = seq;
  events.forEach(handleGift);

  const es = new EventSource('http://127.0.0.1:8080/api/gifts/stream');
  es.onmessage = (e) => {
    const payload = JSON.parse(e.data);
    if (payload.seq > lastSeq) {
      lastSeq = payload.seq;
      handleGift(payload);
    }
  };
}
```

---

## 连击礼物说明

抖音连击礼物一次可能推送多条 SSE 消息：

| 阶段 | RepeatEnd | triggered（默认） | 前端行为 |
|---|---|---|---|
| 连击进行中 | `false` | `false` | 可展示 UI，**不执行** action |
| 连击结束 | `true` | `true` | **执行** action |

若需要连击过程中每次都触发，需在服务端配置 `"trigger_on_repeat_end": false`（找后端改 `config/gifts.json`）。

---

## 配置与动作的对应关系

礼物映射在服务端 `config/gifts.json` 维护，前端只需实现 `action.type` 对应的 handler。

**配置示例：**

```json
{
  "gifts": {
    "小心心": {
      "game": {
        "type": "spawn",
        "scale_by_count": true,
        "params": {
          "entity": "slime",
          "amount": 1
        }
      }
    }
  }
}
```

**用户送 3 个小心心时，前端收到：**

```json
{
  "GiftName": "小心心",
  "Count": 3,
  "RepeatEnd": true,
  "triggered": true,
  "action": {
    "type": "spawn",
    "params": {
      "entity": "slime",
      "amount": 3,
      "count": 3,
      "diamond": 3
    }
  }
}
```

新增礼物类型时：**后端改配置 + 前端加 handler**，两边约定 `type` 字符串即可。

---

## 完整事件示例

### 小心心 ×3（触发 spawn）

```json
{
  "seq": 5,
  "UserName": "观众A",
  "UserID": 987654321,
  "GiftName": "小心心",
  "GiftID": 463,
  "Count": 3,
  "DiamondCount": 1,
  "TotalDiamond": 3,
  "RepeatEnd": true,
  "say": "谢谢 观众A 的小心心~",
  "action": {
    "type": "spawn",
    "params": {
      "entity": "slime",
      "amount": 3,
      "count": 3,
      "diamond": 3
    }
  },
  "triggered": true
}
```

### 跑车（触发 boss_rage）

```json
{
  "seq": 6,
  "UserName": "老板",
  "UserID": 111222333,
  "GiftName": "跑车",
  "GiftID": 888,
  "Count": 1,
  "DiamondCount": 1200,
  "TotalDiamond": 1200,
  "RepeatEnd": true,
  "say": "哇！老板 送出跑车！",
  "action": {
    "type": "boss_rage",
    "params": {
      "duration": 30,
      "intensity": 3,
      "count": 1,
      "diamond": 1200
    }
  },
  "triggered": true
}
```

### 连击进行中（不触发）

```json
{
  "seq": 7,
  "UserName": "观众B",
  "GiftName": "小心心",
  "Count": 10,
  "RepeatEnd": false,
  "action": {
    "type": "spawn",
    "params": { "entity": "slime", "amount": 10, "count": 10, "diamond": 10 }
  },
  "triggered": false
}
```

---

## 常见问题

**Q: 前端和 API 不在同一端口，能跨域吗？**  
A: 可以。所有接口已设置 `Access-Control-Allow-Origin: *`。SSE 同样支持跨域。

**Q: 如何知道有哪些 `action.type`？**  
A: 读 `GET /api/config/gifts` 查看当前配置；或问后端要 `config/gifts.json`。

**Q: 未配置的礼物怎么处理？**  
A: 走 `default` 配置。当前默认为 `type: "toast"`。

**Q: 服务重启后 seq 会重置吗？**  
A: 会。重启后 `seq` 从 1 重新计数，历史缓存清空。

**Q: 如何本地调试？**  
A: 启动服务后打开 `http://127.0.0.1:8080/` 看面板；或在直播间送礼物观察 SSE 输出。

---

## 变更记录

| 版本 | 说明 |
|---|---|
| v1 | 初始版本：`/api/gifts`、`/api/gifts/stream`、`/api/config/gifts`，GiftPayload 含 action / triggered |
