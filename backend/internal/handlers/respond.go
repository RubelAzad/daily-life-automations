package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
)

// RegisterValidation makes validation errors report JSON field names
// ("email") instead of Go struct field names ("Email").
func RegisterValidation() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
	}
}

// bindJSON decodes and validates the body. On failure it writes a 400/422
// response and returns false.
func bindJSON(c *gin.Context, dst any) bool {
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}
	var ve validator.ValidationErrors
	if errors.As(err, &ve) {
		fields := make(map[string]string, len(ve))
		for _, fe := range ve {
			fields[fe.Field()] = validationMessage(fe)
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": fields})
		return false
	}
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) && te.Field != "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": gin.H{te.Field: "has the wrong type"}})
		return false
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
	return false
}

// pathID parses the :id route parameter, writing a 404 if it is not a valid ID.
func pathID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		respondError(c, http.StatusNotFound, "not found")
		return 0, false
	}
	return id, true
}

// respondServiceError maps service and repository errors to HTTP responses.
// Records owned by other users are reported as 404, never 403, so their
// existence is not revealed.
func respondServiceError(c *gin.Context, err error) {
	var ve *services.ValidationError
	switch {
	case errors.As(err, &ve):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "validation failed", "fields": ve.Fields})
	case errors.Is(err, repository.ErrNotFound):
		respondError(c, http.StatusNotFound, "not found")
	default:
		respondInternal(c, err)
	}
}

func validationMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	case "oneof":
		return "must be one of: " + fe.Param()
	default:
		return "is invalid"
	}
}

func respondError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func respondInternal(c *gin.Context, err error) {
	_ = c.Error(err)
	respondError(c, http.StatusInternalServerError, "internal server error")
}
