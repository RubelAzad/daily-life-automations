package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/services"
)

type AuthHandler struct {
	Auth          services.AuthService
	SecureCookies bool
}

type registerRequest struct {
	Name     string `json:"name" binding:"required,max=120"`
	Email    string `json:"email" binding:"required,email,max=255"`
	Password string `json:"password" binding:"required,min=8,max=72"`
	Timezone string `json:"timezone" binding:"max=64"` // optional, from the browser
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Register creates the account and signs the user in.
func (h AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}
	_, err := h.Auth.Register(c.Request.Context(), req.Name, req.Email, req.Password, services.RegisterOptions{Timezone: req.Timezone})
	switch {
	case errors.Is(err, services.ErrEmailTaken):
		respondError(c, http.StatusConflict, err.Error())
		return
	case errors.Is(err, services.ErrPasswordTooLong):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": gin.H{"password": "must be at most 72 bytes"}})
		return
	case err != nil:
		respondInternal(c, err)
		return
	}
	h.startSession(c, req.Email, req.Password, http.StatusCreated)
}

func (h AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	h.startSession(c, req.Email, req.Password, http.StatusOK)
}

func (h AuthHandler) Logout(c *gin.Context) {
	if token, err := c.Cookie(middleware.SessionCookie); err == nil && token != "" {
		if err := h.Auth.Logout(c.Request.Context(), token); err != nil {
			respondInternal(c, err)
			return
		}
	}
	h.setCookie(c, "", time.Unix(0, 0), -1)
	c.Status(http.StatusNoContent)
}

func (h AuthHandler) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": middleware.CurrentUser(c)})
}

func (h AuthHandler) startSession(c *gin.Context, email, password string, status int) {
	user, session, err := h.Auth.Login(c.Request.Context(), email, password, c.Request.UserAgent(), c.ClientIP())
	if errors.Is(err, services.ErrInvalidCredentials) {
		respondError(c, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		respondInternal(c, err)
		return
	}
	h.setCookie(c, session.Token, session.ExpiresAt, int(time.Until(session.ExpiresAt).Seconds()))
	c.JSON(status, gin.H{"user": user})
}

func (h AuthHandler) setCookie(c *gin.Context, value string, expires time.Time, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     middleware.SessionCookie,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
