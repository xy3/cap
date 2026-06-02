package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// modelInfo describes a downloadable whisper.cpp ggml model.
type modelInfo struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SizeMB int    `json:"size_mb"`
}

// modelCatalog is the set of models offered in the UI. Files come from the
// official whisper.cpp model repo on HuggingFace.
var modelCatalog = []modelInfo{
	{"tiny", "ggml-tiny.bin", 75},
	{"tiny (English)", "ggml-tiny.en.bin", 75},
	{"base", "ggml-base.bin", 142},
	{"base (English)", "ggml-base.en.bin", 142},
	{"small", "ggml-small.bin", 466},
	{"small (English)", "ggml-small.en.bin", 466},
	{"medium", "ggml-medium.bin", 1500},
	{"medium (English)", "ggml-medium.en.bin", 1500},
	{"large v3 turbo", "ggml-large-v3-turbo.bin", 1600},
	{"large v3", "ggml-large-v3.bin", 3100},
}

const modelBaseURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/"

// modelsDir is where models live and are downloaded to: next to the executable,
// matching where the bundle ships ggml-base.bin.
func modelsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// resolveModelPath turns a relative model filename into an absolute path next to
// the executable when the file exists there, so transcription works regardless
// of the process working directory. Absolute paths pass through unchanged.
func resolveModelPath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	cand := filepath.Join(modelsDir(), p)
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	return p
}

func catalogEntry(file string) *modelInfo {
	for i := range modelCatalog {
		if modelCatalog[i].File == file {
			return &modelCatalog[i]
		}
	}
	return nil
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	dir := modelsDir()
	type item struct {
		modelInfo
		Downloaded bool `json:"downloaded"`
	}
	items := make([]item, 0, len(modelCatalog))
	for _, m := range modelCatalog {
		_, err := os.Stat(filepath.Join(dir, m.File))
		items = append(items, item{m, err == nil})
	}
	writeJSON(w, 200, map[string]any{"models": items, "dir": dir})
}

// handleModelDownload streams a model from HuggingFace to the models directory,
// reporting progress as NDJSON.
func handleModelDownload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		File string `json:"file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	info := catalogEntry(req.File)
	if info == nil {
		writeErr(w, 400, "unknown model: "+req.File)
		return
	}
	dest := filepath.Join(modelsDir(), info.File)

	streamNDJSON(w, func(emit func(any)) {
		if _, err := os.Stat(dest); err == nil {
			emit(map[string]any{"type": "done", "file": info.File, "already": true})
			return
		}
		emit(map[string]any{"type": "stage", "stage": "downloading"})
		var lastPct int
		err := downloadToFile(modelBaseURL+info.File, dest, func(done, total int64) {
			if total <= 0 {
				return
			}
			if pct := int(float64(done) / float64(total) * 100); pct != lastPct {
				lastPct = pct
				emit(map[string]any{"type": "progress", "stage": "downloading", "value": float64(done) / float64(total)})
			}
		})
		if err != nil {
			emit(map[string]any{"type": "error", "error": err.Error()})
			return
		}
		emit(map[string]any{"type": "done", "file": info.File})
	})
}

// downloadToFile streams a URL to dest (via a .part temp file), reporting
// (downloaded, total) bytes. Suitable for multi-GB models — it never buffers the
// whole file in memory.
func downloadToFile(url, dest string, progress func(done, total int64)) error {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "capper")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	total := resp.ContentLength
	buf := make([]byte, 256*1024)
	var done int64
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				os.Remove(tmp)
				return werr
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(tmp)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if progress != nil {
		progress(done, total)
	}
	return os.Rename(tmp, dest)
}
