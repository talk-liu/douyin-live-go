package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// GiftAction 单个礼物的互动配置
type GiftAction struct {
	Say     string `json:"say"`     // 无人直播口播/字幕文案，支持 {user} {gift} {count}
	Webhook string `json:"webhook"` // 可选：POST 礼物事件到外部服务（OBS、游戏等）
}

// GiftConfig 礼物互动配置
type GiftConfig struct {
	Default GiftAction            `json:"default"`
	Gifts   map[string]GiftAction `json:"gifts"`
}

// GiftInteraction 礼物互动处理器
type GiftInteraction struct {
	cfg    GiftConfig
	client *http.Client
	mu     sync.Mutex
	events []GiftEvent // 最近礼物，供 OBS 等轮询
	seq    uint64      // 单调递增序号，供前端检测新礼物
}

func LoadGiftConfig(path string) (GiftConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GiftConfig{}, err
	}
	var cfg GiftConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return GiftConfig{}, err
	}
	if cfg.Gifts == nil {
		cfg.Gifts = make(map[string]GiftAction)
	}
	return cfg, nil
}

func DefaultGiftConfig() GiftConfig {
	return GiftConfig{
		Default: GiftAction{
			Say: "感谢 {user} 送出的 {gift} x{count}！",
		},
		Gifts: map[string]GiftAction{
			"小心心": {Say: "谢谢 {user} 的小心心，爱你哟~"},
			"玫瑰":  {Say: "{user} 送来玫瑰，浪漫满分！"},
			"大啤酒": {Say: "{user} 请全场喝啤酒，干杯！"},
			"跑车":  {Say: "哇！{user} 送出跑车，老板大气！"},
		},
	}
}

func NewGiftInteraction(cfg GiftConfig) *GiftInteraction {
	return &GiftInteraction{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		events: make([]GiftEvent, 0, 32),
	}
}

func (g *GiftInteraction) Handle(ev GiftEvent) {
	g.mu.Lock()
	g.seq++
	ev.Seq = g.seq
	g.events = append(g.events, ev)
	if len(g.events) > 50 {
		g.events = g.events[len(g.events)-50:]
	}
	g.mu.Unlock()

	action := g.actionFor(ev.GiftName)
	say := g.render(action.Say, ev)
	if say != "" {
		log.Printf("[互动] %s", say)
	}

	if action.Webhook != "" {
		go g.postWebhook(action.Webhook, ev, say)
	}
}

func (g *GiftInteraction) actionFor(giftName string) GiftAction {
	if action, ok := g.cfg.Gifts[giftName]; ok {
		return action
	}
	return g.cfg.Default
}

func (g *GiftInteraction) render(tpl string, ev GiftEvent) string {
	if tpl == "" {
		return ""
	}
	r := strings.NewReplacer(
		"{user}", ev.UserName,
		"{gift}", ev.GiftName,
		"{count}", fmt.Sprintf("%d", ev.Count),
		"{diamond}", fmt.Sprintf("%d", ev.TotalDiamond),
	)
	return r.Replace(tpl)
}

func (g *GiftInteraction) postWebhook(url string, ev GiftEvent, say string) {
	body, _ := json.Marshal(map[string]any{
		"type":         "gift",
		"user":         ev.UserName,
		"user_id":      ev.UserID,
		"gift":         ev.GiftName,
		"gift_id":      ev.GiftID,
		"count":        ev.Count,
		"diamond":      ev.TotalDiamond,
		"say":          say,
		"timestamp":    time.Now().Unix(),
	})
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[webhook] 创建请求失败: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		log.Printf("[webhook] 请求失败: %v", err)
		return
	}
	resp.Body.Close()
}

func (g *GiftInteraction) RecentEvents() []GiftEvent {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]GiftEvent, len(g.events))
	copy(out, g.events)
	return out
}

// StartOverlayServer 启动 HTTP 服务：/ 礼物面板，/api/gifts JSON 数据
func (g *GiftInteraction) StartOverlayServer(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/gifts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(g.RecentEvents())
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(overlayHTML))
	})
	go func() {
		log.Printf("[overlay] 礼物面板: http://%s/  |  API: http://%s/api/gifts", addr, addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Printf("[overlay] HTTP 服务异常: %v", err)
		}
	}()
}

const overlayHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>礼物监控</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: "Microsoft YaHei", sans-serif; background: #0f0f1a; color: #fff; padding: 20px; }
  h1 { font-size: 22px; margin-bottom: 12px; color: #ffd700; }
  #status { color: #888; font-size: 13px; margin-bottom: 16px; }
  .gift { background: linear-gradient(135deg,#1a1a2e,#16213e); border-left: 4px solid #ffd700;
    padding: 12px 16px; margin-bottom: 10px; border-radius: 8px; animation: fadeIn .4s ease; }
  .gift .user { color: #7ec8ff; font-weight: bold; }
  .gift .name { color: #ffd700; font-size: 18px; }
  .gift .meta { color: #aaa; font-size: 12px; margin-top: 4px; }
  @keyframes fadeIn { from { opacity:0; transform:translateY(-8px); } to { opacity:1; transform:none; } }
</style>
</head>
<body>
<h1>🎁 直播间礼物监控</h1>
<div id="status">连接中...</div>
<div id="list"></div>
<script>
let lastSeq = 0;
function renderGifts(data) {
  const list = document.getElementById('list');
  list.innerHTML = data.slice().reverse().map(g =>
    '<div class="gift"><span class="user">' + esc(g.UserName||'匿名') + '</span> 送出 ' +
    '<span class="name">' + esc(g.GiftName||'?') + '</span> x' + (g.Count||1) +
    '<div class="meta">钻石: ' + (g.TotalDiamond||0) + '</div></div>'
  ).join('');
}
async function poll() {
  try {
    const res = await fetch('/api/gifts');
    const data = await res.json();
    const latestSeq = data.reduce((max, g) => Math.max(max, g.seq || 0), 0);
    document.getElementById('status').textContent =
      '已连接 | 显示最近 ' + data.length + ' 条 | 累计 ' + latestSeq + ' 条礼物';
    if (latestSeq !== lastSeq) {
      lastSeq = latestSeq;
      renderGifts(data);
    }
  } catch(e) {
    document.getElementById('status').textContent = '连接失败，请确认程序在运行';
  }
  setTimeout(poll, 1000);
}
function esc(s) { return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
poll();
</script>
</body>
</html>`
