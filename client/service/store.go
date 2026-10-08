package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}
type Store struct {
	Path   string
	Cipher Protector
}

type diskProfiles struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

func (s Store) Load() ([]Profile, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return []Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 8*1024*1024 {
		return nil, errors.New("本地配置存储过大")
	}
	plain, err := s.Cipher.Unprotect(raw)
	if err != nil {
		return nil, errors.New("无法解密本地配置，请使用原 Windows 账户")
	}
	defer clear(plain)
	var data diskProfiles
	if err = json.Unmarshal(plain, &data); err != nil || data.Version != 1 {
		return nil, errors.New("本地配置版本或格式无效")
	}
	return data.Profiles, nil
}

func (s Store) Save(profiles []Profile) error {
	plain, err := json.Marshal(diskProfiles{1, profiles})
	if err != nil {
		return err
	}
	defer clear(plain)
	encrypted, err := s.Cipher.Protect(plain)
	if err != nil {
		return errors.New("无法加密本地配置")
	}
	return AtomicWrite(s.Path, encrypted)
}

func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".wgm-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
