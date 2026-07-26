package douyin

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type BrowserAuthOptions struct {
	Headless   bool
	CookieFile string
	ProfileDir string
	Logger     *log.Logger
	Timeout    time.Duration
}

func FetchCookie(ctx context.Context, roomURL, existingCookie string, opts BrowserAuthOptions) (string, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 45 * time.Second
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}

	roomURL = normalizeLiveURL(roomURL)
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	allocatorOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", opts.Headless),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
		chromedp.UserAgent(defaultUserUA),
	)
	if opts.ProfileDir != "" {
		allocatorOpts = append(allocatorOpts, chromedp.UserDataDir(opts.ProfileDir))
	}
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, allocatorOpts...)
	defer allocCancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	opts.Logger.Printf("[抖音][browser] 打开直播间页面获取 Cookie: %s", roomURL)
	if err := chromedp.Run(browserCtx,
		network.Enable(),
		setBrowserCookies(existingCookie, opts.CookieFile),
		chromedp.Navigate(roomURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Sleep(3*time.Second),
	); err != nil {
		return "", fmt.Errorf("浏览器打开直播间失败: %w", err)
	}

	var cookies []*network.Cookie
	if err := chromedp.Run(browserCtx, chromedp.ActionFunc(func(c context.Context) error {
		var err error
		cookies, err = network.GetCookies().WithURLs([]string{
			"https://live.douyin.com",
			"https://www.douyin.com",
			"https://douyin.com",
		}).Do(c)
		return err
	})); err != nil {
		return "", fmt.Errorf("读取浏览器 Cookie 失败: %w", err)
	}

	header := cookiesToHeader(cookies)
	if header == "" {
		return "", fmt.Errorf("浏览器未获取到有效 Cookie，请尝试 -browser-headless=false 手动登录")
	}

	if opts.CookieFile != "" {
		if err := SaveNetscapeCookies(opts.CookieFile, cookies); err != nil {
			opts.Logger.Printf("[抖音][browser] 保存 Cookie 到 %s 失败: %v", opts.CookieFile, err)
		} else {
			opts.Logger.Printf("[抖音][browser] 已保存 Cookie 到 %s", opts.CookieFile)
		}
	}

	opts.Logger.Printf("[抖音][browser] 已获取 Cookie（%d 项）", strings.Count(header, "="))
	return header, nil
}

func normalizeLiveURL(roomURL string) string {
	roomURL = strings.TrimSpace(roomURL)
	if roomURL == "" {
		return "https://live.douyin.com"
	}
	if !strings.HasPrefix(roomURL, "http") {
		return "https://live.douyin.com/" + strings.Trim(roomURL, "/")
	}
	return roomURL
}

func setBrowserCookies(cookie, cookieFile string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		params := netscapeCookiesToBrowserParams(cookieFile, cookie)
		if len(params) == 0 {
			return nil
		}
		for _, baseURL := range []string{"https://live.douyin.com", "https://www.douyin.com"} {
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
