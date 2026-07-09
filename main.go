package main

import (
	"context"
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
	"github.com/XiaoMiku01/douyin-live-go/internal/kuaishou"
)

var errNotLive = errors.New("直播间未开播")

func extractDouyinLiveID(room string) string {
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
	platformFlag := flag.String("platform", "kuaishou", "直播平台: douyin | kuaishou")
	roomURL := flag.String("room", "", "直播间地址")
	configPath := flag.String("config", "config/gifts.json", "礼物互动配置")
	overlayAddr := flag.String("overlay", "127.0.0.1:8080", "网页/OBS 服务地址，留空关闭")
	giftLogPath := flag.String("log", "gifts.log", "礼物日志文件，留空不写文件")
	debug := flag.Bool("debug", false, "打印所有消息类型（排查用）")
	cookie := flag.String("cookie", "", "平台 Cookie（登录网页版后从浏览器复制）")
	cookieFile := flag.String("cookie-file", "", "Cookie 文件路径（快手默认 config/kuaishou-cookie.txt）")
	flag.Parse()

	platform := normalizePlatform(*platformFlag)
	if *cookieFile == "" {
		*cookieFile = defaultCookieFile(platform)
	}
	*roomURL = loadPlatformRoomURL(platform, *roomURL)
	if *roomURL == "" {
		if platform == "kuaishou" {
			log.Fatal("请通过 -room 或 KUAISHOU_ROOM_URL 指定快手直播间地址，例如 https://live.kuaishou.com/u/主播ID")
		}
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

	cookieStr := loadPlatformCookie(platform, *cookieFile, *cookie)
	log.Printf("平台: %s | 直播间: %s", platform, *roomURL)
	if cookieStr != "" {
		log.Println("已加载 Cookie（登录态），连接成功率更高")
	} else if platform == "kuaishou" {
		log.Println("提示: 未配置快手 Cookie，将尝试匿名连接。如需登录态请设置 KUAISHOU_COOKIE 或 config/kuaishou-cookie.txt")
	} else {
		log.Println("提示: 未配置 Cookie，可能收不到礼物。请登录 live.douyin.com 后复制 Cookie 到 config/cookie.txt")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	switch platform {
	case "kuaishou":
		runKuaishou(ctx, cancel, *roomURL, cookieStr, interaction, frontendOnly, *debug)
	default:
		runDouyin(*roomURL, cookieStr, interaction, frontendOnly, *debug)
	}
}

func runDouyin(roomURL, cookieStr string, interaction *GiftInteraction, frontendOnly, debug bool) {
	liveID := extractDouyinLiveID(roomURL)
	log.Printf("正在连接抖音直播间 (liveID=%s)", liveID)

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

		setupGiftHandlers(instance, interaction, debug)
		dl = instance

		go func() {
			log.Println("直播间已确认开播，礼物监听已启动")
			if debug {
				log.Println("调试模式：前 300 条消息会打印类型")
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

	waitForExit(func() {
		dlMu.Lock()
		if dl != nil {
			dl.Close()
		}
		dlMu.Unlock()
	})
}

func runKuaishou(ctx context.Context, cancel context.CancelFunc, roomURL, cookieStr string, interaction *GiftInteraction, frontendOnly, debug bool) {
	log.Printf("正在连接快手直播间 (principalId=%s)", kuaishou.ExtractPrincipalID(roomURL))

	var (
		clientMu sync.Mutex
		client   *kuaishou.Client
	)

	startLive := func() error {
		clientMu.Lock()
		defer clientMu.Unlock()

		if client != nil {
			return nil
		}

		instance := kuaishou.NewClient(roomURL, cookieStr, log.Default())
		setupKuaishouGiftHandlers(instance, interaction, debug)

		isLive, err := instance.IsLive()
		if err != nil {
			return err
		}
		if !isLive {
			return errNotLive
		}

		client = instance
		go func() {
			log.Println("直播间已确认开播，礼物监听已启动")
			if debug {
				log.Println("调试模式：前 300 条消息会打印类型")
			}
			log.Println("终端和 gifts.log 会记录礼物")

			if err := instance.Start(ctx); err != nil && ctx.Err() == nil {
				log.Printf("直播连接结束: %v", err)
			}

			clientMu.Lock()
			if client == instance {
				client = nil
			}
			clientMu.Unlock()
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
		} else if frontendOnly {
			if strings.Contains(err.Error(), "限流") {
				log.Printf("快手接口限流: %v（将每 30 秒重试，也可更新 Cookie 后重启）", err)
			} else {
				log.Printf("连接直播间失败: %v（将每 30 秒重试）", err)
			}
		} else {
			log.Fatalf("连接直播间失败: %v", err)
		}
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := startLive(); err != nil && !errors.Is(err, errNotLive) {
					log.Printf("重连直播间失败: %v", err)
				}
			}
		}
	}()

	waitForExit(func() {
		cancel()
		clientMu.Lock()
		if client != nil {
			client.Close()
		}
		clientMu.Unlock()
	})
}

func waitForExit(cleanup func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("正在退出...")
	cleanup()
}
