//go:build windows

package cmd

import (
	"os/exec"
	"sort"
	"strings"
)

// listFonts returns installed font family names. It asks .NET's
// InstalledFontCollection via PowerShell, which yields clean family names
// (e.g. "Arial") that libass/ffmpeg can resolve. If PowerShell or the
// System.Drawing assembly is unavailable it returns an empty list, matching
// the Unix behaviour when fc-list is missing.
func listFonts() []string {
	const script = `Add-Type -AssemblyName System.Drawing; ` +
		`(New-Object System.Drawing.Text.InstalledFontCollection).Families | ForEach-Object { $_.Name }`
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return []string{}
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		seen[name] = true
	}
	return sortedKeys(seen)
}

func sortedKeys(m map[string]bool) []string {
	list := make([]string, 0, len(m))
	for k := range m {
		list = append(list, k)
	}
	sort.Strings(list)
	return list
}
