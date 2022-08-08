package main

import (
	"context"
	"encoding/json"
	errs "errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/rkrmr33/pkg/ctx"
	"github.com/rkrmr33/pkg/errors"
	"github.com/rkrmr33/pkg/log"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
)

var (
	flags pflag.FlagSet

	holderId           = flags.String("id", uuid.NewString(), "The holder identity")
	leaseName          = flags.String("lease-name", "", "The lease name")
	leaseDuration      = flags.Duration("lease-duration", 10*time.Second, "The duration of the lease")
	leaseRenewDuration = flags.Duration("lease-renew-duration", 5*time.Second, "The duration the leader will try to refresh the lease")
	namespace          = flags.StringP("namespace", "n", "default", "The lease namespace")
	kubeconfig         = flags.String("kubeconfig", "", "Path to kubeconfig file, not relevant if running in-cluster")
	addr               = flags.String("addr", ":4040", "Address to serve http server on")

	gracePeriod = 5 * time.Second
	logger      *log.Logger
)

type (
	LeaderResponse struct {
		Leader string `json:"leader"`
	}
)

func main() {
	errors.Must(validateFlags())

	cs, err := buildClient()
	errors.Must(err)

	ctx := ctx.ContextWithCancelOnSignals(context.Background(), syscall.SIGTERM, syscall.SIGINT)

	leaderC, err := runLeaderElection(ctx, cs)
	errors.Must(err)

	if err := serveHTTP(ctx, leaderC); !errs.Is(err, http.ErrServerClosed) {
		logger.Fatal(err)
	}
}

func serveHTTP(ctx context.Context, leaderC <-chan string) error {
	mux := http.NewServeMux()
	leader := ""

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	}

	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/readyz", healthHandler)

	mux.HandleFunc("/api/leader", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}

		lr := LeaderResponse{Leader: leader}

		res, err := json.Marshal(&lr)
		errors.Must(err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write(res); err != nil {
			logger.With("err", err).Error("failed to write response")
			return
		}
	})

	s := http.Server{
		Handler: mux,
		Addr:    *addr,
	}

	go func() {
		for {
			select {
			case newLeader := <-leaderC:
				leader = newLeader
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
				defer cancel()

				logger.Warnw("Shutting down server", "gracePeriod", gracePeriod)
				if err := s.Shutdown(shutdownCtx); err != nil {
					logger.With("err", err).Error("failed to shutdown server")
				}
			}
		}
	}()

	logger.Infow("Starting to server http", "addr", *addr)
	return s.ListenAndServe()
}

func runLeaderElection(ctx context.Context, cs *kubernetes.Clientset) (<-chan string, error) {
	eb := record.NewBroadcaster()
	eb.StartRecordingToSink(&typedv1.EventSinkImpl{Interface: cs.CoreV1().Events(*namespace)})

	lock := resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      *leaseName,
			Namespace: *namespace},
		Client: cs.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      *holderId,
			EventRecorder: eb.NewRecorder(scheme.Scheme, v1.EventSource{Component: "leader-election-controller"}),
		},
	}

	res := make(chan string)

	le, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:            &lock,
		LeaseDuration:   *leaseDuration,
		RenewDeadline:   *leaseRenewDuration,
		RetryPeriod:     3 * time.Second,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Info("Started leading")
			},
			OnStoppedLeading: func() {
				logger.Info("Stopped leading")
			},
			OnNewLeader: func(identity string) {
				logger.Infow("New leader", "leader", identity)
				res <- identity
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create leader-elector: %w", err)
	}

	logger.Infow("Starting leader-election", "lease", *leaseName, "namespace", *namespace)

	go le.Run(ctx)

	return res, nil
}

func buildClient() (*kubernetes.Clientset, error) {
	cfg, err := getClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build kubernetes config: %w", err)
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubernetes client: %w", err)
	}

	return cs, nil
}

func getClientConfig() (*rest.Config, error) {
	if *kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", *kubeconfig)
	}

	return rest.InClusterConfig()
}

func validateFlags() error {
	if *leaseName == "" {
		return fmt.Errorf("missing flag --lease-name")
	}

	return nil
}

func init() {
	var err error
	c := log.AddFlags(&flags)

	errors.Must(flags.Parse(os.Args[1:]))

	logger, err = log.Build(c)
	errors.Must(err)
}
