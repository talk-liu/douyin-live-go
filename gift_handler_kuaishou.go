package main

import (
	"hash/fnv"
	"log"
	"strconv"

	"github.com/XiaoMiku01/douyin-live-go/internal/kuaishou"
)

func setupKuaishouGiftHandlers(client *kuaishou.Client, interaction *GiftInteraction, debug bool) {
	var total int
	client.OnGift(func(g kuaishou.Gift) {
		ev := giftEventFromKuaishou(g)
		writeGiftLog(formatGiftLine("[礼物]", ev))
		interaction.Handle(ev)
	})
	if debug {
		client.OnDebug(func(msg kuaishou.DebugMessage) {
			total++
			if total <= 300 {
				log.Printf("[消息#%d] payloadType=%d size=%d", total, msg.PayloadType, msg.Size)
			}
		})
	}
}

func giftEventFromKuaishou(g kuaishou.Gift) GiftEvent {
	ev := GiftEvent{
		UserName:  g.UserName,
		UserID:    parseKuaishouUserID(g.UserID),
		GiftName:  g.GiftName,
		GiftID:    uint64(g.GiftID),
		Count:     g.Count,
		RepeatEnd: true,
	}
	if ev.Count == 0 {
		ev.Count = 1
	}
	return ev
}

func parseKuaishouUserID(id string) uint64 {
	if id == "" {
		return 0
	}
	if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		return n
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}
