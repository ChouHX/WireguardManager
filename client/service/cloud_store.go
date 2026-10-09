package service

import (
	"encoding/json"
	"errors"
	"os"
	"wireguardmanager/client/cloud"
)

type CloudLogin struct {
	ServerURL string     `json:"serverURL"`
	Token     string     `json:"token"`
	User      cloud.User `json:"user"`
}
type CloudData struct {
	Version    int               `json:"version"`
	AccessKeys map[string]string `json:"accessKeys,omitempty"`
	Login      *CloudLogin       `json:"login,omitempty"`
	Targets    map[string]string `json:"targets"`
}
type CloudStore struct {
	Path   string
	Cipher Protector
}

func (s CloudStore) Load() (CloudData, error) {
	data := CloudData{Version: 1, Targets: map[string]string{}, AccessKeys: map[string]string{}}
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return data, nil
	}
	if err != nil {
		return data, err
	}
	if len(raw) > 2*1024*1024 {
		return data, errors.New("本地登录数据过大")
	}
	plain, err := s.Cipher.Unprotect(raw)
	if err != nil {
		return data, errors.New("无法解密本地登录，请使用原 Windows 用户")
	}
	defer clear(plain)
	if err = json.Unmarshal(plain, &data); err != nil || data.Version != 1 {
		return data, errors.New("本地登录数据格式无效")
	}
	if data.AccessKeys == nil {
		data.AccessKeys = map[string]string{}
	}
	if data.Targets == nil {
		data.Targets = map[string]string{}
	}
	return data, nil
}
func (s CloudStore) Save(data CloudData) error {
	data.Version = 1
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	defer clear(raw)
	encrypted, err := s.Cipher.Protect(raw)
	if err != nil {
		return err
	}
	return AtomicWrite(s.Path, encrypted)
}
