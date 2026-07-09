package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var videoExtensions = map[string]bool{
	".mp4":  true,
	".webm": true,
	".mov":  true,
	".m4v":  true,
	".ogg":  true,
}

const (
	videoAssetsDir = "assets/videos"
	webDistDir     = "web/dist"
)

func listVideoAssets() (videos []string, groups map[string][]string) {
	groups = map[string][]string{}

	_ = filepath.WalkDir(videoAssetsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if !videoExtensions[ext] {
			return nil
		}

		rel, err := filepath.Rel(videoAssetsDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		url := "/videos/" + rel
		videos = append(videos, url)

		group := "default"
		if parts := strings.Split(rel, "/"); len(parts) > 1 {
			group = parts[0]
		}
		groups[group] = append(groups[group], url)
		return nil
	})

	sortVideoURLs(videos)
	for name := range groups {
		sortVideoURLs(groups[name])
	}
	return videos, groups
}

func sortVideoURLs(urls []string) {
	sort.Slice(urls, func(i, j int) bool {
		return compareVideoPath(urls[i], urls[j]) < 0
	})
}

func compareVideoPath(a, b string) int {
	partsA := strings.Split(strings.TrimPrefix(a, "/videos/"), "/")
	partsB := strings.Split(strings.TrimPrefix(b, "/videos/"), "/")

	for k := 0; k < len(partsA)-1 && k < len(partsB)-1; k++ {
		if c := strings.Compare(partsA[k], partsB[k]); c != 0 {
			return c
		}
	}
	return compareNumericName(partsA[len(partsA)-1], partsB[len(partsB)-1])
}

func compareNumericName(a, b string) int {
	numA, errA := strconv.Atoi(strings.TrimSuffix(a, filepath.Ext(a)))
	numB, errB := strconv.Atoi(strings.TrimSuffix(b, filepath.Ext(b)))
	if errA == nil && errB == nil && numA != numB {
		if numA < numB {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func mountVideoAssets(mux *http.ServeMux) {
	_ = os.MkdirAll(videoAssetsDir, 0o755)

	mux.HandleFunc("/api/assets/videos", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		videos, groups := listVideoAssets()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"videos": videos,
			"groups": groups,
			"dir":    videoAssetsDir,
		})
	})

	mux.Handle("/videos/", http.StripPrefix("/videos/", safeFileServer(videoAssetsDir)))
}

func mountWebDist(mux *http.ServeMux, addr string) bool {
	if _, err := os.Stat(webDistDir); err != nil {
		return false
	}

	fileServer := http.FileServer(http.Dir(webDistDir))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		switch path {
		case "/gifts":
			serveOverlayHTML(w)
			return
		case "/video", "/video/":
			serveWebFile(w, r, filepath.Join(webDistDir, "video.html"))
			return
		case "/":
			serveWebFile(w, r, filepath.Join(webDistDir, "index.html"))
			return
		}

		clean := filepath.Clean(path)
		if strings.Contains(clean, "..") {
			http.NotFound(w, r)
			return
		}

		full := filepath.Join(webDistDir, strings.TrimPrefix(clean, "/"))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		http.NotFound(w, r)
	})

	log.Printf("[overlay] 前端: http://%s/  |  视频页: http://%s/video  |  礼物面板: http://%s/gifts", addr, addr, addr)
	return true
}

func mountLegacyOverlay(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveOverlayHTML(w)
	})
}

func serveOverlayHTML(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(overlayHTML))
}

func serveWebFile(w http.ResponseWriter, r *http.Request, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(filePath, ".html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", http.DetectContentType(data))
	}
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func safeFileServer(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := filepath.Clean(r.URL.Path)
		if clean == "." || clean == "/" || strings.Contains(clean, "..") {
			http.NotFound(w, r)
			return
		}
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	})
}
