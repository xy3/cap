//go:build !windows

package cmd

import (
	"os/exec"
	"sort"
	"strings"
)

// listFonts returns installed font family names via fontconfig (fc-list).
func listFonts() []string {
	out, err := exec.Command("fc-list", ":", "family").Output()
	if err != nil {
		return []string{}
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		name := strings.TrimSpace(strings.SplitN(line, ",", 2)[0])
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
