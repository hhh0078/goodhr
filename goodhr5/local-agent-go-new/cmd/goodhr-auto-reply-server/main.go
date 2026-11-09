// Command goodhr-auto-reply-server 在服务器运行自动回复编排与 AI，浏览器命令由已认证设备主动领取。
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"goodhr5/local-agent-go-new/internal/cloudserver"
)

// main 只从服务环境读取云后端、私有持久卷和服务鉴权，不接受设备提交的存储地址。
func main() {
	svc, err := cloudserver.New(os.Getenv("GOODHR_BACKEND_URL"), os.Getenv("GOODHR_AUTO_REPLY_SERVICE_TOKEN"), os.Getenv("GOODHR_AUTO_REPLY_FILES_DIR"))
	if err != nil {
		log.Fatal(err)
	}
	listen := os.Getenv("GOODHR_AUTO_REPLY_LISTEN")
	if listen == "" {
		listen = "127.0.0.1:18646"
	}
	server := &http.Server{Addr: listen, Handler: svc, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 120 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 90 * time.Second}
	stop, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-stop.Done()
		ctx, done := context.WithTimeout(context.Background(), 25*time.Second)
		defer done()
		_ = server.Shutdown(ctx)
	}()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	svc.Close()
}
