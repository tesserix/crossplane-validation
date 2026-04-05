package k8s

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardOptions configures a kubectl-style port-forward to the operator pod.
type PortForwardOptions struct {
	Namespace   string
	ServiceName string
	ServicePort int
	KubeContext string
}

// PortForwardResult holds the local port and a channel to stop the forward.
type PortForwardResult struct {
	LocalPort int
	StopChan  chan struct{}
}

// PortForward creates a port-forward to the operator pod using the user's kubeconfig.
func PortForward(ctx context.Context, opts PortForwardOptions) (*PortForwardResult, error) {
	config, err := buildConfig(opts.KubeContext)
	if err != nil {
		return nil, fmt.Errorf("building kubeconfig: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	pods, err := clientset.CoreV1().Pods(opts.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=crossplane-validate-operator"),
		Limit:         1,
	})
	if err != nil {
		return nil, fmt.Errorf("finding operator pod: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no operator pod found in namespace %s", opts.Namespace)
	}

	podName := pods.Items[0].Name

	localPort, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("finding free port: %w", err)
	}

	reqURL := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(opts.Namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("creating transport: %w", err)
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", reqURL)

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})

	ports := []string{fmt.Sprintf("%d:%d", localPort, opts.ServicePort)}

	fw, err := portforward.New(dialer, ports, stopChan, readyChan, os.Stdout, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("creating port-forward: %w", err)
	}

	errChan := make(chan error, 1)
	go func() {
		errChan <- fw.ForwardPorts()
	}()

	select {
	case <-readyChan:
		return &PortForwardResult{
			LocalPort: localPort,
			StopChan:  stopChan,
		}, nil
	case err := <-errChan:
		return nil, fmt.Errorf("port-forward failed: %w", err)
	case <-ctx.Done():
		close(stopChan)
		return nil, ctx.Err()
	}
}

func (r *PortForwardResult) Stop() {
	if r.StopChan != nil {
		close(r.StopChan)
	}
}

func (r *PortForwardResult) Address() string {
	return net.JoinHostPort("localhost", strconv.Itoa(r.LocalPort))
}

func buildConfig(kubeContext string) (*rest.Config, error) {
	if _, err := rest.InClusterConfig(); err == nil {
		return rest.InClusterConfig()
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
