package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mingzaily/mailwake/internal/auth"
	"github.com/mingzaily/mailwake/internal/fault"
	"github.com/mingzaily/mailwake/internal/storage"
)

const sessionCookie = "mailwake_session"

func decodeRequest(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(c, 413, fault.New("input_too_large"))
			return false
		}
		respondError(c, 400, fault.New("request_invalid"))
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		respondError(c, 400, fault.New("request_invalid"))
		return false
	}
	return true
}
func setSession(c *gin.Context, g auth.Grant) {
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: g.ID, Path: "/", HttpOnly: true, Secure: secureRequest(c), SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 60 * 60, Expires: time.Now().Add(30 * 24 * time.Hour)})
}
func setupRoute(r *gin.Engine, service *auth.Service) {
	r.POST("/api/v1/setup", func(c *gin.Context) {
		var input struct {
			Code     string `json:"code"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		if !sameOriginOrAbsent(c) {
			respondError(c, 403, fault.New("csrf_invalid"))
			return
		}
		g, err := service.Setup(c.Request.Context(), c.ClientIP(), input.Code, input.Username, input.Password)
		if err != nil {
			respondFault(c, err)
			return
		}
		setSession(c, g)
		c.JSON(201, g)
	})
}
func authenticate(service *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if header := c.GetHeader("Authorization"); header != "" {
			if !strings.HasPrefix(header, "Bearer ") {
				respondError(c, 401, fault.New("unauthorized"))
				return
			}
			if err := service.AuthenticateToken(c.Request.Context(), strings.TrimPrefix(header, "Bearer ")); err != nil {
				respondFault(c, err)
				return
			}
			c.Next()
			return
		}
		cookie, err := c.Request.Cookie(sessionCookie)
		if err != nil {
			respondError(c, 401, fault.New("unauthorized"))
			return
		}
		session, err := service.AuthenticateSession(c.Request.Context(), cookie.Value)
		if err != nil {
			respondFault(c, err)
			return
		}
		if c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS" {
			if !sameOrigin(c) || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
				respondError(c, 403, fault.New("csrf_invalid"))
				return
			}
		}
		c.Set("session", session)
		c.Next()
	}
}

func loginRoute(r *gin.Engine, service *auth.Service) {
	r.POST("/api/v1/session", func(c *gin.Context) {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		// Login and setup have no prior session; an explicit cross-site Origin is rejected.
		if !sameOriginOrAbsent(c) {
			respondError(c, 403, fault.New("csrf_invalid"))
			return
		}
		g, err := service.Login(c.Request.Context(), c.ClientIP(), input.Username, input.Password)
		if err != nil {
			respondFault(c, err)
			return
		}
		setSession(c, g)
		c.JSON(200, g)
	})
}

// An Origin is a serialized HTTP(S) origin: scheme and authority only.
func requestOrigin(c *gin.Context) (*url.URL, bool) {
	raw := c.GetHeader("Origin")
	origin, err := url.Parse(raw)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.User != nil || raw != origin.Scheme+"://"+origin.Host {
		return nil, false
	}
	return origin, true
}
func sameOrigin(c *gin.Context) bool {
	origin, ok := requestOrigin(c)
	return ok && origin.Host == c.Request.Host
}
func sameOriginOrAbsent(c *gin.Context) bool { return c.GetHeader("Origin") == "" || sameOrigin(c) }
func secureRequest(c *gin.Context) bool {
	origin, ok := requestOrigin(c)
	return c.Request.TLS != nil || (ok && origin.Scheme == "https")
}
func sessionRoutes(api *gin.RouterGroup, service *auth.Service) {
	api.GET("/session", func(c *gin.Context) {
		v, exists := c.Get("session")
		if !exists {
			c.JSON(200, gin.H{"authenticated": true, "method": "token"})
			return
		}
		s, ok := v.(*storage.Session)
		if !ok {
			respondError(c, 401, fault.New("unauthorized"))
			return
		}
		c.JSON(200, gin.H{"authenticated": true, "csrf_token": s.CSRF})
	})
	api.DELETE("/session", func(c *gin.Context) {
		v, exists := c.Get("session")
		if !exists {
			respondError(c, 400, fault.New("session_required"))
			return
		}
		s := v.(*storage.Session)
		if err := service.Logout(c.Request.Context(), s.Hash); err != nil {
			respondFault(c, err)
			return
		}
		http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: secureRequest(c)})
		c.Status(204)
	})
	api.PUT("/admin/password", func(c *gin.Context) {
		var input struct {
			Current  string `json:"current_password"`
			Password string `json:"password"`
		}
		if !decodeRequest(c, &input) {
			return
		}
		keep := ""
		if v, ok := c.Get("session"); ok {
			keep = v.(*storage.Session).Hash
		}
		if err := service.ChangePassword(c.Request.Context(), c.ClientIP(), keep, input.Current, input.Password); err != nil {
			respondFault(c, err)
			return
		}
		c.Status(204)
	})
}
