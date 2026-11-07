// Package imgsrv 提供只读图片服务：把本地缓存的画廊图片以不可变 URL 暴露给 Telegraph 页面。
package imgsrv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/usfese/telehentai/internal/store"
)

// TLSConfig 描述 TLS 暴露方式。
type TLSConfig struct {
	// Mode 为 off（自行反代）或 autocert（内置 Let's Encrypt）。
	Mode       string
	Domain     string
	Email      string
	CacheDir   string
	HTTPListen string
}

// Config 是图床配置。
type Config struct {
	Listen        string
	PublicBaseURL string
	TLS           TLSConfig
}

// Server 是图片服务；Handler 与 Run 可分别用于测试与生产。
type Server struct {
	cfg    Config
	store  *store.Store
	logger *slog.Logger
	mux    *http.ServeMux
}

// imageNameRe 限制可访问的文件名，杜绝路径穿越与任意文件读取。
var imageNameRe = regexp.MustCompile(`^\d{1,6}\.(webp|jpg|jpeg|png|gif)$`)

// contentTypes 硬编码扩展名到 MIME 的映射，避免依赖宿主机 mime.types。
var contentTypes = map[string]string{
	".webp": "image/webp",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
}

// New 构造图片服务。
func New(cfg Config, st *store.Store, logger *slog.Logger) (*Server, error) {
	if st == nil {
		return nil, errors.New("imgsrv: 缺少 store")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8088"
	}
	s := &Server{cfg: cfg, store: st, logger: logger}

	mux := http.NewServeMux()
	// Go 1.22 起支持方法+占位符模式；非 GET/HEAD 会自动 405。
	mux.HandleFunc("GET /i/{gid}/{name}", s.handleImage)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /robots.txt", s.handleRobots)
	s.mux = mux
	return s, nil
}

// Handler 返回可直接挂到 httptest 或反向代理的处理器。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	gid, ok := parseGID(r.PathValue("gid"))
	name := r.PathValue("name")
	if !ok || !imageNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(s.store.ImagePath(gid, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	ext := extensionOf(name)
	h := w.Header()
	h.Set("Content-Type", contentTypes[ext])
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Disposition", "inline")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// parseGID 校验 gid 是 1~8 位正整数。
func parseGID(raw string) (int, bool) {
	if len(raw) == 0 || len(raw) > 8 {
		return 0, false
	}
	for i := range len(raw) {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	gid, err := strconv.Atoi(raw)
	if err != nil || gid <= 0 {
		return 0, false
	}
	return gid, true
}

func extensionOf(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '.' {
			return name[i:]
		}
	}
	return ""
}

// Run 启动监听并在 ctx 取消时优雅关闭。
// autocert 模式固定监听 :443，另起 HTTP 监听负责 ACME 挑战与 https 跳转。
func (s *Server) Run(ctx context.Context) error {
	var servers []*http.Server
	errCh := make(chan error, 2)

	start := func(srv *http.Server, useTLS bool) {
		servers = append(servers, srv)
		go func() {
			var err error
			if useTLS {
				err = srv.ListenAndServeTLS("", "")
			} else {
				err = srv.ListenAndServe()
			}
			if errors.Is(err, http.ErrServerClosed) {
				err = nil
			}
			errCh <- err
		}()
	}

	base := func(handler http.Handler) *http.Server {
		return &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
	}

	switch s.cfg.TLS.Mode {
	case "autocert":
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(s.cfg.TLS.Domain),
			Cache:      autocert.DirCache(s.cfg.TLS.CacheDir),
			Email:      s.cfg.TLS.Email,
		}
		mainSrv := base(s.mux)
		mainSrv.Addr = ":443"
		mainSrv.TLSConfig = m.TLSConfig()
		s.logger.Info("图床启动（autocert）", "addr", ":443", "domain", s.cfg.TLS.Domain)

		httpSrv := base(m.HTTPHandler(http.HandlerFunc(redirectToHTTPS)))
		httpSrv.Addr = s.cfg.TLS.HTTPListen
		s.logger.Info("HTTP 监听启动（ACME 挑战与跳转）", "addr", s.cfg.TLS.HTTPListen)

		start(mainSrv, true)
		start(httpSrv, false)
	default:
		srv := base(s.mux)
		srv.Addr = s.cfg.Listen
		s.logger.Info("图床启动", "addr", s.cfg.Listen, "public_base_url", s.cfg.PublicBaseURL)
		start(srv, false)
	}

	select {
	case <-ctx.Done():
		s.shutdown(servers)
		return nil
	case err := <-errCh:
		s.shutdown(servers)
		if err == nil {
			return fmt.Errorf("图床监听意外退出")
		}
		return err
	}
}

func (s *Server) shutdown(servers []*http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if err := srv.Shutdown(ctx); err != nil {
			s.logger.Warn("图床关闭失败", "addr", srv.Addr, "err", err)
		}
	}
}

// redirectToHTTPS 把明文请求永久跳到 https，保留原路径与查询串。
func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusFound)
}
