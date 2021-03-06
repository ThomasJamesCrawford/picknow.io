package middleware

import (
	"log"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/twinj/uuid"
)

func UserId() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		userID := session.Get("user_id")

		session.Options(sessions.Options{
			SameSite: http.SameSiteStrictMode,
			HttpOnly: true,
			MaxAge:   31536000, // 1 year
			Path:     "/",
		})

		if userID == nil {
			session.Set("user_id", uuid.NewV4().String())
		} else {
			// Keep refreshing the cookie
			session.Set("user_id", userID)
		}

		session.Save()

		log.Default().Printf("UserID %s accessing %s", userID, c.Request.URL)
	}
}
