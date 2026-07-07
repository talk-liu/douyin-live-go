package main

import "github.com/jwwsjlm/douyinLive/v2/generated/new_douyin"

// GiftEvent 礼物事件，供互动逻辑使用
type GiftEvent struct {
	Seq          uint64 `json:"seq"`
	UserName     string `json:"UserName"`
	UserID       uint64 `json:"UserID"`
	GiftName     string `json:"GiftName"`
	GiftID       uint64 `json:"GiftID"`
	Count        uint64 `json:"Count"`
	DiamondCount int32  `json:"DiamondCount"`
	TotalDiamond uint64 `json:"TotalDiamond"`
	RepeatEnd    bool   `json:"RepeatEnd"`
}

// GameAction 前端/游戏侧要执行的动作
type GameAction struct {
	Type         string         `json:"type"`
	Params       map[string]any `json:"params,omitempty"`
	ScaleByCount bool           `json:"scale_by_count,omitempty"` // 为 true 时 params.amount *= Count
}

// GiftPayload 推送给前端的完整礼物事件（含已解析动作）
type GiftPayload struct {
	GiftEvent
	Say       string      `json:"say,omitempty"`
	Action    *GameAction `json:"action,omitempty"`
	Triggered bool        `json:"triggered"` // true 时前端应执行 action
}

func giftEventFromDouyin(msg *new_douyin.Webcast_Im_GiftMessage) GiftEvent {
	ev := GiftEvent{
		GiftID:    msg.GetGiftId(),
		Count:     msg.GetCount(),
		RepeatEnd: msg.GetRepeatEnd() == 1,
	}
	if ev.Count == 0 {
		ev.Count = msg.GetComboCount()
	}
	if ev.Count == 0 {
		ev.Count = 1
	}
	if msg.GetUser() != nil {
		ev.UserName = msg.GetUser().GetNickname()
		ev.UserID = msg.GetUser().GetId()
	}
	if msg.GetGift() != nil {
		ev.GiftName = msg.GetGift().GetName()
		ev.DiamondCount = msg.GetGift().GetDiamondCount()
	}
	ev.TotalDiamond = uint64(ev.DiamondCount) * ev.Count
	return ev
}