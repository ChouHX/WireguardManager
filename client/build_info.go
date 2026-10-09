package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionText string

// Set by the package tool, independently of the Go working tree stamp.
var buildCommit = "dev"

type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func applicationBuildInfo() BuildInfo {
	return BuildInfo{Version: strings.TrimSpace(versionText), Commit: buildCommit}
}
