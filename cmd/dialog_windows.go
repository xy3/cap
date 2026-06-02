//go:build windows

package cmd

import (
	"os/exec"
	"path/filepath"
	"strings"
)

const videoFilter = "Video files|*.mp4;*.mov;*.mkv;*.webm;*.avi;*.m4v|All files|*.*"

// psEscape escapes a string for a PowerShell single-quoted literal.
func psEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

// pickFile shows a native Open/Save dialog and returns the chosen path, or ""
// if the user cancelled.
func pickFile(mode, title, def string) (string, error) {
	var b strings.Builder
	b.WriteString("Add-Type -AssemblyName System.Windows.Forms;")
	// A top-most owner window forces the dialog to the foreground; without it
	// PowerShell's file dialog opens behind the current window and looks hung.
	b.WriteString("$owner = New-Object System.Windows.Forms.Form;")
	b.WriteString("$owner.TopMost = $true;$owner.ShowInTaskbar = $false;")
	if mode == "save" {
		b.WriteString("$d = New-Object System.Windows.Forms.SaveFileDialog;")
		b.WriteString("$d.DefaultExt = 'mp4';$d.OverwritePrompt = $true;")
		if def != "" {
			b.WriteString("$d.FileName = '" + psEscape(filepath.Base(def)) + "';")
			if dir := filepath.Dir(def); dir != "" && dir != "." {
				b.WriteString("$d.InitialDirectory = '" + psEscape(dir) + "';")
			}
		}
	} else {
		b.WriteString("$d = New-Object System.Windows.Forms.OpenFileDialog;")
		if def != "" {
			if dir := filepath.Dir(def); dir != "" && dir != "." {
				b.WriteString("$d.InitialDirectory = '" + psEscape(dir) + "';")
			}
		}
	}
	b.WriteString("$d.Filter = '" + psEscape(videoFilter) + "';")
	b.WriteString("$d.Title = '" + psEscape(title) + "';")
	b.WriteString("$res = $d.ShowDialog($owner);$owner.Dispose();")
	b.WriteString("if ($res -eq 'OK') { [Console]::Out.Write($d.FileName) }")

	// -STA is required for the WinForms dialogs.
	out, err := exec.Command("powershell", "-STA", "-NoProfile", "-NonInteractive", "-Command", b.String()).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// revealFile opens the system file explorer with the file selected.
func revealFile(path string) error {
	// explorer returns a non-zero exit code even on success, so ignore the error.
	_ = exec.Command("explorer", "/select,", filepath.Clean(path)).Run()
	return nil
}
