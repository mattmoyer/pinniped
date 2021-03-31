// Copyright 2020-2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"time"

	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"

	"go.pinniped.dev/internal/concierge/server"
)

func main() {
	for i := range os.Args {
		if os.Args[i] == "--sleep" {
			time.Sleep(time.Duration(math.MaxInt64))
			return
		}
		if os.Args[i] == "--cat" {
			f, err := os.Open(os.Args[i+1])
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not open file: %v", err)
				os.Exit(1)
			}
			if _, err := io.Copy(os.Stdout, f); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "could not copy from file: %v", err)
				os.Exit(1)
			}
			return
		}
	}

	logs.InitLogs()
	defer logs.FlushLogs()

	// Dump out the time since compile (mostly useful for benchmarking our local development cycle latency).
	var timeSinceCompile time.Duration
	if buildDate, err := time.Parse(time.RFC3339, version.Get().BuildDate); err == nil {
		timeSinceCompile = time.Since(buildDate).Round(time.Second)
	}
	klog.Infof("Running %s at %#v (%s since build)", rest.DefaultKubernetesUserAgent(), version.Get(), timeSinceCompile)

	ctx := genericapiserver.SetupSignalContext()

	if err := server.New(ctx, os.Args[1:], os.Stdout, os.Stderr).Run(); err != nil {
		klog.Fatal(err)
	}
}
