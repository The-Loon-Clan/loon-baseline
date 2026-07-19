package webauth

import (
	"github.com/gin-gonic/gin"

	"github.com/the-loon-clan/loon/core"
)

// CoreAuth builds a loon core.AuthService from this Auth, so plugins get the
// host's session policy through the standard seam (c.Auth.RequireUser(...) etc.).
//
// Optional and Authenticate both map to Soft: this baseline has no closed-mode
// "anonymous ⇒ redirect" policy (that is a product decision a richer host layers
// on by supplying its own AuthenticateFn). RequireUser/RequireRole map to
// Require/RequireExact; CurrentUser reads the resolved user.
func (a Auth) CoreAuth() core.AuthService {
	return core.NewAuth(core.AuthAdapter{
		OptionalFn:     func() gin.HandlersChain { return gin.HandlersChain{a.Soft()} },
		AuthenticateFn: func() gin.HandlersChain { return gin.HandlersChain{a.Soft()} },
		RequireUserFn:  a.Require,
		RequireRoleFn:  a.RequireExact,
		CurrentUserFn:  a.Current,
	})
}
