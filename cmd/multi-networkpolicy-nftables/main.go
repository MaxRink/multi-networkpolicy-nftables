/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This is a Kubernetes controller to generate nftables rules for
// multi-networkpolicy.
// It reads multiNetworkpolicy object and generates nftables rules into
// container network namespaces.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/controller"
	"github.com/telekom/multi-networkpolicy-nftables/pkg/server"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

type managerStarter interface {
	Start(context.Context) error
}

func run(opts *server.Options) error {
	cfg, err := opts.BuildReconcilerConfig()
	if err != nil {
		return fmt.Errorf("build reconciler config: %w", err)
	}

	klog.Infof("hostname: %v", cfg.NodeName)

	var restCfg *rest.Config
	if cfg.Kubeconfig == "" {
		klog.Info("No kubeconfig specified. Falling back to in-cluster config.")
		restCfg, err = rest.InClusterConfig()
	} else {
		restCfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: cfg.Master}},
		).ClientConfig()
	}
	if err != nil {
		return fmt.Errorf("build kubeconfig: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := controller.SetupScheme(scheme); err != nil {
		return fmt.Errorf("setup scheme: %w", err)
	}

	ctrl.SetLogger(klog.NewKlogr())

	syncPeriod := time.Duration(cfg.SyncPeriodSeconds) * time.Second
	gracefulTimeout := 10 * time.Second
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          false,
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		Cache:                   cache.Options{SyncPeriod: &syncPeriod},
		GracefulShutdownTimeout: &gracefulTimeout,
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	ctx := ctrl.SetupSignalHandler()
	if err := controller.SetupIndexes(ctx, mgr); err != nil {
		return fmt.Errorf("setup indexes: %w", err)
	}

	reconciler := &controller.NodeReconciler{
		NodeName:                 cfg.NodeName,
		Client:                   mgr.GetClient(),
		HostPrefix:               cfg.HostPrefix,
		NetworkPlugins:           cfg.NetworkPlugins,
		CommonCfg:                cfg.CommonRuleConfig,
		ContainerRuntimeEndpoint: cfg.ContainerRuntimeEndpoint,
	}
	defer func() {
		if cerr := reconciler.CloseCRI(); cerr != nil {
			klog.Errorf("failed to close CRI connection: %v", cerr)
		}
	}()
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup reconciler: %w", err)
	}

	directClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create direct client for cleanup: %w", err)
	}

	klog.Infof("Starting manager for node %s", cfg.NodeName)
	return startManagerAndCleanup(ctx, mgr, 45*time.Second, func(cleanupCtx context.Context) error {
		return controller.CleanupOnShutdown(cleanupCtx, reconciler, directClient)
	})
}

func startManagerAndCleanup(
	ctx context.Context,
	mgr managerStarter,
	cleanupTimeout time.Duration,
	cleanup func(context.Context) error,
) error {
	startErr := mgr.Start(ctx)
	if startErr != nil && ctx.Err() == nil {
		klog.Errorf("manager stopped with error, skipping post-shutdown cleanup: %v", startErr)
		return startErr
	}
	if startErr != nil {
		klog.Errorf("manager stopped during shutdown with error, running post-shutdown cleanup: %v", startErr)
	} else {
		klog.Info("Manager stopped, running post-shutdown cleanup")
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	cleanupErr := cleanup(cleanupCtx)
	if startErr != nil || cleanupErr != nil {
		return errors.Join(startErr, cleanupErr)
	}
	return nil
}

func run(opts *server.Options) error {
	cfg, err := opts.BuildReconcilerConfig()
	if err != nil {
		return fmt.Errorf("build reconciler config: %w", err)
	}

	klog.Infof("hostname: %v", cfg.NodeName)
	klog.Infof("container-runtime: %v", cfg.ContainerRuntime)

	var restCfg *rest.Config
	if cfg.Kubeconfig == "" {
		klog.Info("No kubeconfig specified. Falling back to in-cluster config.")
		restCfg, err = rest.InClusterConfig()
	} else {
		restCfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: cfg.Kubeconfig},
			&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: cfg.Master}},
		).ClientConfig()
	}
	if err != nil {
		return fmt.Errorf("build kubeconfig: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := controller.SetupScheme(scheme); err != nil {
		return fmt.Errorf("setup scheme: %w", err)
	}

	ctrl.SetLogger(klog.NewKlogr())

	syncPeriod := time.Duration(cfg.SyncPeriodSeconds) * time.Second
	gracefulTimeout := 10 * time.Second
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          false,
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		Cache:                   cache.Options{SyncPeriod: &syncPeriod},
		GracefulShutdownTimeout: &gracefulTimeout,
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	ctx := ctrl.SetupSignalHandler()
	if err := controller.SetupIndexes(ctx, mgr); err != nil {
		return fmt.Errorf("setup indexes: %w", err)
	}

	if err := controller.PreparePodIptablesDir(cfg.PodIptables); err != nil {
		return fmt.Errorf("prepare pod iptables directory: %w", err)
	}

	criClient, criConn, err := controllers.GetCriRuntimeClient(cfg.ContainerRuntimeEndpoint, cfg.HostPrefix)
	if err != nil {
		klog.Warningf("failed to create CRI client (will retry at runtime): %v", err)
		criClient = nil
		criConn = nil
	}

	reconciler := &controller.NodeReconciler{
		NodeName:                 cfg.NodeName,
		Client:                   mgr.GetClient(),
		HostPrefix:               cfg.HostPrefix,
		PodIptables:              cfg.PodIptables,
		NetworkPlugins:           cfg.NetworkPlugins,
		CommonCfg:                cfg.CommonRuleConfig,
		CriClient:                criClient,
		CriConn:                  criConn,
		ContainerRuntimeEndpoint: cfg.ContainerRuntimeEndpoint,
	}
	defer func() {
		if cerr := reconciler.CloseCRI(); cerr != nil {
			klog.Errorf("failed to close CRI connection: %v", cerr)
		}
	}()
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setup reconciler: %w", err)
	}

	directClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("create direct client for cleanup: %w", err)
	}

	klog.Infof("Starting manager for node %s", cfg.NodeName)
	return startManagerAndCleanup(ctx, mgr, 45*time.Second, func(cleanupCtx context.Context) error {
		return controller.CleanupOnShutdown(cleanupCtx, reconciler, directClient)
	})
}

func startManagerAndCleanup(
	ctx context.Context,
	mgr managerStarter,
	cleanupTimeout time.Duration,
	cleanup func(context.Context) error,
) error {
	startErr := mgr.Start(ctx)
	if startErr != nil {
		klog.Errorf("manager stopped with error, running post-shutdown cleanup: %v", startErr)
	} else {
		klog.Info("Manager stopped, running post-shutdown cleanup")
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	cleanupErr := cleanup(cleanupCtx)

	return errors.Join(startErr, cleanupErr)
}

func main() {
	defer klog.Flush()
	opts := server.NewOptions()

	cmd := &cobra.Command{
		Use:  "multi-networkpolicy-node",
		Long: `Run the multi-networkpolicy nftables controller on a node.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return run(opts)
		},
	}
	opts.AddFlags(cmd.Flags())

	if err := cmd.Execute(); err != nil {
		klog.Infof("Execute failed: %v", err)
		os.Exit(1)
	}
}
