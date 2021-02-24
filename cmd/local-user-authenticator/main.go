// Copyright 2020-2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package main provides a authentication webhook program.
//
// This webhook is meant to be used in demo settings to play around with
// Pinniped. As well, it can come in handy in integration tests.
//
// This webhook is NOT meant for use in production systems.
package main

import (
	"go.pinniped.dev/cmd/local-user-authenticator/localuserauthcmd"
)

func main() {
	localuserauthcmd.Run()
}

