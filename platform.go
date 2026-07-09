package main

import (
	"os"
	"strings"
)

func loadPlatformCookie(platform, cookieFile, flagCookie string) string {
	if c := strings.TrimSpace(flagCookie); c != "" {
		return c
	}
	envKey := "DOUYIN_COOKIE"
	if platform == "kuaishou" || platform == "ks" {
		envKey = "KUAISHOU_COOKIE"
	}
	if c := strings.TrimSpace(os.Getenv(envKey)); c != "" {
		return c
	}
	c := loadCookie(cookieFile, "")
	if c == "" {
		return ""
	}
	if platform == "kuaishou" || platform == "ks" {
		if isDouyinCookie(c) && !isKuaishouCookie(c) {
			return ""
		}
		return c
	}
	if isKuaishouCookie(c) && !isDouyinCookie(c) {
		return ""
	}
	return c
}

func defaultCookieFile(platform string) string {
	if platform == "kuaishou" || platform == "ks" {
		if _, err := os.Stat("config/kuaishou-cookie.txt"); err == nil {
			return "config/kuaishou-cookie.txt"
		}
		return "config/cookie.txt"
	}
	return "config/cookie.txt"
}

func isKuaishouCookie(c string) bool {
	return strings.Contains(c, "kuaishou") || strings.Contains(c, "client_key=") || strings.Contains(c, "did=web_")
}

func isDouyinCookie(c string) bool {
	return strings.Contains(c, "ttwid") || strings.Contains(c, "douyin") || strings.Contains(c, "__ac_nonce")
}

func loadPlatformRoomURL(platform, flagRoom string) string {
	if strings.TrimSpace(flagRoom) != "" {
		return flagRoom
	}
	if platform == "kuaishou" || platform == "ks" {
		if v := strings.TrimSpace(os.Getenv("KUAISHOU_ROOM_URL")); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(os.Getenv("DOUYIN_ROOM_URL")); v != "" {
		return v
	}
	return ""
}

func normalizePlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "ks", "kuaishou", "快手":
		return "kuaishou"
	default:
		return "douyin"
	}
}
