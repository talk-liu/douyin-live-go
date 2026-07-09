package main

import (
	"errors"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	douyinlive "github.com/jwwsjlm/douyinLive/v2"
)

var errNotLive = errors.New("直播间未开播")

func extractLiveID(room string) string {
	room = strings.TrimSpace(room)
	if m := regexp.MustCompile(`live\.douyin\.com/(\d+)`).FindStringSubmatch(room); len(m) > 1 {
		return m[1]
	}
	if u, err := url.Parse(room); err == nil && u.Path != "" {
		if id := strings.Trim(path.Base(u.Path), "/"); id != "" && id != "." {
			return id
		}
	}
	return room
}

func main() {
	roomURL := flag.String("room", "", "抖音直播间地址")
	configPath := flag.String("config", "config/gifts.json", "礼物互动配置")
	overlayAddr := flag.String("overlay", "127.0.0.1:8080", "网页/OBS 服务地址，留空关闭")
	giftLogPath := flag.String("log", "gifts.log", "礼物日志文件，留空不写文件")
	debug := flag.Bool("debug", false, "打印所有消息类型（排查用）")
	cookie := flag.String("cookie", "", "抖音 Cookie（登录 live.douyin.com 后从浏览器复制）")
	cookieFile := flag.String("cookie-file", "config/cookie.txt", "Cookie 文件路径")
	flag.Parse()

	if *roomURL == "" {
		*roomURL = os.Getenv("DOUYIN_ROOM_URL")
	}
	if *roomURL == "" {
		*roomURL = "https://live.douyin.com/732174177525"
	}

	openGiftLog(*giftLogPath)

	cfg, err := LoadGiftConfig(*configPath)
	if err != nil {
		log.Printf("未找到配置文件 %s，使用默认规则", *configPath)
		cfg = DefaultGiftConfig()
	}

	interaction := NewGiftInteraction(cfg)
	frontendOnly := *overlayAddr != ""
	if frontendOnly {
		interaction.StartOverlayServer(*overlayAddr)
		log.Println("前端服务已启动（未开播也可访问页面）")
		log.Println("视频资源目录: assets/videos/  （放入 mp4/webm 后访问 /video）")
		log.Println("浏览器打开 http://" + *overlayAddr + "/video 可查看视频循环页")
		log.Println("浏览器打开 http://" + *overlayAddr + "/gifts 可查看礼物面板")
	}

	liveID := extractLiveID(*roomURL)
	log.Printf("正在连接直播间: %s (liveID=%s)", *roomURL, liveID)

	cookieStr := loadCookie(*cookieFile, *cookie)
	if cookieStr != "" {
		log.Println("已加载 Cookie（登录态），礼物消息成功率更高")
	} else {
		log.Println("提示: 未配置 Cookie，可能收不到礼物。请登录 live.douyin.com 后复制 Cookie 到 config/cookie.txt")
	}

	var (
		dlMu sync.Mutex
		dl   *douyinlive.DouyinLive
	)

	startLive := func() error {
		dlMu.Lock()
		defer dlMu.Unlock()

		if dl != nil {
			return nil
		}

		instance, err := douyinlive.NewDouyinLive(liveID, log.Default(), cookieStr)
		if err != nil {
			return err
		}

		isLive, err := instance.IsLive()
		if err != nil {
			instance.Close()
			return err
		}
		if !isLive {
			instance.Close()
			return errNotLive
		}

		setupGiftHandlers(instance, interaction, *debug)
		dl = instance

		go func() {
			log.Println("直播间已确认开播，礼物监听已启动")
			if *debug {
				log.Println("调试模式：前 200 条消息会打印类型")
			}
			log.Println("终端和 gifts.log 会记录礼物")

			if err := instance.Start(); err != nil {
				log.Printf("直播连接结束: %v", err)
			}

			dlMu.Lock()
			if dl == instance {
				dl = nil
			}
			dlMu.Unlock()
			log.Println("礼物监听已断开，将尝试重新连接...")
		}()

		return nil
	}

	if err := startLive(); err != nil {
		if errors.Is(err, errNotLive) {
			if frontendOnly {
				log.Println("直播间当前未开播，前端页面仍可访问；开播后将自动连接礼物监听")
			} else {
				log.Fatal("直播间当前未开播，请开播后再运行")
			}
		} else {
			log.Fatalf("连接直播间失败: %v", err)
		}
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := startLive(); err != nil && !errors.Is(err, errNotLive) {
				log.Printf("重连直播间失败: %v", err)
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("正在退出...")
	dlMu.Lock()
	if dl != nil {
		dl.Close()
	}
	dlMu.Unlock()
}
