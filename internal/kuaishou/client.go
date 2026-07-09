package kuaishou

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/XiaoMiku01/douyin-live-go/protobuf/kspb"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const (
	defaultHeartbeatInterval = 20 * time.Second
	pageIDCharset            = "-_zyxwvutsrqponmlkjihgfedcba9876543210ZYXWVUTSRQPONMLKJIHGFEDCBA"
)

type Gift struct {
	UserName string
	UserID   string
	GiftName string
	GiftID   uint32
	Count    uint64
}

type DebugMessage struct {
	PayloadType int32
	Size        int
}

type Client struct {
	liveURL string
	cookie  string
	logger  *log.Logger

	room    *RoomInfo
	gifts   map[uint32]string
	onGift  func(Gift)
	onDebug func(DebugMessage)

	connMu          sync.Mutex
	conn            *websocket.Conn
	closed          bool
	heartbeatTicker time.Duration
}

func NewClient(liveURL, cookie string, logger *log.Logger) *Client {
	if logger == nil {
		logger = log.Default()
	}
	return &Client{
		liveURL:         liveURL,
		cookie:          cookie,
		logger:          logger,
		gifts:           make(map[uint32]string),
		heartbeatTicker: defaultHeartbeatInterval,
	}
}

func (c *Client) OnGift(fn func(Gift)) {
	c.onGift = fn
}

func (c *Client) OnDebug(fn func(DebugMessage)) {
	c.onDebug = fn
}

func (c *Client) IsLive() (bool, error) {
	resolver := NewRoomResolver(c.liveURL, c.cookie)
	room, err := resolver.Resolve()
	if err != nil {
		return false, err
	}
	c.room = room
	return room.IsLive && room.LiveStreamID != "", nil
}

func (c *Client) loadGiftTable() {
	if len(c.gifts) > 0 {
		return
	}
	resolver := NewRoomResolver(c.liveURL, c.cookie)
	if table, err := resolver.FetchGiftTable(); err == nil && len(table) > 0 {
		c.gifts = table
		c.logger.Printf("[快手] 已加载礼物表 %d 个", len(table))
	}
}

func (c *Client) Start(ctx context.Context) error {
	if c.room == nil || !c.room.IsLive || c.room.LiveStreamID == "" {
		isLive, err := c.IsLive()
		if err != nil {
			return err
		}
		if !isLive {
			return fmt.Errorf("直播间未开播")
		}
	}

	if c.room.Token == "" || c.room.WebSocketURL == "" {
		resolver := NewRoomResolver(c.liveURL, c.cookie)
		wsInfo, err := resolver.fetchWebSocketInfo(c.room.LiveStreamID)
		if err != nil {
			return fmt.Errorf("获取 WebSocket 鉴权失败: %w", err)
		}
		c.room.Token = wsInfo.Token
		c.room.WebSocketURL = wsInfo.WebSocketURL
	}

	c.loadGiftTable()

	header := http.Header{}
	header.Set("User-Agent", defaultUserUA)
	header.Set("Origin", apiHost)
	if c.cookie != "" {
		header.Set("Cookie", c.cookie)
	}

	c.logger.Printf("[快手] 连接 WebSocket: %s", c.room.WebSocketURL)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.room.WebSocketURL, header)
	if err != nil {
		return fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.closed = false
	c.connMu.Unlock()

	if err := c.sendEnterRoom(); err != nil {
		_ = conn.Close()
		return fmt.Errorf("进房鉴权失败: %w", err)
	}

	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	go c.keepHeartbeat(heartbeatCtx)

	err = c.readLoop(ctx)
	cancelHeartbeat()

	c.connMu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.connMu.Unlock()
	return err
}

func (c *Client) Close() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) sendEnterRoom() error {
	msg := &kspb.CSWebEnterRoom{
		PayloadType: int64(kspb.PayloadType_CS_ENTER_ROOM),
		Payload: &kspb.CSWebEnterRoom_Payload{
			Token:          c.room.Token,
			LiveStreamId:   c.room.LiveStreamID,
			PageId:         newPageID(),
			ReconnectCount: 0,
		},
	}
	data, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return c.writeBinary(data)
}

func (c *Client) keepHeartbeat(ctx context.Context) {
	interval := c.heartbeatTicker
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			msg := &kspb.CSWebHeartbeat{
				PayloadType: int64(kspb.PayloadType_CS_HEARTBEAT),
				Payload: &kspb.CSWebHeartbeat_Payload{
					Timestamp: uint64(time.Now().UnixMilli()),
				},
			}
			data, err := proto.Marshal(msg)
			if err != nil {
				continue
			}
			if err := c.writeBinary(data); err != nil {
				return
			}
		}
	}
}

func (c *Client) writeBinary(data []byte) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn == nil || c.closed {
		return fmt.Errorf("连接已关闭")
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (c *Client) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()
		if conn == nil {
			return fmt.Errorf("连接已关闭")
		}

		if err := conn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
			return err
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if err := c.handleMessage(data); err != nil {
			c.logger.Printf("[快手] 处理消息失败: %v", err)
		}
	}
}

func (c *Client) handleMessage(data []byte) error {
	var msg kspb.SocketMessage
	if err := proto.Unmarshal(data, &msg); err != nil {
		return err
	}

	if c.onDebug != nil {
		c.onDebug(DebugMessage{PayloadType: int32(msg.PayloadType), Size: len(data)})
	}

	payload := msg.Payload
	if msg.CompressionType == kspb.CompressionType_GZIP {
		reader, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return err
		}
		decompressed, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return err
		}
		payload = decompressed
	}

	switch msg.PayloadType {
	case kspb.PayloadType_SC_ENTER_ROOM_ACK:
		var ack kspb.SCWebEnterRoomAck
		if err := proto.Unmarshal(payload, &ack); err != nil {
			return err
		}
		if ack.HeartbeatIntervalMs > 0 {
			c.heartbeatTicker = time.Duration(ack.HeartbeatIntervalMs) * time.Millisecond
			if c.room != nil {
				c.room.HeartbeatMs = ack.HeartbeatIntervalMs
			}
		}
		c.logger.Printf("[快手] 进房成功 liveStreamId=%s heartbeat=%ds", c.room.LiveStreamID, int(c.heartbeatTicker/time.Second))
	case kspb.PayloadType_SC_HEARTBEAT_ACK:
		// 心跳响应，保持连接
	case kspb.PayloadType_SC_FEED_PUSH:
		var feed kspb.SCWebFeedPush
		if err := proto.Unmarshal(payload, &feed); err != nil {
			return err
		}
		for _, gift := range feed.GiftFeeds {
			c.emitGift(gift)
		}
	case kspb.PayloadType_SC_AUTHOR_PUSH_TRAFFIC_ZERO:
		return fmt.Errorf("主播已下播")
	}
	return nil
}

func (c *Client) emitGift(gift *kspb.WebGiftFeed) {
	if gift == nil || c.onGift == nil {
		return
	}
	count := uint64(gift.ComboCount)
	if count == 0 {
		count = uint64(gift.BatchSize)
	}
	if count == 0 {
		count = 1
	}
	name := c.gifts[gift.GiftId]
	if name == "" {
		name = fmt.Sprintf("礼物#%d", gift.GiftId)
	}
	ev := Gift{
		GiftID:   gift.GiftId,
		GiftName: name,
		Count:    count,
	}
	if gift.User != nil {
		ev.UserName = gift.User.UserName
		ev.UserID = gift.User.PrincipalId
	}
	c.onGift(ev)
}

func newPageID() string {
	b := make([]byte, 16)
	for i := range b {
		b[i] = pageIDCharset[rand.Intn(len(pageIDCharset))]
	}
	return string(b) + "_" + fmt.Sprintf("%d", time.Now().UnixMilli())
}
