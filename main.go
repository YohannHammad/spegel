package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/signal"

	"github.com/alexflint/go-arg"
	"github.com/containerd/containerd"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/spf13/afero"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/xenitab/spegel/internal/mirror"
	"github.com/xenitab/spegel/internal/registry"
	"github.com/xenitab/spegel/internal/state"
	"github.com/xenitab/spegel/internal/store"
)

type arguments struct {
	PodIP                        string    `arg:"--pod-ip,required"`
	ServiceName                  string    `arg:"--service-name,required"`
	RedisEndpoints               []string  `arg:"--redis-endpoints,required"`
	MirrorRegistries             []url.URL `arg:"--mirror-registries,required"`
	RegistryAddr                 string    `arg:"--registry-addr" default:":5000"`
	ContainerdSock               string    `arg:"--containerd-sock" default:"/run/containerd/containerd.sock"`
	ContainerdNamespace          string    `arg:"--containerd-namespace" default:"k8s.io"`
	ContainerdRegistryConfigPath string    `arg:"--containerd-registry-config-path" default:"/etc/containerd/certs.d"`
	ContainerdMirrorAdd          bool      `arg:"--containerd-mirror-add" default:"true"`
	ContainerdMirrorRemove       bool      `arg:"--containerd-mirror-remove" default:"true"`
}

func main() {
	args := &arguments{}
	arg.MustParse(args)

	zapLog, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("who watches the watchmen (%v)?", err))
	}
	log := zapr.NewLogger(zapLog)
	ctx := logr.NewContext(context.Background(), log)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	containerdClient, err := containerd.New(args.ContainerdSock, containerd.WithDefaultNamespace(args.ContainerdNamespace))
	if err != nil {
		log.Error(err, "could not create containerd client")
		os.Exit(1)
	}
	defer containerdClient.Close()

	// Setup and run store
	store, err := store.NewRedisStore(args.PodIP, store.NewDNS(args.ServiceName), args.RedisEndpoints)
	if err != nil {
		log.Error(err, "could not create store")
		os.Exit(1)
	}
	g.Go(func() error {
		return state.Track(ctx, containerdClient, store)
	})

	// Configure mirrors
	if args.ContainerdMirrorAdd {
		fs := afero.NewOsFs()
		err := mirror.AddMirrorConfiguration(ctx, fs, args.ContainerdRegistryConfigPath, args.RegistryAddr, args.MirrorRegistries)
		if err != nil {
			log.Error(err, "could not configure containerd mirror")
			os.Exit(1)
		}
		// TODO: Validate clean up is run if error occurs before start.
		if args.ContainerdMirrorRemove {
			g.Go(func() error {
				<-ctx.Done()
				return mirror.RemoveMirrorConfiguration(ctx, fs, args.ContainerdRegistryConfigPath, args.MirrorRegistries)
			})
		}
	}

	// Setup and run registry
	reg, err := registry.NewRegistry(ctx, args.RegistryAddr, containerdClient, store)
	if err != nil {
		log.Error(err, "could not create registry")
		os.Exit(1)
	}
	g.Go(func() error {
		return reg.ListenAndServe(ctx)
	})
	g.Go(func() error {
		<-ctx.Done()
		return reg.Shutdown()
	})

	log.Info("running registry", "addr", args.RegistryAddr)
	err = g.Wait()
	if err != nil {
		log.Error(err, "exiting with error")
		os.Exit(1)
	}
	log.Info("gracefully shutdown registry")
}