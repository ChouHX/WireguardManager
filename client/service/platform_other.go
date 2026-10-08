//go:build !windows

package service

// The portable configuration/lifecycle packages are testable without GTK or Windows.
// The actual tunnel and DPAPI implementation is Windows-only.
