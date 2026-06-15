package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	douyinlive "github.com/jwwsjlm/douyinLive/v2"
	"github.com/jwwsjlm/douyinLive/v2/generated/new_douyin"
	"google.golang.org/protobuf/proto"
)

var (
	giftLogMu sync.Mutex
	giftLog   *os.File
)

// 需要监听的礼物相关消息（抖音可能走不同通道）
var giftMethodSet = map[string]struct{}{
	douyinlive.WebcastGiftMessage:         {},
	"WebcastLightGiftMessage":             {},
	"WebcastBindingGiftMessage":           {},
	"WebcastFansclubMessage":              {},
	"WebcastGiftIconFlashMessage":         {},
	"WebcastNotifyEffectMessage":          {},
}

func openGiftLog(path string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[礼物] 无法打开日志文件 %s: %v", path, err)
		return
	}
	giftLog = f
}

func writeGiftLog(line string) {
	log.Println(line)
	if giftLog == nil {
		return
	}
	giftLogMu.Lock()
	defer giftLogMu.Unlock()
	_, _ = giftLog.WriteString(line + "\n")
}

func isGiftMethod(method string) bool {
	if _, ok := giftMethodSet[method]; ok {
		return true
	}
	// 兜底：方法名含 Gift 但不是排序/投票等元数据
	if strings.Contains(method, "Gift") &&
		!strings.Contains(method, "GiftSort") &&
		!strings.Contains(method, "GiftVote") {
		return true
	}
	return false
}

func setupGiftHandlers(dl *douyinlive.DouyinLive, interaction *GiftInteraction, debug bool) {
	var total int
	dl.SubscribeMessage(func(msg *douyinlive.LiveMessage) {
		method := msg.GetMethod()
		if method == "" {
			return
		}
		total++
		if debug && total <= 300 {
			log.Printf("[消息#%d] %s payload=%d", total, method, len(msg.GetPayload()))
		}
		if !isGiftMethod(method) {
			return
		}
		log.Printf("[Gift通道] %s payload=%d", method, len(msg.GetPayload()))
		ev, ok := extractGiftEvent(method, msg.GetPayload())
		if !ok {
			log.Printf("[礼物] %s 收到但未能解析出礼物详情", method)
			return
		}
		writeGiftLog(formatGiftLine("[礼物]", ev))
		interaction.Handle(ev)
	})
}

func extractGiftEvent(method string, payload []byte) (GiftEvent, bool) {
	switch method {
	case douyinlive.WebcastGiftMessage:
		return fromGiftMessage(payload)
	case "WebcastLightGiftMessage":
		return fromLightGiftMessage(payload)
	case "WebcastBindingGiftMessage":
		return fromBindingGiftMessage(payload)
	case "WebcastFansclubMessage":
		return fromFansclubMessage(payload)
	default:
		return fromGiftMessage(payload)
	}
}

func fromGiftMessage(payload []byte) (GiftEvent, bool) {
	gift := &new_douyin.Webcast_Im_GiftMessage{}
	if err := proto.Unmarshal(payload, gift); err != nil {
		log.Printf("[礼物] GiftMessage 解析失败: %v", err)
		return GiftEvent{}, false
	}
	return fromGiftMessagePayload(gift)
}

func fromLightGiftMessage(payload []byte) (GiftEvent, bool) {
	light := &new_douyin.Webcast_Im_LightGiftMessage{}
	if err := proto.Unmarshal(payload, light); err != nil {
		return GiftEvent{}, false
	}
	ev := GiftEvent{RepeatEnd: true}
	if light.GetCommon() != nil && light.GetCommon().GetUser() != nil {
		ev.UserName = light.GetCommon().GetUser().GetNickname()
		ev.UserID = light.GetCommon().GetUser().GetId()
	}
	if light.GetGiftStruct() != nil {
		ev.GiftName = light.GetGiftStruct().GetName()
		ev.GiftID = light.GetGiftStruct().GetId()
		ev.DiamondCount = int32(light.GetGiftStruct().GetDiamondCount())
	}
	if light.GetGiftInfo() != nil {
		if ev.GiftID == 0 {
			ev.GiftID = light.GetGiftInfo().GetGiftId()
		}
		if ev.DiamondCount == 0 {
			ev.DiamondCount = int32(light.GetGiftInfo().GetDiamondCount())
		}
	}
	ev.Count = light.GetCount()
	if ev.Count == 0 {
		ev.Count = light.GetComboCount()
	}
	if ev.Count == 0 {
		ev.Count = 1
	}
	if ev.GiftName == "" {
		ev.GiftName = "礼物"
	}
	ev.TotalDiamond = uint64(ev.DiamondCount) * ev.Count
	return ev, true
}

func fromBindingGiftMessage(payload []byte) (GiftEvent, bool) {
	bind := &new_douyin.Webcast_Im_BindingGiftMessage{}
	if err := proto.Unmarshal(payload, bind); err != nil {
		return GiftEvent{}, false
	}
	if bind.GetMsg() != nil {
		return fromGiftMessagePayload(bind.GetMsg())
	}
	return GiftEvent{}, false
}

func fromGiftMessagePayload(gift *new_douyin.Webcast_Im_GiftMessage) (GiftEvent, bool) {
	ev := giftEventFromDouyin(gift)
	if ev.GiftName == "" && ev.GiftID == 0 {
		return GiftEvent{}, false
	}
	return ev, true
}

func fromFansclubMessage(payload []byte) (GiftEvent, bool) {
	fan := &new_douyin.Webcast_Im_FansclubMessage{}
	if err := proto.Unmarshal(payload, fan); err != nil {
		return GiftEvent{}, false
	}
	ev := GiftEvent{RepeatEnd: true, GiftName: "粉丝团灯牌", Count: 1}
	if fan.GetUser() != nil {
		ev.UserName = fan.GetUser().GetNickname()
		ev.UserID = fan.GetUser().GetId()
	}
	if fan.GetContent() != "" {
		ev.GiftName = fan.GetContent()
	}
	return ev, true
}

func formatGiftLine(prefix string, ev GiftEvent) string {
	return fmt.Sprintf("%s %s : %s x%d (钻石:%d, repeatEnd=%v)",
		prefix, ev.UserName, ev.GiftName, ev.Count, ev.TotalDiamond, ev.RepeatEnd)
}

func loadCookie(path string, flagCookie string) string {
	if c := strings.TrimSpace(flagCookie); c != "" {
		return c
	}
	if c := strings.TrimSpace(os.Getenv("DOUYIN_COOKIE")); c != "" {
		return c
	}
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(line, "=") {
			return line
		}
	}
	return ""
}
