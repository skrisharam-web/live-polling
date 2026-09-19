package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders sets the response headers that cost nothing and remove whole
// classes of attack.
//
// Each one earns its place:
//
//   - nosniff stops a browser deciding for itself that a JSON response is
//     really HTML. Without it, an endpoint that echoes user-supplied text can be
//     talked into executing it by a browser that sniffs the content type.
//   - frame-ancestors 'none' (with the older X-Frame-Options alongside it, for
//     browsers that predate CSP) means no page can put this API in a frame.
//   - The rest of the CSP is default-src 'none': this origin serves JSON and a
//     WebSocket, so a response from it should never be able to load a script,
//     an image or a stylesheet. If one ever tries, something is wrong.
//   - A referrer policy stops poll IDs leaking to third parties through the
//     Referer header — a share link is a capability, and quietly handing it to
//     an analytics script is how a "private" poll stops being one.
//
// HSTS is set only in production, where the site is actually served over HTTPS;
// sending it from a development server over plain HTTP would pin localhost to
// HTTPS in the developer's browser and break it for a year.
func SecurityHeaders(isProduction bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")

		if isProduction {
			header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		c.Next()
	}
}
