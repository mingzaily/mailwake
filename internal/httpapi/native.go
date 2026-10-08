package httpapi

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/delivery"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/native"
)

func nativeRoutes(api *gin.RouterGroup, runtime Runtime) {
	var service *native.Service
	if provider, ok := runtime.(interface{ Native() *native.Service }); ok {
		service = provider.Native()
	}
	group := api.Group("/native", func(c *gin.Context) {
		if !service.Available() {
			respondError(c, 503, fault.New("native_push_unavailable"))
			return
		}
		c.Next()
	})
	group.POST("/pairings", func(c *gin.Context) {
		result, err := service.Create(c.Request.Context())
		if err != nil {
			nativeError(c, err)
			return
		}
		c.JSON(201, result)
	})
	group.GET("/pairings/:id", func(c *gin.Context) {
		result, err := service.Get(c.Request.Context(), c.Param("id"))
		if err != nil {
			nativeError(c, err)
			return
		}
		c.JSON(200, result)
	})
	group.GET("/devices", func(c *gin.Context) {
		items, err := service.Devices(c.Request.Context())
		if err != nil {
			nativeError(c, err)
			return
		}
		c.JSON(200, gin.H{"devices": items})
	})
	group.DELETE("/devices/:id", func(c *gin.Context) {
		if err := service.Unlink(c.Request.Context(), c.Param("id")); err != nil {
			nativeError(c, err)
			return
		}
		c.Status(204)
	})
}
func nativeError(c *gin.Context, err error) {
	var failure *delivery.Failure
	if errors.As(err, &failure) {
		respondError(c, 503, &fault.Error{Code: failure.Code, Params: failure.Params})
		return
	}
	f := fault.From(err, "database_unavailable")
	switch f.Code {
	case "native_pairing_not_found":
		respondError(c, 404, f)
	case "native_pairing_limit", "native_pairing_conflict", "clock_skew":
		respondError(c, 409, f)
	default:
		respondFault(c, err)
	}
}
