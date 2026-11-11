package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/clicheck"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/config"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/static"
)

// setupOptions 讓測試替換前端資源、CLI 檢查，以及同埠切換時要用的正常模式啟動。
type setupOptions struct {
	assets      fs.FS
	check       func(context.Context) []clicheck.Tool
	startNormal func(context.Context) (*Server, error)
}

const setupBodyLimit = 64 << 10

// setupOnMissing 只在設定檔不存在時進入設定模式。壞掉的 YAML 不是缺檔。
func setupOnMissing(err error) bool {
	return errors.Is(err, config.ErrConfigMissing)
}

// startSetup 只提供首次設定需要的頁面與三支 API，不開資料庫、不啟動代理或 MCP。
func startSetup(ctx context.Context, port int, opts setupOptions) (*Server, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	assets := opts.assets
	if assets == nil {
		assets = static.Assets
	}
	check := opts.check
	if check == nil {
		check = clicheck.Check
	}
	starter := opts.startNormal
	if starter == nil {
		starter = startNormal
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("產生設定憑證失敗: %w", err)
	}
	token := hex.EncodeToString(buf)

	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(wd, "config.yaml")

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		BodyLimit:             setupBodyLimit,
		// 切換後舊連線仍綁在設定模式的 worker。關掉 keep-alive，下一條請求才會連到新的正常模式。
		DisableKeepalive: true,
	})
	// 設定網址帶一次性憑證，而頁面會載入第三方腳本；不讓瀏覽器把這個網址當 Referer 送出去。
	app.Use(func(c *fiber.Ctx) error {
		c.Set("Referrer-Policy", "no-referrer")
		return c.Next()
	})
	app.Use(rejectCompressedBody)
	app.Use(static.Handler(assets))
	app.Get("/setup/status", setupGuard(port, token, false), func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"needs_setup": true,
			"port":        port,
			"config_path": configPath,
		})
	})
	app.Get("/setup/cli-check", setupGuard(port, token, true), func(c *fiber.Ctx) error {
		tools := check(c.UserContext())
		if tools == nil {
			tools = []clicheck.Tool{}
		}
		return c.JSON(fiber.Map{"tools": tools})
	})
	s := &Server{done: make(chan struct{})}
	var saveMu sync.Mutex
	app.Post("/setup/save", setupGuard(port, token, true), func(c *fiber.Ctx) error {
		return handleSetupSave(c, setupSaveEnv{
			s: s, app: app, listenPort: port, configPath: configPath,
			ctx: ctx, starter: starter, mu: &saveMu,
		})
	})
	app.Use(func(c *fiber.Ctx) error {
		return c.Status(503).JSON(fiber.Map{"error": "需要先完成首次設定", "code": "setup_required"})
	})

	// 只綁 loopback：設定模式沒有登入，區網不該連得到，即使防火牆沒擋。
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("無法監聽 %s（埠可能被占用）: %w", addr, err)
	}
	wrapped := &setupListener{Listener: ln}
	s.mu.Lock()
	s.app = app
	s.url = "http://" + addr
	s.entryURL = fmt.Sprintf("http://%s/?setup=%s", addr, token)
	s.setupLn = wrapped
	s.mu.Unlock()
	go s.serveLoop(app, wrapped, false)
	// 憑證只放在這一行與 EntryURL，不要再寫進其他 log 或錯誤訊息。
	slog.Info("首次使用，請在這台電腦開啟此網址完成設定：" + s.EntryURL())
	return s, nil
}

// setupSaveEnv 是儲存請求要碰到的設定模式狀態。mu 讓同時進來的儲存排成一列。
type setupSaveEnv struct {
	s          *Server
	app        *fiber.App
	listenPort int
	configPath string
	ctx        context.Context
	starter    func(context.Context) (*Server, error)
	mu         *sync.Mutex
}

func handleSetupSave(c *fiber.Ctx, env setupSaveEnv) error {
	env.mu.Lock()
	defer env.mu.Unlock()

	if _, err := os.Lstat(env.configPath); err == nil {
		return setupJSON(c, 409, "already_configured", "", "設定檔已存在")
	}

	in, field, err := decodeSetupInput(c.Body())
	if err != nil {
		return setupJSON(c, 400, "invalid_input", field, "JSON 格式不正確")
	}
	if ferr := config.ValidateSetup(in); ferr != nil {
		return setupJSON(c, 400, "invalid_input", ferr.Field, ferr.Message)
	}
	body, err := config.RenderConfig(in)
	if err != nil {
		return setupJSON(c, 500, "write_failed", "", scrubSecret(err.Error(), in.Password))
	}
	if err := config.WriteNew(env.configPath, body); err != nil {
		if errors.Is(err, config.ErrConfigExists) {
			return setupJSON(c, 409, "already_configured", "", "設定檔已存在")
		}
		return setupJSON(c, 500, "write_failed", "", scrubSecret(err.Error(), in.Password))
	}
	if in.Port != env.listenPort {
		return c.JSON(fiber.Map{
			"ok": true, "applied": "restart_required",
			"url": fmt.Sprintf("http://127.0.0.1:%d/", in.Port),
		})
	}

	// 不能在這條連線裡 Shutdown 整個 app，否則會等自己。只關監聽，埠才讓得出來。
	if err := env.s.releaseSetupListener(); err != nil {
		// 監聽已經關掉卻沒確認結束時，仍要把設定頁綁回去，否則桌面視窗連不上錯誤。
		env.s.rebindSetup(env.app, env.listenPort)
		return setupJSON(c, 500, "start_failed", "", scrubSecret(err.Error(), in.Password))
	}
	next, err := startNormalRetry(env.ctx, env.starter)
	if err != nil {
		env.s.rebindSetup(env.app, env.listenPort)
		slog.Error("設定模式切換失敗", "code", "start_failed")
		return setupJSON(c, 500, "start_failed", "", scrubSecret(err.Error(), in.Password))
	}
	env.s.adoptNormal(next)
	return c.JSON(fiber.Map{
		"ok": true, "applied": "switched",
		"url": fmt.Sprintf("http://127.0.0.1:%d/", in.Port),
	})
}

func decodeSetupInput(body []byte) (config.SetupInput, string, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var in config.SetupInput
	if err := dec.Decode(&in); err != nil {
		return config.SetupInput{}, "body", err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return config.SetupInput{}, "body", errors.New("多出來的 JSON")
	}
	return in, "", nil
}

func setupJSON(c *fiber.Ctx, status int, code, field, msg string) error {
	if msg == "" {
		msg = code
	}
	body := fiber.Map{"error": msg, "code": code}
	if field != "" {
		body["field"] = field
	}
	return c.Status(status).JSON(body)
}

func scrubSecret(msg, secret string) string {
	if secret != "" {
		msg = strings.ReplaceAll(msg, secret, "")
	}
	if strings.TrimSpace(msg) == "" {
		return "設定失敗"
	}
	return msg
}

// setupListener 的 Close 可以重複呼叫。fasthttp 關掉時若碰到已經關過的監聽會直接放棄剩下的，
// 切換失敗重新綁上之後就關不乾淨。
type setupListener struct {
	net.Listener
	once sync.Once
}

func (l *setupListener) Close() error {
	l.once.Do(func() { _ = l.Listener.Close() })
	return nil
}

// serveLoop 跑設定模式的監聽。keepAlive 表示這次結束是為了把埠讓給正常模式，不能關掉 Wait。
func (s *Server) serveLoop(app *fiber.App, ln net.Listener, rebound bool) {
	var err error
	if rebound {
		// 重新綁定時不要再 buildTree，進行中的儲存請求還在用同一棵路由。
		err = app.Server().Serve(ln)
	} else {
		err = app.Listener(ln)
	}
	s.mu.Lock()
	keep := s.keepAlive
	ch := s.listenerStopped
	s.listenerStopped = nil
	if keep {
		s.keepAlive = false
	}
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
	if keep {
		return
	}
	s.finish(err)
}

func (s *Server) releaseSetupListener() error {
	stopped := make(chan struct{})
	s.mu.Lock()
	ln := s.setupLn
	if ln == nil {
		s.mu.Unlock()
		return errors.New("設定模式監聽已結束")
	}
	s.setupLn = nil
	s.keepAlive = true
	s.listenerStopped = stopped
	s.mu.Unlock()
	_ = ln.Close()
	select {
	case <-stopped:
		return nil
	case <-time.After(5 * time.Second):
		return errors.New("關閉設定模式監聽逾時")
	}
}

func (s *Server) rebindSetup(app *fiber.App, port int) {
	ln, err := retryListen(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		slog.Error("設定模式無法重新監聽", "err", err)
		return
	}
	wrapped := &setupListener{Listener: ln}
	s.mu.Lock()
	s.setupLn = wrapped
	s.app = app
	s.keepAlive = false
	s.mu.Unlock()
	go s.serveLoop(app, wrapped, true)
}

// adoptNormal 把已經在聽的正常模式換進同一個 Server。舊的 Wait 改等這一個結束。
func (s *Server) adoptNormal(next *Server) {
	next.mu.Lock()
	app := next.app
	url := next.url
	next.mu.Unlock()
	s.mu.Lock()
	s.app = app
	s.url = url
	s.entryURL = url
	s.mu.Unlock()
	go func() {
		<-next.done
		next.mu.Lock()
		err := next.serveErr
		next.mu.Unlock()
		s.finish(err)
	}()
}

func startNormalRetry(ctx context.Context, starter func(context.Context) (*Server, error)) (*Server, error) {
	var last error
	for i := 0; i < 20; i++ {
		next, err := starter(ctx)
		if err == nil {
			return next, nil
		}
		last = err
		if !addrInUse(err) {
			return nil, err
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, last
}

func retryListen(addr string) (net.Listener, error) {
	var last error
	for i := 0; i < 20; i++ {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		last = err
		time.Sleep(25 * time.Millisecond)
	}
	return nil, last
}

func addrInUse(err error) bool {
	return errors.Is(err, syscall.EADDRINUSE)
}
