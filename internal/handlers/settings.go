package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"cloud-platform/internal/response"
	"cloud-platform/internal/services"

	"github.com/gin-gonic/gin"
)

// GetRuntimeSettings 返回可运行时配置的当前值与定义，供管理端渲染表单。
func GetRuntimeSettings(c *gin.Context) {
	store := services.GetSettings()
	if store == nil {
		response.ServiceUnavailable(c, "Settings store is not initialized")
		return
	}

	response.Success(c, "Settings retrieved successfully", gin.H{
		"values": store.All(),
		"defs":   services.SettingDefs,
	})
}

// UpdateRuntimeSettings 批量更新运行时配置（仅接受白名单内的键）。
func UpdateRuntimeSettings(c *gin.Context) {
	store := services.GetSettings()
	if store == nil {
		response.ServiceUnavailable(c, "Settings store is not initialized")
		return
	}

	var req struct {
		Values map[string]string `json:"values"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err.Error())
		return
	}
	if len(req.Values) == 0 {
		response.BadRequest(c, "No settings provided", nil)
		return
	}

	normalized := make(map[string]string, len(req.Values))
	for key, value := range req.Values {
		def, ok := services.SettingDefByKey(key)
		if !ok {
			// 客户端可能回传整份配置，其中夹带已下线的历史键：
			// 忽略即可，不因此让整次保存失败。
			continue
		}

		clean, err := normalizeSettingValue(def, value)
		if err != nil {
			response.BadRequest(c, err.Error(), nil)
			return
		}
		normalized[key] = clean
	}

	if err := store.Update(normalized); err != nil {
		response.InternalError(c, err.Error())
		return
	}

	response.Success(c, "Settings updated successfully", gin.H{"values": store.All()})
}

// normalizeSettingValue 按定义校验并规范化取值。
func normalizeSettingValue(def services.SettingDef, raw string) (string, error) {
	value := strings.TrimSpace(raw)

	switch def.Type {
	case "int":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return "", fmt.Errorf("%s must be an integer, got %q", def.Key, raw)
		}
		if def.Min != 0 || def.Max != 0 {
			if parsed < def.Min || parsed > def.Max {
				return "", fmt.Errorf("%s must be within %d..%d, got %d", def.Key, def.Min, def.Max, parsed)
			}
		}
		return strconv.Itoa(parsed), nil

	case "bool":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return "", fmt.Errorf("%s must be a boolean, got %q", def.Key, raw)
		}
		return strconv.FormatBool(parsed), nil

	default: // string
		return value, nil
	}
}
