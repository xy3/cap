//go:build !windows

package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// pickFile shows a native Open/Save dialog and returns the chosen path, or ""
// if the user cancelled. Uses osascript on macOS and zenity (falling back to
// kdialog) on Linux/BSD.
func pickFile(mode, title, def string) (string, error) {
	if runtime.GOOS == "darwin" {
		return pickFileMac(mode, title, def)
	}
	return pickFileZenity(mode, title, def)
}

func pickFileZenity(mode, title, def string) (string, error) {
	zenity, err := exec.LookPath("zenity")
	if err != nil {
		return pickFileKDialog(mode, title, def)
	}
	args := []string{"--file-selection", "--title=" + title}
	if mode == "save" {
		args = append(args, "--save", "--confirm-overwrite")
	} else {
		args = append(args,
			"--file-filter=Videos | *.mp4 *.mov *.mkv *.webm *.avi *.m4v",
			"--file-filter=All files | *")
	}
	if def != "" {
		args = append(args, "--filename="+def)
	}
	out, err := exec.Command(zenity, args...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil // user cancelled
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pickFileKDialog(mode, title, def string) (string, error) {
	kdialog, err := exec.LookPath("kdialog")
	if err != nil {
		return "", fmt.Errorf("no file dialog found (install zenity or kdialog)")
	}
	flag := "--getopenfilename"
	if mode == "save" {
		flag = "--getsavefilename"
	}
	start := def
	if start == "" {
		start = "."
	}
	out, err := exec.Command(kdialog, "--title", title, flag, start).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func pickFileMac(mode, title, def string) (string, error) {
	var script string
	if mode == "save" {
		name := filepath.Base(def)
		script = fmt.Sprintf(`POSIX path of (choose file name with prompt %q default name %q)`, title, name)
	} else {
		script = fmt.Sprintf(`POSIX path of (choose file with prompt %q)`, title)
	}
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		// User cancel yields exit status 1 with "User canceled." on stderr.
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// revealFile opens the system file manager with the file's folder shown.
func revealFile(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Run()
	}
	dir := filepath.Dir(path)
	if xdg, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command(xdg, dir).Run()
	}
	return fmt.Errorf("no file manager found (install xdg-utils)")
}
