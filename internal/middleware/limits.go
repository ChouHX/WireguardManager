package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodySizeLimit 限制请求体大小。
//
// 所有接口接收的都是 JSON（没有文件上传），因此上限可以取得很小。
// 不加限制时，一个超大请求体就会让进程按声明长度分配内存。
func BodySizeLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
