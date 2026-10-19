package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/RyeT7/Minotaur-BE/internal/inventory"
)

const maxBodyBytes = 64 << 10

type Options struct {
	AllowedOrigins []string
}

func NewRouter(store *inventory.Store, opts Options) (*gin.Engine, error) {
	binding.EnableDecoderDisallowUnknownFields = true

	r := gin.New()
	r.HandleMethodNotAllowed = true
	if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}

	r.Use(gin.Logger(), gin.Recovery(), securityHeaders(), cors(opts.AllowedOrigins), limitBody(maxBodyBytes))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	h := &handlers{store: store}
	v1 := r.Group("/api/v1")
	{
		v1.GET("/items", h.listItems)
		v1.POST("/items", h.createItem)
		v1.GET("/items/:sku/:warehouse", h.getItem)

		v1.GET("/movements", h.listMovements)
		v1.POST("/movements", h.recordMovement)
		v1.GET("/movements/daily", h.dailyMovements)

		v1.GET("/activity", h.listActivity)
	}

	return r, nil
}
