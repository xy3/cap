package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

// updateStatus is what the UI and CLI report about available updates.
type updateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Notes     string `json:"notes,omitempty"`
	AssetURL  string `json:"-"`
	AssetSize int64  `json:"asset_size,omitempty"`
	AssetName string `json:"asset_name,omitempty"`
}

// assetName is the per-platform release asset the updater downloads.
func assetName() string {
	if runtime.GOOS == "windows" {
		return "capper.exe"
	}
	return fmt.Sprintf("capper-%s-%s", runtime.GOOS, runtime.GOARCH)
}

type ghAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Body    string    `json:"body"`
	Assets  []ghAsset `json:"assets"`
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// checkForUpdate queries the latest GitHub release and compares it to the
// running version.
func checkForUpdate() (*updateStatus, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "capper-updater")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("contacting GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// No published release yet — treat as "nothing to update to".
		return &updateStatus{Current: Version, Available: false}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GitHub returned %s: %s", resp.Status, body)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("parsing release: %w", err)
	}

	st := &updateStatus{
		Current: Version,
		Latest:  rel.TagName,
		Notes:   rel.Body,
	}
	want := assetName()
	for _, a := range rel.Assets {
		if a.Name == want {
			st.AssetURL = a.URL
			st.AssetSize = a.Size
			st.AssetName = a.Name
			break
		}
	}
	// An update is offered when the tags differ and a matching asset exists.
	// "dev" builds always see the published release as an update.
	st.Available = rel.TagName != "" && rel.TagName != Version && st.AssetURL != ""
	return st, nil
}

// downloadAsset streams the asset, reporting (downloaded, total) bytes.
func downloadAsset(url string, total int64, progress func(done, total int64)) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "capper-updater")

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	if total == 0 {
		total = resp.ContentLength
	}

	buf := make([]byte, 0, total)
	tmp := make([]byte, 64*1024)
	var done int64
	for {
		n, rerr := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	if len(buf) == 0 {
		return nil, fmt.Errorf("downloaded an empty file")
	}
	return buf, nil
}

// replaceExecutable atomically swaps the running binary for new bytes using the
// rename-then-replace dance that works even while the exe is running on Windows.
// The old image is left as "<exe>.old" (Windows can't delete a running file);
// cleanupOldExecutable removes it on the next launch.
func replaceExecutable(data []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)

	tmp := filepath.Join(dir, ".capper-update.tmp")
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("writing update: %w", err)
	}

	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("moving current binary: %w", err)
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // roll back
		return fmt.Errorf("installing update: %w", err)
	}
	_ = os.Remove(old) // best effort; ignored while the old image is in use
	return nil
}

// cleanupOldExecutable removes a leftover "<exe>.old" from a prior update.
func cleanupOldExecutable() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	_ = os.Remove(exe + ".old")
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update capper to the latest release",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := checkForUpdate()
		if err != nil {
			return err
		}
		if !st.Available {
			fmt.Printf("capper %s is up to date (latest: %s)\n", st.Current, st.Latest)
			return nil
		}
		fmt.Printf("Updating %s -> %s ...\n", st.Current, st.Latest)
		data, err := downloadAsset(st.AssetURL, st.AssetSize, func(done, total int64) {
			if total > 0 {
				fmt.Printf("\r  downloading %3.0f%%", float64(done)/float64(total)*100)
			}
		})
		fmt.Println()
		if err != nil {
			return err
		}
		if err := replaceExecutable(data); err != nil {
			return err
		}
		fmt.Printf("Updated to %s. Restart capper to run the new version.\n", st.Latest)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(updateCmd)
}
