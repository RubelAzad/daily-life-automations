package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/services"
)

const (
	SessionCookie = "lifeos_session"
	userKey       = "auth.user"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (models.User, error)
}

// RequireAuth rejects requests without a valid session cookie and stores the
// signed-in user on the context for CurrentUser.
func RequireAuth(auth Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(SessionCookie)
		if err != nil || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": services.ErrUnauthenticated.Error()})
			return
		}
		user, err := auth.Authenticate(c.Request.Context(), token)
		if errors.Is(err, services.ErrUnauthenticated) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
			return
		}
		c.Set(userKey, user)
		c.Next()
	}
}

// CurrentUser must only be called on routes behind RequireAuth.
func CurrentUser(c *gin.Context) models.User {
	return c.MustGet(userKey).(models.User)
}
