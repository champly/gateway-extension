package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/symcn/pkg/metrics"
	"k8s.io/klog/v2"
)

var (
	HttpPort = 8080
)

// startHTTPServer start http server with prometheus route
func startHTTPServer(ctx context.Context) error {
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", HttpPort),
	}
	mux := http.NewServeMux()
	metrics.RegisterHTTPHandler(func(pattern string, handler http.Handler) {
		mux.Handle(pattern, handler)
	})
	registryProbleCheck(mux)
	initDebug(mux)
	server.Handler = mux

	go func() {
		if err := server.ListenAndServe(); err != nil {
			if !strings.EqualFold(err.Error(), "http: Server closed") {
				klog.Error(err)
				return
			}
		}
		klog.Info("http shutdown")
	}()
	<-ctx.Done()
	return server.Shutdown(context.Background())
}
