// Copyright 2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package conciergecmd

import (
	genericapiserver "go.pinniped.dev/internal/concierge/server"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/pkg/version"
	"k8s.io/client-go/rest"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"os"
	"time"
)

func Run() {
	logs.InitLogs()
	defer logs.FlushLogs()

	// Dump out the time since compile (mostly useful for benchmarking our local development cycle latency).
	var timeSinceCompile time.Duration
	if buildDate, err := time.Parse(time.RFC3339, version.Get().BuildDate); err == nil {
		timeSinceCompile = time.Since(buildDate).Round(time.Second)
	}
	klog.Infof("Running %s at %#v (%s since build)", rest.DefaultKubernetesUserAgent(), version.Get(), timeSinceCompile)

	ctx := server.SetupSignalContext()

	if err := genericapiserver.New(ctx, os.Args[1:], os.Stdout, os.Stderr).Run(); err != nil {
		klog.Fatal(err)
	}
}
