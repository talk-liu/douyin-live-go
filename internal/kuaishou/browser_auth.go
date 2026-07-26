package kuaishou

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type BrowserAuthOptions struct {
	Headless   bool
	CookieFile string
	Logger     *log.Logger
	Timeout    time.Duration
}

func (r *RoomResolver) resolveViaBrowser(ctx context.Context) (*RoomInfo, error) {
	opts := r.browserOpts
	if opts.Timeout <= 0 {
		opts.Timeout = 45 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}

	principalID := ExtractPrincipalID(r.liveURL)
	liveURL := normalizeLiveURL(r.liveURL)

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	allocatorOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", opts.Headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.UserAgent(defaultUserUA),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocatorOpts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	var (
		mu   sync.Mutex
		info *RoomInfo
	)
	pending := make(map[network.RequestID]string)
	authReady := make(chan struct{}, 1)

	notifyIfReady := func() {
		mu.Lock()
		ready := info != nil && info.LiveStreamID != "" && info.Token != "" && info.WebSocketURL != ""
		mu.Unlock()
		if ready {
			select {
			case authReady <- struct{}{}:
			default:
			}
		}
	}

	chromedp.ListenTarget(browserCtx, func(ev any) {
		switch ev := ev.(type) {
		case *network.EventResponseReceived:
			if !isAuthAPIURL(ev.Response.URL) {
				return
			}
			mu.Lock()
			pending[ev.RequestID] = ev.Response.URL
			mu.Unlock()
		case *network.EventLoadingFinished:
			mu.Lock()
			apiURL, ok := pending[ev.RequestID]
			if ok {
				delete(pending, ev.RequestID)
			}
			mu.Unlock()
			if !ok {
				return
			}
			reqID := ev.RequestID
			go func() {
				var body []byte
				err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(c context.Context) error {
					var err error
					body, err = network.GetResponseBody(reqID).Do(c)
					return err
				}))
				if err != nil || len(body) == 0 {
					return
				}
				parsed, err := roomInfoFromInterceptedAuth(body, apiURL, principalID)
				if err != nil {
					opts.Logger.Printf("[快手][browser] 解析 %s 失败: %v", apiURL, err)
					return
				}
				mu.Lock()
				info = mergeRoomInfo(info, parsed)
				mu.Unlock()
				opts.Logger.Printf("[快手][browser] 已拦截鉴权 liveStreamId=%s token=%t ws=%t",
					parsed.LiveStreamID, parsed.Token != "", parsed.WebSocketURL != "")
				notifyIfReady()
			}()
		}
	})

	opts.Logger.Printf("[快手][browser] 打开直播间页面拦截鉴权: %s", liveURL)
	runErr := chromedp.Run(browserCtx,
		network.Enable(),
		setBrowserCookies(r.cookie, opts.CookieFile),
		chromedp.Navigate(liveURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
	)

	waitUntil := time.Now().Add(15 * time.Second)
	for time.Now().Before(waitUntil) {
		mu.Lock()
		ready := info != nil && info.LiveStreamID != "" && info.Token != "" && info.WebSocketURL != ""
		mu.Unlock()
		if ready {
			break
		}
		select {
		case <-authReady:
		case <-time.After(300 * time.Millisecond):
		case <-ctx.Done():
			goto finish
		}
	}

finish:

	mu.Lock()
	result := info
	mu.Unlock()

	if result == nil || result.LiveStreamID == "" {
		if runErr != nil {
			return nil, fmt.Errorf("浏览器打开直播间失败: %w", runErr)
		}
		return nil, fmt.Errorf("浏览器未捕获 websocket 鉴权，请确认 Cookie 有效且直播间在播")
	}
	if result.Token == "" || result.WebSocketURL == "" {
		return nil, fmt.Errorf("浏览器捕获的鉴权信息不完整（缺少 token 或 websocket 地址）")
	}
	result.PrincipalID = principalID
	result.IsLive = true
	return result, nil
}

func isAuthAPIURL(rawURL string) bool {
	return strings.Contains(rawURL, "/live_api/liveroom/websocketinfo") ||
		strings.Contains(rawURL, "/live_api/liveroom/livedetail")
}

func setBrowserCookies(cookie, cookieFile string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		params := netscapeCookiesToBrowserParams(cookieFile, cookie)
		if len(params) == 0 {
			return fmt.Errorf("没有可用的 Cookie，请配置 config/cookie.txt")
		}
		for _, baseURL := range []string{"https://live.kuaishou.com", "https://www.kuaishou.com"} {
			batch := make([]*network.CookieParam, 0, len(params))
		for _, c := range params {
			batch = append(batch, &network.CookieParam{
				Name:     c.Name,
				Value:    c.Value,
				Path:     c.Path,
				Secure:   c.Secure,
				HTTPOnly: c.HTTPOnly,
				URL:      baseURL,
			})
		}
			if err := network.SetCookies(batch).Do(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func netscapeCookiesToBrowserParams(cookieFile, cookie string) []*network.CookieParam {
	if cookieFile != "" {
		if params := loadNetscapeBrowserCookies(cookieFile); len(params) > 0 {
			return params
		}
	}
	return cookieHeaderToBrowserParams(cookie)
}

func loadNetscapeBrowserCookies(path string) []*network.CookieParam {
	text, err := readTextFile(path)
	if err != nil {
		return nil
	}
	if !strings.Contains(text, "\tTRUE\t") && !strings.Contains(text, "# Netscape HTTP Cookie File") {
		return nil
	}
	var out []*network.CookieParam
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := fields[0]
		if !strings.Contains(domain, "kuaishou.com") {
			continue
		}
		name := fields[5]
		value := strings.Join(fields[6:], "\t")
		if name == "" || !isValidCookieValue(value) {
			continue
		}
		out = append(out, &network.CookieParam{
			Name:     name,
			Value:    value,
			Domain:   strings.TrimPrefix(domain, "."),
			Path:     fields[2],
			Secure:   strings.EqualFold(fields[3], "TRUE"),
			HTTPOnly: strings.EqualFold(fields[1], "TRUE"),
		})
	}
	return out
}

func cookieHeaderToBrowserParams(cookie string) []*network.CookieParam {
	var out []*network.CookieParam
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, value, ok := strings.Cut(part, "=")
		if !ok || name == "" || !isValidCookieValue(value) {
			continue
		}
		out = append(out, &network.CookieParam{
			Name:   name,
			Value:  value,
			Domain: "live.kuaishou.com",
			Path:   "/",
		})
	}
	return out
}

func readTextFile(path string) (string, error) {
	data, err := readFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
