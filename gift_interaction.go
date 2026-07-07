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
	Say                string      `json:"say,omitempty"`
	Webhook            string      `json:"webhook,omitempty"`
	Game               *GameAction `json:"game,omitempty"`
	TriggerOnRepeatEnd *bool       `json:"trigger_on_repeat_end,omitempty"` // 默认 true：连击礼物等 RepeatEnd 再触发
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
	events []GiftPayload // 最近礼物，供前端轮询
	seq    uint64        // 单调递增序号，供前端检测新礼物

	subsMu sync.Mutex
	subs   map[chan GiftPayload]struct{}
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
			Game: &GameAction{
				Type: "toast",
				Params: map[string]any{
					"message": "感谢 {user} 的 {gift}",
				},
			},
		},
		Gifts: map[string]GiftAction{
			"小心心": {
				Say: "谢谢 {user} 的小心心，爱你哟~",
				Game: &GameAction{
					Type:         "spawn",
					ScaleByCount: true,
					Params:       map[string]any{"entity": "slime", "amount": 1},
				},
			},
			"玫瑰": {
				Say:  "{user} 送来玫瑰，浪漫满分！",
				Game: &GameAction{Type: "heal", Params: map[string]any{"hp": 50}},
			},
			"大啤酒": {
				Say:  "{user} 请全场喝啤酒，干杯！",
				Game: &GameAction{Type: "buff", Params: map[string]any{"buff": "attack_up", "duration": 10}},
			},
			"跑车": {
				Say:  "哇！{user} 送出跑车，老板大气！",
				Game: &GameAction{Type: "boss_rage", Params: map[string]any{"duration": 30, "intensity": 3}},
			},
		},
	}
}

func NewGiftInteraction(cfg GiftConfig) *GiftInteraction {
	return &GiftInteraction{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		events: make([]GiftPayload, 0, 32),
	}
}

func (g *GiftInteraction) Handle(ev GiftEvent) {
	g.mu.Lock()
	g.seq++
	ev.Seq = g.seq
	g.mu.Unlock()

	cfgAction := g.actionFor(ev.GiftName)
	payload := g.buildPayload(ev, cfgAction)

	g.mu.Lock()
	g.events = append(g.events, payload)
	if len(g.events) > 50 {
		g.events = g.events[len(g.events)-50:]
	}
	g.mu.Unlock()

	g.broadcast(payload)

	if payload.Say != "" {
		log.Printf("[互动] %s", payload.Say)
	}
	if payload.Triggered && payload.Action != nil {
		log.Printf("[游戏] %s → %s %v", ev.GiftName, payload.Action.Type, payload.Action.Params)
	}

	if cfgAction.Webhook != "" {
		go g.postWebhook(cfgAction.Webhook, payload)
	}
}

func (g *GiftInteraction) buildPayload(ev GiftEvent, cfg GiftAction) GiftPayload {
	say := g.render(cfg.Say, ev)
	action, triggered := g.resolveGameAction(cfg, ev)
	return GiftPayload{
		GiftEvent: ev,
		Say:       say,
		Action:    action,
		Triggered: triggered,
	}
}

func (g *GiftInteraction) resolveGameAction(cfg GiftAction, ev GiftEvent) (*GameAction, bool) {
	if cfg.Game == nil {
		return nil, false
	}
	triggerOnRepeatEnd := true
	if cfg.TriggerOnRepeatEnd != nil {
		triggerOnRepeatEnd = *cfg.TriggerOnRepeatEnd
	}
	triggered := !triggerOnRepeatEnd || ev.RepeatEnd

	params := g.renderParams(cfg.Game.Params, ev)
	if cfg.Game.ScaleByCount {
		if amount, ok := params["amount"]; ok {
			switch v := amount.(type) {
			case float64:
				params["amount"] = v * float64(ev.Count)
			case int:
				params["amount"] = v * int(ev.Count)
			case int64:
				params["amount"] = v * int64(ev.Count)
			}
		}
	}

	return &GameAction{
		Type:   cfg.Game.Type,
		Params: params,
	}, triggered
}

func (g *GiftInteraction) renderParams(params map[string]any, ev GiftEvent) map[string]any {
	out := map[string]any{
		"count":   ev.Count,
		"diamond": ev.TotalDiamond,
	}
	for k, v := range params {
		out[k] = g.renderValue(v, ev)
	}
	return out
}

func (g *GiftInteraction) renderValue(v any, ev GiftEvent) any {
	switch val := v.(type) {
	case string:
		return g.render(val, ev)
	case map[string]any:
		nested := make(map[string]any, len(val))
		for k, item := range val {
			nested[k] = g.renderValue(item, ev)
		}
		return nested
	default:
		return v
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

func (g *GiftInteraction) postWebhook(url string, payload GiftPayload) {
	body, _ := json.Marshal(map[string]any{
		"type":      "gift",
		"user":      payload.UserName,
		"user_id":   payload.UserID,
		"gift":      payload.GiftName,
		"gift_id":   payload.GiftID,
		"count":     payload.Count,
		"diamond":   payload.TotalDiamond,
		"say":       payload.Say,
		"action":    payload.Action,
		"triggered": payload.Triggered,
		"timestamp": time.Now().Unix(),
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

func (g *GiftInteraction) RecentEvents() []GiftPayload {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]GiftPayload, len(g.events))
	copy(out, g.events)
	return out
}

func (g *GiftInteraction) snapshot() (uint64, []GiftPayload) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]GiftPayload, len(g.events))
	copy(out, g.events)
	return g.seq, out
}

func (g *GiftInteraction) subscribe() chan GiftPayload {
	ch := make(chan GiftPayload, 16)
	g.subsMu.Lock()
	if g.subs == nil {
		g.subs = make(map[chan GiftPayload]struct{})
	}
	g.subs[ch] = struct{}{}
	g.subsMu.Unlock()
	return ch
}

func (g *GiftInteraction) unsubscribe(ch chan GiftPayload) {
	g.subsMu.Lock()
	delete(g.subs, ch)
	g.subsMu.Unlock()
	close(ch)
}

func (g *GiftInteraction) broadcast(payload GiftPayload) {
	g.subsMu.Lock()
	defer g.subsMu.Unlock()
	for ch := range g.subs {
		select {
		case ch <- payload:
		default:
		}
	}
}

// StartOverlayServer 启动 HTTP 服务：/ 礼物面板，/api/gifts JSON，/api/gifts/stream SSE，/api/config/gifts 配置
func (g *GiftInteraction) StartOverlayServer(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config/gifts", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(g.cfg)
	})
	mux.HandleFunc("/api/gifts", func(w http.ResponseWriter, r *http.Request) {
		seq, events := g.snapshot()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"seq":    seq,
			"events": events,
		})
	})
	mux.HandleFunc("/api/gifts/stream", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		ch := g.subscribe()
		defer g.unsubscribe(ch)

		_, _ = fmt.Fprintf(w, ": ok\n\n")
		flusher.Flush()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(ev)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/examples/gift-listener.js", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("examples/gift-listener.js")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(data)
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
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(overlayHTML))
	})
	go func() {
		log.Printf("[overlay] 礼物面板: http://%s/  |  API: /api/gifts  |  SSE: /api/gifts/stream  |  配置: /api/config/gifts", addr)
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
    padding: 12px 16px; margin-bottom: 10px; border-radius: 8px; }
  .gift.new { animation: fadeIn .4s ease; }
  .gift .user { color: #7ec8ff; font-weight: bold; }
  .gift .name { color: #ffd700; font-size: 18px; }
  .gift .action { color: #7dff7d; font-size: 12px; margin-top: 4px; font-family: monospace; }
  @keyframes fadeIn { from { opacity:0; transform:translateY(-8px); } to { opacity:1; transform:none; } }
</style>
</head>
<body>
<h1>🎁 直播间礼物监控</h1>
<div id="status">连接中...</div>
<div id="list"></div>
<script>
const MAX_DISPLAY = 50;
let total = 0;

function esc(s) { return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }

function giftHTML(g, animate) {
  const cls = animate ? 'gift new' : 'gift';
  let action = '';
  if (g.action && g.action.type) {
    action = '<div class="action">→ ' + esc(g.action.type) +
      (g.triggered ? '' : ' (等待连击结束)') + '</div>';
  }
  return '<div class="' + cls + '"><span class="user">' + esc(g.UserName||'匿名') + '</span> 送出 ' +
    '<span class="name">' + esc(g.GiftName||'?') + '</span> x' + (g.Count||1) +
    '<div class="meta">钻石: ' + (g.TotalDiamond||0) + '</div>' + action + '</div>';
}

function updateStatus() {
  const n = document.getElementById('list').children.length;
  document.getElementById('status').textContent =
    '实时连接 | 显示最近 ' + n + ' 条 | 累计 ' + total + ' 条礼物';
}

function prependGift(g) {
  const list = document.getElementById('list');
  list.insertAdjacentHTML('afterbegin', giftHTML(g, true));
  while (list.children.length > MAX_DISPLAY) {
    list.removeChild(list.lastChild);
  }
  total = g.seq || total;
  updateStatus();
}

async function init() {
  try {
    const res = await fetch('/api/gifts', { cache: 'no-store' });
    const body = await res.json();
    const data = body.events || [];
    total = body.seq || 0;
    const list = document.getElementById('list');
    list.innerHTML = data.slice().reverse().map(g => giftHTML(g, false)).join('');
    updateStatus();
  } catch (e) {
    document.getElementById('status').textContent = '加载历史失败，等待实时推送...';
  }

  const es = new EventSource('/api/gifts/stream');
  es.onmessage = (e) => {
    try { prependGift(JSON.parse(e.data)); } catch (_) {}
  };
  es.onopen = () => updateStatus();
  es.onerror = () => {
    document.getElementById('status').textContent = '连接断开，正在重连... | 累计 ' + total + ' 条礼物';
  };
}
init();
</script>
</body>
</html>`
