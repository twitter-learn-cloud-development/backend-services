package router

import (
	"github.com/gin-gonic/gin"

	"twitter-clone/internal/gateway/handler"
	"twitter-clone/internal/gateway/middleware"
)

func RegisterSupportQuery(
	router *gin.Engine,
	auth *middleware.SupportQueryAuth,
	queryHandler *handler.SupportQueryHandler,
) {
	if router == nil || auth == nil || queryHandler == nil {
		return
	}
	router.POST(
		"/internal/v1/support/query",
		auth.Authenticate(),
		queryHandler.Execute,
	)
}
