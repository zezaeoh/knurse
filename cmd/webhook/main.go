package main

import (
	"flag"
	"github.com/zezaeoh/knurse/internal/config"
	"knative.dev/pkg/webhook/certificates"
	"os"
	"strconv"

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/webhook"
)

const (
	defaultWebhookSecretName = "knurse-tls"
	defaultServiceName       = "knurse"
	defaultPort              = 8443
)

// init initialize configs
func init() {
	config.InitFlags(flag.CommandLine)
}

func main() {
	wsn := os.Getenv("KNURSE_WEBHOOK_SECRET_NAME")
	if wsn == "" {
		wsn = defaultWebhookSecretName
	}

	sn := os.Getenv("KNURSE_SERVICE_NAME")
	if sn == "" {
		sn = defaultServiceName
	}

	port, err := strconv.Atoi(os.Getenv("KNURSE_WEBHOOK_PORT"))
	if err != nil {
		port = defaultPort
	}

	ctx := webhook.WithOptions(signals.NewContext(), webhook.Options{
		ServiceName: sn,
		Port:        port,
		SecretName:  wsn,
	})

	sharedmain.MainWithContext(ctx, "knurse",
		certificates.NewController,
	)
}
