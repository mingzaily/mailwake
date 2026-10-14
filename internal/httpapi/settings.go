package httpapi

import (
	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/settings"
)

type deliveryHandlers struct{ runtime DeliveryRuntime }

func (h deliveryHandlers) Get(c *gin.Context) {
	c.JSON(200, h.runtime.DeliveryView())
}

func (h deliveryHandlers) Update(c *gin.Context) {
	var body settings.DeliveryUpdate
	if !decodeRequest(c, &body) {
		return
	}
	if body.Revision == nil || *body.Revision < 0 {
		respondError(c, 400, fault.New("request_invalid"))
		return
	}
	if err := h.runtime.UpdateDelivery(c.Request.Context(), body); err != nil {
		respondFault(c, err)
		return
	}
	c.JSON(200, h.runtime.DeliveryView())
}

func (h deliveryHandlers) Test(c *gin.Context) {
	var input settings.DeliveryUpdate
	if !decodeRequest(c, &input) {
		return
	}
	if err := h.runtime.TestDelivery(c.Request.Context(), input); err != nil {
		respondFault(c, err)
		return
	}
	c.Status(204)
}

func (h deliveryHandlers) Delete(c *gin.Context) {
	var input struct {
		Revision *int64 `json:"revision"`
	}
	if !decodeRequest(c, &input) {
		return
	}
	if input.Revision == nil || *input.Revision < 0 {
		respondError(c, 400, fault.New("request_invalid"))
		return
	}
	if err := h.runtime.DeleteDelivery(c.Request.Context(), *input.Revision); err != nil {
		respondFault(c, err)
		return
	}
	c.Status(204)
}
