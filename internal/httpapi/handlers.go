package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/RyeT7/Minotaur-BE/internal/inventory"
)

const (
	defaultLimit = 50
	maxLimit     = 200
	defaultDays  = 14
)

type handlers struct {
	store *inventory.Store
}

type errorBody struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}

func (h *handlers) listItems(c *gin.Context) {
	c.JSON(http.StatusOK, h.store.ListItems(c.Query("warehouse")))
}

func (h *handlers) getItem(c *gin.Context) {
	it, err := h.store.GetItem(c.Param("sku"), c.Param("warehouse"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, it)
}

func (h *handlers) createItem(c *gin.Context) {
	var in inventory.Item
	if err := c.ShouldBindJSON(&in); err != nil {
		writeBindError(c, err)
		return
	}
	it, err := h.store.CreateItem(in)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, it)
}

func (h *handlers) listMovements(c *gin.Context) {
	limit, ok := queryInt(c, "limit", defaultLimit, 1, maxLimit)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.store.ListMovements(inventory.MovementFilter{
		SKU:       c.Query("sku"),
		Warehouse: c.Query("warehouse"),
		Limit:     limit,
	}))
}

func (h *handlers) recordMovement(c *gin.Context) {
	var in inventory.MovementInput
	if err := c.ShouldBindJSON(&in); err != nil {
		writeBindError(c, err)
		return
	}
	m, err := h.store.RecordMovement(in)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, m)
}

func (h *handlers) dailyMovements(c *gin.Context) {
	days, ok := queryInt(c, "days", defaultDays, 1, 366)
	if !ok {
		return
	}
	out, err := h.store.DailyMovements(days)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *handlers) listActivity(c *gin.Context) {
	limit, ok := queryInt(c, "limit", defaultLimit, 1, maxLimit)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, h.store.ListActivity(limit))
}

func queryInt(c *gin.Context, name string, def, lo, hi int) (int, bool) {
	raw, present := c.GetQuery(name)
	if !present {
		return def, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < lo || n > hi {
		c.JSON(http.StatusBadRequest, errorBody{
			Error: "must be an integer between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi),
			Field: name,
		})
		return 0, false
	}
	return n, true
}

func writeBindError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, errorBody{Error: "request body too large"})
		return
	}
	c.JSON(http.StatusBadRequest, errorBody{Error: "invalid JSON body"})
}

func writeError(c *gin.Context, err error) {
	var ve *inventory.ValidationError
	switch {
	case errors.As(err, &ve):
		c.JSON(http.StatusBadRequest, errorBody{Error: ve.Message, Field: ve.Field})
	case errors.Is(err, inventory.ErrNotFound):
		c.JSON(http.StatusNotFound, errorBody{Error: "item not found"})
	case errors.Is(err, inventory.ErrConflict):
		c.JSON(http.StatusConflict, errorBody{Error: "item already exists in this warehouse"})
	case errors.Is(err, inventory.ErrInsufficientStock):
		c.JSON(http.StatusConflict, errorBody{Error: "insufficient stock"})
	default:
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, errorBody{Error: "internal error"})
	}
}
