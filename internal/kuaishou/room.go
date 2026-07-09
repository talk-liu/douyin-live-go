package kuaishou

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	apiHost       = "https://live.kuaishou.com"
	graphqlHost   = "https://live.kuaishou.com/live_graphql"
	defaultUserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	statePrefix    = "_STATE__="
	stateSuffix    = ";(function(){var s;(s=document.currentScript||document.scripts[document.scripts.length-1]).parentNode.r"
	stateJSONRe    = regexp.MustCompile(`_STATE__=(.*?);\(function\(\)\{var s;\(s=document\.currentScript\|\|document\.scripts\[document\.scripts\.length-1]\)\.parentNode\.r`)
	liveStreamIDRe = regexp.MustCompile(`"liveStream"\s*:\s*\{[^}]*"id"\s*:\s*"([^"]+)"`)
	principalIDRe  = regexp.MustCompile(`live\.kuaishou\.com/u/([^/?#]+)`)
)

type RoomInfo struct {
	PrincipalID  string
	LiveStreamID string
	IsLive       bool
	Token        string
	WebSocketURL string
	HeartbeatMs  uint64
}

type RoomResolver struct {
	client  *http.Client
	cookie  string
	liveURL string
	headers http.Header
}

func NewRoomResolver(liveURL, cookie string) *RoomResolver {
	liveURL = normalizeLiveURL(liveURL)
	headers := http.Header{}
	headers.Set("User-Agent", defaultUserUA)
	headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	headers.Set("Accept-Language", "zh-CN,zh;q=0.9")
	headers.Set("Referer", liveURL)
	if cookie != "" {
		headers.Set("Cookie", cookie)
	}
	return &RoomResolver{
		client:  newHTTPClient(cookie),
		cookie:  cookie,
		liveURL: liveURL,
		headers: headers,
	}
}

func normalizeLiveURL(liveURL string) string {
	liveURL = strings.TrimSpace(liveURL)
	if !strings.HasPrefix(liveURL, "http") {
		liveURL = "https://live.kuaishou.com/u/" + strings.Trim(liveURL, "/")
	}
	return liveURL
}

func ExtractPrincipalID(room string) string {
	room = strings.TrimSpace(room)
	if m := principalIDRe.FindStringSubmatch(room); len(m) > 1 {
		return m[1]
	}
	if u, err := url.Parse(room); err == nil && u.Path != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 2 && parts[0] == "u" {
			return parts[1]
		}
		if id := parts[len(parts)-1]; id != "" && id != "." {
			return id
		}
	}
	return room
}

// Resolve 按 kwai2 流程：页面取 liveStreamId → websocketinfo 鉴权（不以 livedetail 为准）。
func (r *RoomResolver) Resolve() (*RoomInfo, error) {
	principalID := ExtractPrincipalID(r.liveURL)
	if principalID == "" {
		return nil, fmt.Errorf("无法从地址解析快手主播 ID: %s", r.liveURL)
	}

	info := &RoomInfo{PrincipalID: principalID}

	liveStreamID, err := r.fetchLiveStreamIDFromPage(principalID)
	if err != nil {
		return nil, err
	}

	if liveStreamID == "" {
		detail, derr := r.fetchLiveDetail(principalID)
		if derr != nil {
			return nil, derr
		}
		liveStreamID = detail.LiveStreamID
		if detail.Token != "" {
			info.Token = detail.Token
		}
		if len(detail.WebSocketURL) > 0 {
			info.WebSocketURL = detail.WebSocketURL
		}
	}

	if liveStreamID == "" {
		return info, nil
	}
	info.LiveStreamID = liveStreamID

	if info.Token == "" || info.WebSocketURL == "" {
		wsInfo, wsErr := r.fetchWebSocketInfo(liveStreamID)
		if wsErr != nil {
			return nil, fmt.Errorf("liveStreamId=%s WebSocket 鉴权失败: %w", liveStreamID, wsErr)
		}
		info.Token = wsInfo.Token
		info.WebSocketURL = wsInfo.WebSocketURL
	}

	info.IsLive = true
	return info, nil
}

type liveDetailResult struct {
	AuthorLiving bool
	LiveStreamID string
	Token        string
	WebSocketURL string
}

func (r *RoomResolver) fetchLiveDetail(principalID string) (*liveDetailResult, error) {
	apiURL := fmt.Sprintf("%s/live_api/liveroom/livedetail?principalId=%s", apiHost, url.QueryEscape(principalID))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = cloneHeaders(r.headers, r.liveURL)
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			Result int `json:"result"`
			LiveStream struct {
				ID string `json:"id"`
			} `json:"liveStream"`
			Author struct {
				Living bool `json:"living"`
			} `json:"author"`
			WebsocketInfo struct {
				Token         string   `json:"token"`
				WebsocketURLs []string `json:"websocketUrls"`
			} `json:"websocketInfo"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	out := &liveDetailResult{
		AuthorLiving: payload.Data.Author.Living,
		LiveStreamID: payload.Data.LiveStream.ID,
		Token:        payload.Data.WebsocketInfo.Token,
	}
	if len(payload.Data.WebsocketInfo.WebsocketURLs) > 0 {
		out.WebSocketURL = payload.Data.WebsocketInfo.WebsocketURLs[0]
	}
	return out, nil
}

func (r *RoomResolver) fetchLiveStreamIDFromPage(principalID string) (string, error) {
	pageURL := fmt.Sprintf("%s/u/%s", apiHost, principalID)
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header = cloneHeaders(r.headers, r.liveURL)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	text := string(body)

	liveStreamID := ""
	if stateJSON := extractStateJSON(text); stateJSON != "" {
		var state struct {
			Liveroom struct {
				LiveStream struct {
					ID string `json:"id"`
				} `json:"liveStream"`
				PlayList []struct {
					LiveStream struct {
						ID string `json:"id"`
					} `json:"liveStream"`
					IsLiving bool `json:"isLiving"`
					ErrorType struct {
						Title string `json:"title"`
					} `json:"errorType"`
				} `json:"playList"`
			} `json:"liveroom"`
		}
		if err := json.Unmarshal([]byte(sanitizeStateJSON(stateJSON)), &state); err == nil {
			liveStreamID = state.Liveroom.LiveStream.ID
			for _, item := range state.Liveroom.PlayList {
				if item.ErrorType.Title != "" {
					if isRateLimitError(item.ErrorType.Title) {
						continue
					}
					return "", fmt.Errorf("快手页面返回: %s", item.ErrorType.Title)
				}
				if item.IsLiving && item.LiveStream.ID != "" {
					liveStreamID = item.LiveStream.ID
					break
				}
			}
		}
	}

	if liveStreamID == "" {
		if m := liveStreamIDRe.FindStringSubmatch(text); len(m) > 1 {
			liveStreamID = m[1]
		}
	}

	if strings.Contains(text, "请求过快") && liveStreamID == "" {
		return "", fmt.Errorf("快手页面限流，请稍后重试")
	}

	return liveStreamID, nil
}

func isRateLimitError(title string) bool {
	return strings.Contains(title, "请求过快") || strings.Contains(title, "稍后重试")
}

type websocketInfo struct {
	Token        string
	WebSocketURL string
}

func (r *RoomResolver) fetchWebSocketInfo(liveStreamID string) (*websocketInfo, error) {
	apiURL := fmt.Sprintf("%s/live_api/liveroom/websocketinfo?liveStreamId=%s", apiHost, url.QueryEscape(liveStreamID))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = cloneHeaders(r.headers, r.liveURL)
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			Result        int      `json:"result"`
			Token         string   `json:"token"`
			WebsocketURLs []string `json:"websocketUrls"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Data.Result != 1 || payload.Data.Token == "" || len(payload.Data.WebsocketURLs) == 0 {
		return nil, fmt.Errorf("websocketinfo result=%d", payload.Data.Result)
	}
	return &websocketInfo{
		Token:        payload.Data.Token,
		WebSocketURL: payload.Data.WebsocketURLs[0],
	}, nil
}

func (r *RoomResolver) FetchGiftTable() (map[uint32]string, error) {
	query := `query AllGifts { allGifts }`
	body, _ := json.Marshal(map[string]any{
		"operationName": "AllGifts",
		"variables":     map[string]any{},
		"query":         query,
	})
	req, err := http.NewRequest(http.MethodPost, graphqlHost, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header = cloneHeaders(r.headers, apiHost+"/")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload struct {
		Data struct {
			AllGifts string `json:"allGifts"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Data.AllGifts == "" {
		return map[uint32]string{}, nil
	}

	var gifts []struct {
		ID   uint32 `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(payload.Data.AllGifts), &gifts); err != nil {
		return nil, err
	}
	out := make(map[uint32]string, len(gifts))
	for _, g := range gifts {
		if g.Name != "" {
			out[g.ID] = g.Name
		}
	}
	return out, nil
}

func sanitizeStateJSON(raw string) string {
	raw = strings.ReplaceAll(raw, ":undefined", ":null")
	raw = strings.ReplaceAll(raw, ",undefined", ",null")
	raw = strings.ReplaceAll(raw, "[undefined", "[null")
	return raw
}

func extractStateJSON(html string) string {
	if m := stateJSONRe.FindStringSubmatch(html); len(m) > 1 {
		return m[1]
	}
	start := strings.Index(html, statePrefix)
	if start < 0 {
		return ""
	}
	start += len(statePrefix)
	end := strings.Index(html[start:], stateSuffix)
	if end < 0 {
		return ""
	}
	return html[start : start+end]
}
