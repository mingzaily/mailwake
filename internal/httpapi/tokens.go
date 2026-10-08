package httpapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/storage"
)

func tokenRoutes(api *gin.RouterGroup, service *auth.Service) {
	api.POST("/tokens", func(c *gin.Context) {
		var input struct {
			Name string `json:"name"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		token, err := service.CreateToken(c.Request.Context(), input.Name)
		if err != nil {
			respondFault(c, err)
			return
		}
		view := publicToken(token.Token)
		view.Value = token.Value
		c.JSON(201, view)
	})
	api.GET("/tokens", func(c *gin.Context) {
		tokens, err := service.Tokens(c.Request.Context())
		if err != nil {
			respondFault(c, err)
			return
		}
		views := make([]tokenView, 0, len(tokens))
		for _, token := range tokens {
			views = append(views, publicToken(token))
		}
		c.JSON(200, gin.H{"tokens": views})
	})
	api.DELETE("/tokens/:id", func(c *gin.Context) {
		if err := service.RevokeToken(c.Request.Context(), c.Param("id")); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
}

type tokenView struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Value      string     `json:"token,omitempty"`
}

func publicToken(token storage.Token) tokenView {
	return tokenView{ID: token.ID, Name: token.Name, CreatedAt: time.Unix(token.CreatedAt, 0).UTC(), LastUsedAt: optionalTime(token.LastUsedAt, func(seconds int64) time.Time { return time.Unix(seconds, 0) })}
}
