// Copyright 2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go.pinniped.dev/cmd/local-user-authenticator/localuserauthcmd"
	"go.pinniped.dev/cmd/pinniped-concierge/conciergecmd"
	"go.pinniped.dev/cmd/pinniped-supervisor/supervisorcmd"
)

func main() {
	localuserauthcmd.Run()
	conciergecmd.Run()
	supervisorcmd.Run()
}