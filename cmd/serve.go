package cmd

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"capper/config"
	"capper/render"
	"capper/whisper"

	"github.com/spf13/cobra"
)

//go:embed ui
var uiFS embed.FS

var (
	servePort       int
	serveConfigPath string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Launch the styling UI in a browser",
	RunE:  runServe,
}

func init() {
	serveCmd.Flags().IntVar(&servePort, "port", 8080, "Port to listen on")
	serveCmd.Flags().StringVarP(&serveConfigPath, "config", "c", "my_config.json", "Default config file path")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", noCache(http.FileServer(http.FS(sub))))
	mux.HandleFunc("/api/config", handleConfig)
	mux.HandleFunc("/api/info", handleInfo)
	mux.HandleFunc("/api/transcribe", handleTranscribe)
	mux.HandleFunc("/api/preview", handlePreview)
	mux.HandleFunc("/api/generate", handleGenerate)
	mux.HandleFunc("/api/file", handleFile)

	addr := fmt.Sprintf(":%d", servePort)
	fmt.Printf("Capper UI listening on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

type generateReq struct {
	Config *config.Config `json:"config"`
	Input  string         `json:"input"`
	APIKey string         `json:"api_key"`
}

type previewReq struct {
	Config *config.Config `json:"config"`
	Input  string         `json:"input"`
	Time   float64        `json:"time"`
}

type transcribeReq struct {
	Config *config.Config `json:"config"`
	Input  string         `json:"input"`
	APIKey string         `json:"api_key"`
	Force  bool           `json:"force"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = serveConfigPath
	}

	switch r.Method {
	case http.MethodGet:
		var cfg *config.Config
		if _, err := os.Stat(path); err == nil {
			cfg, err = config.LoadConfig(path)
			if err != nil {
				writeErr(w, 500, err.Error())
				return
			}
		} else {
			cfg = config.DefaultConfig()
		}
		writeJSON(w, 200, map[string]any{"config": cfg, "path": path})

	case http.MethodPost:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		data, err := json.MarshalIndent(&cfg, "", "  ")
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		writeJSON(w, 200, map[string]string{"path": path})

	default:
		writeErr(w, 405, "method not allowed")
	}
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if input == "" {
		writeErr(w, 400, "missing input")
		return
	}
	if _, err := os.Stat(input); err != nil {
		writeErr(w, 404, "input not found")
		return
	}
	width, height, _ := render.DetectResolution(input)
	duration := probeDuration(input)

	hasCache := false
	if _, err := whisper.LoadCache(input); err == nil {
		hasCache = true
	}

	writeJSON(w, 200, map[string]any{
		"width":     width,
		"height":    height,
		"duration":  duration,
		"has_cache": hasCache,
	})
}

func probeDuration(input string) float64 {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nokey=1:noprint_wrappers=1",
		input,
	).Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return d
}

func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	var req transcribeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Config == nil || req.Input == "" {
		writeErr(w, 400, "config and input required")
		return
	}

	streamNDJSON(w, func(emit func(any)) {
		pe := &ProgressEmitter{
			OnStage:    func(s string) { emit(map[string]any{"type": "stage", "stage": s}) },
			OnProgress: func(s string, v float64) { emit(map[string]any{"type": "progress", "stage": s, "value": v}) },
		}
		result, err := transcribeOrCacheProgress(req.Config, req.Input, req.APIKey, req.Force, false, pe)
		if err != nil {
			emit(map[string]any{"type": "error", "error": err.Error()})
			return
		}
		emit(map[string]any{"type": "done", "words": len(result.Words), "duration": result.Duration})
	})
}

func streamNDJSON(w http.ResponseWriter, work func(emit func(any))) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)

	var mu sync.Mutex
	emit := func(ev any) {
		mu.Lock()
		defer mu.Unlock()
		data, _ := json.Marshal(ev)
		w.Write(data)
		w.Write([]byte("\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}
	work(emit)
}

var previewMu sync.Mutex

func handlePreview(w http.ResponseWriter, r *http.Request) {
	var req previewReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Config == nil || req.Input == "" {
		writeErr(w, 400, "config and input required")
		return
	}

	previewMu.Lock()
	defer previewMu.Unlock()

	resW, resH, _ := render.DetectResolution(req.Input)
	if resW > 0 && resH > 0 {
		req.Config.Resolution = fmt.Sprintf("%dx%d", resW, resH)
	}

	result, err := whisper.LoadCache(req.Input)
	if err != nil {
		writeErr(w, 400, "transcription cache missing; call /api/transcribe first")
		return
	}

	assContent := BuildASS(result.Words, req.Config)

	tmpAss, err := os.CreateTemp("", "capper-preview-*.ass")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer os.Remove(tmpAss.Name())
	tmpAss.WriteString(assContent)
	tmpAss.Close()

	tmpPng, err := os.CreateTemp("", "capper-preview-*.png")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	tmpPng.Close()
	defer os.Remove(tmpPng.Name())

	absAss, _ := filepath.Abs(tmpAss.Name())

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", req.Time),
		"-copyts",
		"-i", req.Input,
		"-vf", "ass=" + escapeAssPath(absAss),
		"-frames:v", "1",
		"-update", "1",
		"-loglevel", "error",
		tmpPng.Name(),
	}
	out, err := exec.Command("ffmpeg", args...).CombinedOutput()
	if err != nil {
		writeErr(w, 500, fmt.Sprintf("ffmpeg: %v: %s", err, out))
		return
	}

	data, err := os.ReadFile(tmpPng.Name())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

func escapeAssPath(p string) string {
	p = strings.ReplaceAll(p, `\`, `\\`)
	p = strings.ReplaceAll(p, `:`, `\:`)
	return p
}

func handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req generateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if req.Config == nil || req.Input == "" {
		writeErr(w, 400, "config and input required")
		return
	}
	if _, err := os.Stat(req.Input); err != nil {
		writeErr(w, 400, "input not found: "+req.Input)
		return
	}

	streamNDJSON(w, func(emit func(any)) {
		pe := &ProgressEmitter{
			OnStage:    func(s string) { emit(map[string]any{"type": "stage", "stage": s}) },
			OnProgress: func(s string, v float64) { emit(map[string]any{"type": "progress", "stage": s, "value": v}) },
		}
		output, err := RunPipelineProgress(req.Config, req.Input, req.APIKey, false, false, pe)
		if err != nil {
			emit(map[string]any{"type": "error", "error": err.Error()})
			return
		}
		abs, _ := filepath.Abs(output)
		emit(map[string]any{"type": "done", "output": abs})
	})
}

func handleFile(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeErr(w, 400, "missing path")
		return
	}
	http.ServeFile(w, r, path)
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		h.ServeHTTP(w, r)
	})
}
