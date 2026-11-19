package controller

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/seats"
)

// seatPlanTTL is how long a node trusts its master's counts; past it the node counts its own seats.
const seatPlanTTL = 30 * time.Second

// SeatController serves a node's side of the user limit across servers.
type SeatController struct{}

func NewSeatController(g *gin.RouterGroup) *SeatController {
	a := &SeatController{}
	g.POST("/sync", a.sync)
	return a
}

// sync applies the master's plan and answers the seats this server holds after it.
func (a *SeatController) sync(c *gin.Context) {
	var plan seats.Plan
	if err := c.ShouldBindJSON(&plan); err != nil {
		jsonMsg(c, "seats", err)
		return
	}
	ledger := seats.Local()
	ledger.SetElsewhere(plan.Elsewhere, time.Now().Add(seatPlanTTL))
	ledger.Cut(plan.Cut)
	jsonObj(c, ledger.All(), nil)
}
