package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// GetOTLPEnabled returns the current OTLP telemetry status
func (h *Handler) GetOTLPEnabled(c *gin.Context) {
	enabled := usage.OTLPEnabled()
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled,
	})
}

// SetOTLPEnabled sets the OTLP telemetry status
func (h *Handler) SetOTLPEnabled(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	usage.SetOTLPEnabled(req.Enabled)
	c.JSON(http.StatusOK, gin.H{
		"enabled": req.Enabled,
		"message": "OTLP telemetry status updated",
	})
}

// GetOTLPEndpoint returns the current OTLP endpoint
func (h *Handler) GetOTLPEndpoint(c *gin.Context) {
	endpoint := usage.OTLPEndpoint()
	c.JSON(http.StatusOK, gin.H{
		"endpoint": endpoint,
	})
}

// SetOTLPEndpoint sets the OTLP endpoint
func (h *Handler) SetOTLPEndpoint(c *gin.Context) {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	usage.SetOTLPEndpoint(req.Endpoint)
	c.JSON(http.StatusOK, gin.H{
		"endpoint": req.Endpoint,
		"message":  "OTLP endpoint updated",
	})
}
