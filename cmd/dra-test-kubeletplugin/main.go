/*
 * Copyright 2023 The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	coreclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/dynamic-resource-allocation/kubeletplugin"
	"k8s.io/klog/v2"

	"sigs.k8s.io/dra-test-driver/internal/profiles"
	"sigs.k8s.io/dra-test-driver/internal/profiles/network"
)

const (
	DriverPluginCheckpointFile = "checkpoint.json"
)

type Config struct {
	nodeName                    string
	cdiRoot                     string
	numDevices                  int
	kubeletPluginsDirectoryPath string
	driverName                  string
	kubeconfig                  string

	coreclient    coreclientset.Interface
	cancelMainCtx func(error)
	profile       profiles.Profile
}

func (c Config) DriverPluginPath() string {
	return filepath.Join(c.kubeletPluginsDirectoryPath, c.driverName)
}

func main() {
	config := &Config{}

	flag.StringVar(&config.nodeName, "node-name", os.Getenv("NODE_NAME"), "The name of the node (required)")
	flag.StringVar(&config.cdiRoot, "cdi-root", "/etc/cdi", "Absolute path to the CDI directory")
	flag.IntVar(&config.numDevices, "num-devices", 10, "Number of network devices to advertise")
	flag.StringVar(&config.kubeletPluginsDirectoryPath, "kubelet-plugins-directory-path", kubeletplugin.KubeletPluginsDir, "Kubelet plugins directory")
	flag.StringVar(&config.driverName, "driver-name", "hostpath.network.com", "DRA driver name")
	flag.StringVar(&config.kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "Path to kubeconfig (optional, uses in-cluster config if not set)")

	klog.InitFlags(nil)
	flag.Parse()

	if config.nodeName == "" {
		fmt.Fprintf(os.Stderr, "Error: --node-name is required\n")
		os.Exit(1)
	}

	ctx := context.Background()

	// Create Kubernetes client
	var err error
	config.coreclient, err = newKubeClient(config.kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	// Create network profile
	config.profile = network.NewProfile(config.nodeName, config.numDevices)

	if err := RunPlugin(ctx, config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newKubeClient(kubeconfig string) (coreclientset.Interface, error) {
	var config *rest.Config
	var err error

	if kubeconfig == "" {
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("create in-cluster config: %w", err)
		}
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("create config from kubeconfig: %w", err)
		}
	}

	client, err := coreclientset.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create clientset: %w", err)
	}

	return client, nil
}

func RunPlugin(ctx context.Context, config *Config) error {
	logger := klog.FromContext(ctx)

	err := os.MkdirAll(config.DriverPluginPath(), 0750)
	if err != nil {
		return err
	}

	info, err := os.Stat(config.cdiRoot)
	switch {
	case err != nil && os.IsNotExist(err):
		err := os.MkdirAll(config.cdiRoot, 0750)
		if err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("path for cdi file generation is not a directory: '%v'", err)
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer stop()
	ctx, cancel := context.WithCancelCause(ctx)
	config.cancelMainCtx = cancel

	driver, err := NewDriver(ctx, config)
	if err != nil {
		return err
	}

	<-ctx.Done()
	stop()
	if err := context.Cause(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error(err, "error from context")
	}

	err = driver.Shutdown(logger)
	if err != nil {
		logger.Error(err, "Unable to cleanly shutdown driver")
	}

	return nil
}
