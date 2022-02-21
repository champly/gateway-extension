package main

import (
	"math/rand"
	"time"

	"github.com/champly/gateway-extension/cmd/gateway"
	"k8s.io/klog/v2"
)

func main() {
	rand.Seed(time.Now().UnixNano())

	cmd := gateway.NewRootCmd()
	if err := cmd.Execute(); err != nil {
		klog.Errorf("Execute gateway-extension failed.")
	}
}
