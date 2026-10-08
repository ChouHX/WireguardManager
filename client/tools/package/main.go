// Package builds the desktop app without requiring CGO, a C cross compiler, or a global Wails CLI.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/tc-hib/winres"
)

const driverURL = "https://download.wireguard.com/wireguard-nt/wireguard-nt-1.1.zip"
const driverSHA = "dceb30a9bc4be48cce0f74160fc88a585a2c2627366e8f846fc6658f9038dace"

func main() {
	archive := flag.String("driver-zip", "", "optional cached official wireguard-nt-1.1.zip")
	flag.Parse()
	if err := build(*archive); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func build(archive string) error {
	if _, err := os.Stat("wails.json"); err != nil {
		return errors.New("run from client/: go run ./tools/package")
	}
	if _, err := os.Stat("frontend/dist/index.html"); err != nil {
		return errors.New("build UI first: npm --prefix frontend ci && npm --prefix frontend run build")
	}
	var data []byte
	var err error
	if archive != "" {
		data, err = os.ReadFile(archive)
	} else {
		fmt.Println("Downloading pinned official WireGuardNT 1.1")
		client := http.Client{Timeout: 90 * time.Second}
		res, e := client.Get(driverURL)
		if e != nil {
			return e
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return fmt.Errorf("driver download: HTTP %d", res.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(res.Body, 16*1024*1024))
	}
	if err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != driverSHA {
		return errors.New("WireGuardNT SHA-256 mismatch; refusing to build")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	wanted := map[string]string{"wireguard-nt/bin/amd64/wireguard.dll": "driver", "wireguard-nt/LICENSE.txt": "license"}
	files := map[string][]byte{}
	for _, f := range reader.File {
		if key, ok := wanted[f.Name]; ok {
			r, e := f.Open()
			if e != nil {
				return e
			}
			files[key], err = io.ReadAll(io.LimitReader(r, 16*1024*1024))
			r.Close()
			if err != nil {
				return err
			}
		}
	}
	if len(files["driver"]) == 0 || len(files["license"]) == 0 {
		return errors.New("driver archive incomplete")
	}
	manifest, err := os.ReadFile("build/windows/app.manifest")
	if err != nil {
		return err
	}
	rs := winres.ResourceSet{}
	if err = rs.Set(winres.RT_RCDATA, winres.Name("wireguard.dll"), winres.LCIDNeutral, files["driver"]); err != nil {
		return err
	}
	if err = rs.Set(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault, manifest); err != nil {
		return err
	}
	resource, err := os.Create("wgm_windows_amd64.syso")
	if err != nil {
		return err
	}
	err = rs.WriteObject(resource, winres.ArchAMD64)
	closeErr := resource.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	out := filepath.Join("build", "bin")
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	exe := filepath.Join(out, "WireguardManagerDesktop.exe")
	fmt.Println("Building Windows amd64 with embedded WireGuardNT and administrator manifest")
	cmd := exec.Command("go", "build", "-trimpath", "-tags", "desktop,production,load_wgnt_from_rsrc", "-ldflags", "-s -w -H=windowsgui", "-o", exe, ".")
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return err
	}
	if err = verify(exe, manifest, files["driver"]); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "WireGuardNT-LICENSE.txt"), files["license"], 0644); err != nil {
		return err
	}
	readme, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "README.md"), readme, 0644); err != nil {
		return err
	}
	notices, err := os.ReadFile("THIRD_PARTY_NOTICES.md")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "THIRD_PARTY_NOTICES.md"), notices, 0644); err != nil {
		return err
	}
	archivePath := filepath.Join(out, "WireguardManagerDesktop-windows-amd64.zip")
	names := []string{"WireguardManagerDesktop.exe", "WireGuardNT-LICENSE.txt", "README.md", "THIRD_PARTY_NOTICES.md"}
	licenses, err := collectLicenses(out)
	if err != nil {
		return err
	}
	names = append(names, licenses...)
	if err = zipFiles(archivePath, out, names); err != nil {
		return err
	}

	zipped, err := os.ReadFile(archivePath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(zipped)
	if err = os.WriteFile(filepath.Join(out, "SHA256SUMS"), []byte(fmt.Sprintf("%x  %s\n", sum, filepath.Base(archivePath))), 0644); err != nil {
		return err
	}
	fmt.Println("Created", archivePath)
	return nil
}
func verify(exe string, manifest, driver []byte) error {
	f, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()
	rs, err := winres.LoadFromEXE(f)
	if err != nil {
		return err
	}
	if !bytes.Equal(rs.Get(winres.RT_RCDATA, winres.Name("wireguard.dll"), winres.LCIDNeutral), driver) {
		return errors.New("embedded driver verification failed")
	}
	if !bytes.Equal(rs.Get(winres.RT_MANIFEST, winres.ID(1), winres.LCIDDefault), manifest) {
		return errors.New("administrator manifest verification failed")
	}
	return nil
}
func zipFiles(path, dir string, names []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	z := zip.NewWriter(f)
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			z.Close()
			return err
		}
		entry, err := z.Create(name)
		if err != nil {
			z.Close()
			return err
		}
		if _, err = entry.Write(raw); err != nil {
			z.Close()
			return err
		}
	}
	return z.Close()
}
