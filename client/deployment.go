package main

import (
	"encoding/base64"
	"errors"
	"wireguardmanager/client/cloud"
)

// Set by the packaging tool. Base64 keeps the URL out of linker flag parsing.
var serverURLBase64 string

func deploymentServerURL() (string, error) {
	raw, err := base64.StdEncoding.DecodeString(serverURLBase64)
	if err != nil || len(raw) == 0 {
		return "", errors.New("客户端未绑定服务端，请构建时指定 -server-url")
	}
	return cloud.NormalizeURL(string(raw))
}
