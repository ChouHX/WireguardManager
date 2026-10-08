package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Include complete dependency notices, not just links, in distributed archives.
func collectLicenses(out string) ([]string, error) {
	listing, err := exec.Command("go", "list", "-m", "-json", "all").Output()
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(listing))
	var names []string
	copyNotice := func(source, name string) error {
		raw, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		dest := filepath.Join("licenses", name)
		if err = os.MkdirAll(filepath.Join(out, filepath.Dir(dest)), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(out, dest), raw, 0644); err != nil {
			return err
		}
		names = append(names, filepath.ToSlash(dest))
		return nil
	}
	for {
		var module struct {
			Path, Version, Dir string
			Main               bool
		}
		err := decoder.Decode(&module)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if module.Main || module.Dir == "" {
			continue
		}
		entries, err := os.ReadDir(module.Dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			upper := strings.ToUpper(entry.Name())
			if entry.IsDir() || (!strings.HasPrefix(upper, "LICENSE") && !strings.HasPrefix(upper, "LICENCE") && !strings.HasPrefix(upper, "COPYING") && !strings.HasPrefix(upper, "NOTICE")) {
				continue
			}
			name := strings.ReplaceAll(module.Path, "/", "_") + "@" + module.Version + "_" + entry.Name()
			if err = copyNotice(filepath.Join(module.Dir, entry.Name()), name); err != nil {
				return nil, err
			}
		}
	}
	for _, pkg := range []string{"react", "react-dom", "scheduler"} {
		if err = copyNotice(filepath.Join("frontend", "node_modules", pkg, "LICENSE"), pkg+"-LICENSE"); err != nil {
			return nil, err
		}
	}
	if err = copyNotice(filepath.Join(runtime.GOROOT(), "LICENSE"), "Go-LICENSE"); err != nil {
		return nil, err
	}
	if _, err = os.Stat("../LICENSE"); err == nil {
		if err = copyNotice("../LICENSE", "Application-LICENSE"); err != nil {
			return nil, err
		}
	}
	sort.Strings(names)
	return names, nil
}
