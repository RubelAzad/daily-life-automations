package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// CORS allows a single explicit origin (never a wildcard) so it stays safe
// to use with credentialed requests later.
func CORS(allowedOrigin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("Origin") == allowedOrigin {
			h := c.Writer.Header()
			h.Set("Access-Control-Allow-Origin", allowedOrigin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Add("Vary", "Origin")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
