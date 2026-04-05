package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	grpcpkg "github.com/tesserix/crossplane-validation/pkg/grpc"
	"github.com/tesserix/crossplane-validation/pkg/notify"
	"github.com/tesserix/crossplane-validation/pkg/operator"
)

var version = "dev"

func main() {
	var (
		apiPort        int
		healthPort     int
		watchGroups    string
		kubeconfig     string
		logLevel       string
		slackURL       string
		teamsURL       string
		apiToken       string
		leaderElect    bool
		leaderLockName string
		podName        string
		podNamespace   string
	)

	flag.IntVar(&apiPort, "grpc-port", 9443, "API server port")
	flag.IntVar(&healthPort, "health-port", 8081, "Health check HTTP port")
	flag.StringVar(&watchGroups, "watch-groups", "", "Additional API groups to watch (comma-separated)")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (uses in-cluster config if empty)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.StringVar(&slackURL, "slack-webhook", "", "Slack webhook URL for notifications")
	flag.StringVar(&teamsURL, "teams-webhook", "", "Teams webhook URL for notifications")
	flag.StringVar(&apiToken, "api-token", os.Getenv("API_TOKEN"), "Bearer token for API authentication (required for external access)")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election for HA deployments")
	flag.StringVar(&leaderLockName, "leader-lock-name", "crossplane-validate-operator", "Leader election lock name")
	flag.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "Pod name (from downward API)")
	flag.StringVar(&podNamespace, "pod-namespace", os.Getenv("POD_NAMESPACE"), "Pod namespace (from downward API)")
	flag.Parse()

	log.Printf("crossplane-validate-operator %s starting", version)

	config, err := buildConfig(kubeconfig)
	if err != nil {
		log.Fatalf("building kubernetes config: %v", err)
	}

	config.QPS = 50
	config.Burst = 100

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("creating dynamic client: %v", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		log.Fatalf("creating discovery client: %v", err)
	}

	var extraGroups []string
	if watchGroups != "" {
		extraGroups = strings.Split(watchGroups, ",")
	}

	cache := operator.NewStateCache(dynamicClient, discoveryClient, extraGroups)

	var notifiers []notify.Notifier
	if slackURL != "" {
		notifiers = append(notifiers, notify.NewSlackNotifier(slackURL, ""))
	}
	if teamsURL != "" {
		notifiers = append(notifiers, notify.NewTeamsNotifier(teamsURL))
	}

	var notifier notify.Notifier
	if len(notifiers) > 0 {
		notifier = notify.NewMultiNotifier(notifiers...)
	}

	httpAPI := grpcpkg.NewHTTPServer(grpcpkg.HTTPServerConfig{
		Cache:    cache,
		Port:     apiPort,
		Notifier: notifier,
		APIToken: apiToken,
	})

	var ready atomic.Bool
	errCh := make(chan error, 2)

	go func() {
		if err := httpAPI.Start(); err != nil {
			errCh <- fmt.Errorf("API server: %w", err)
		}
	}()

	healthSrv := startHealthServer(healthPort, cache, &ready)

	run := func(ctx context.Context) {
		log.Println("starting state cache, discovering Crossplane resources...")
		if err := cache.Start(ctx); err != nil {
			log.Printf("state cache start error: %v", err)
			errCh <- err
			return
		}
		ready.Store(true)
		log.Printf("operator ready — API :%d, health :%d, cached %d resources",
			apiPort, healthPort, cache.ResourceCount())

		<-ctx.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if leaderElect {
		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			log.Fatalf("creating kubernetes clientset: %v", err)
		}

		ns := podNamespace
		if ns == "" {
			ns = "crossplane-validator"
		}
		id := podName
		if id == "" {
			hostname, _ := os.Hostname()
			id = hostname
		}

		log.Printf("starting leader election (id=%s, namespace=%s)", id, ns)

		lock := &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaderLockName,
				Namespace: ns,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: id,
			},
		}

		go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			LeaseDuration:   15 * time.Second,
			RenewDeadline:   10 * time.Second,
			RetryPeriod:     2 * time.Second,
			ReleaseOnCancel: true,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: run,
				OnStoppedLeading: func() {
					log.Println("lost leadership, shutting down")
					cancel()
				},
				OnNewLeader: func(identity string) {
					if identity != id {
						log.Printf("new leader elected: %s", identity)
					}
				},
			},
		})
	} else {
		go run(ctx)
	}

	select {
	case sig := <-sigCh:
		log.Printf("received %s, shutting down...", sig)
	case err := <-errCh:
		log.Printf("fatal error: %v, shutting down...", err)
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	httpAPI.Stop()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("health server shutdown: %v", err)
	}
	cache.Stop()
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig == "" {
		config, err := rest.InClusterConfig()
		if err == nil {
			return config, nil
		}
		log.Println("not running in-cluster, trying default kubeconfig")
	}

	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = home + "/.kube/config"
	}

	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}

func startHealthServer(port int, cache *operator.StateCache, ready *atomic.Bool) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, "cache syncing")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ok, %d resources cached\n", cache.ResourceCount())
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("health server listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("health server error: %v", err)
		}
	}()

	return srv
}
