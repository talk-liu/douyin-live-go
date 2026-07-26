package kuaishou

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func parseLiveDetailBody(body []byte) (*liveDetailResult, error) {
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
	if err := json.Unmarshal(body, &payload); err != nil {
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

func parseWebSocketInfoBody(body []byte) (*websocketInfo, error) {
	var payload struct {
		Data struct {
			Result        int      `json:"result"`
			Token         string   `json:"token"`
			WebsocketURLs []string `json:"websocketUrls"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if payload.Data.Result != 1 || payload.Data.Token == "" || len(payload.Data.WebsocketURLs) == 0 {
		if payload.Data.Result == 2 {
			return nil, fmt.Errorf("websocketinfo 限流 result=2（请求过快，请降低重连频率）")
		}
		return nil, fmt.Errorf("websocketinfo result=%d", payload.Data.Result)
	}
	return &websocketInfo{
		Token:        payload.Data.Token,
		WebSocketURL: payload.Data.WebsocketURLs[0],
	}, nil
}

func roomInfoFromInterceptedAuth(body []byte, apiURL, principalID string) (*RoomInfo, error) {
	if strings.Contains(apiURL, "/live_api/liveroom/livedetail") {
		detail, err := parseLiveDetailBody(body)
		if err != nil {
			return nil, err
		}
		if detail.LiveStreamID == "" {
			return nil, fmt.Errorf("livedetail 未返回 liveStreamId")
		}
		info := &RoomInfo{
			PrincipalID:  principalID,
			LiveStreamID: detail.LiveStreamID,
			Token:        detail.Token,
			WebSocketURL: detail.WebSocketURL,
		}
		if info.Token != "" && info.WebSocketURL != "" {
			info.IsLive = true
		}
		return info, nil
	}

	if strings.Contains(apiURL, "/live_api/liveroom/websocketinfo") {
		ws, err := parseWebSocketInfoBody(body)
		if err != nil {
			return nil, err
		}
		liveStreamID := extractLiveStreamIDFromAPIURL(apiURL)
		if liveStreamID == "" {
			return nil, fmt.Errorf("websocketinfo 缺少 liveStreamId")
		}
		return &RoomInfo{
			PrincipalID:  principalID,
			LiveStreamID: liveStreamID,
			Token:        ws.Token,
			WebSocketURL: ws.WebSocketURL,
			IsLive:       true,
		}, nil
	}
	return nil, fmt.Errorf("未知鉴权接口: %s", apiURL)
}

func extractLiveStreamIDFromAPIURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Query().Get("liveStreamId"))
}

func mergeRoomInfo(base, incoming *RoomInfo) *RoomInfo {
	if base == nil {
		return incoming
	}
	if incoming == nil {
		return base
	}
	out := *base
	if incoming.LiveStreamID != "" {
		out.LiveStreamID = incoming.LiveStreamID
	}
	if incoming.Token != "" {
		out.Token = incoming.Token
	}
	if incoming.WebSocketURL != "" {
		out.WebSocketURL = incoming.WebSocketURL
	}
	if incoming.IsLive {
		out.IsLive = true
	}
	return &out
}
