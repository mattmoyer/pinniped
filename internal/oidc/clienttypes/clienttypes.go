// Copyright 2021 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

// Package clienttypes defines the OAuth2/OIDC client type representing the Pinniped CLI client.
package clienttypes

import (
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/ory/fosite"
)

type Client struct {
	*fosite.DefaultClient
	*fosite.DefaultOpenIDConnectClient
	*fosite.DefaultResponseModeClient
}

func New() *Client {
	return &Client{
		DefaultClient: &fosite.DefaultClient{
			ID:            "pinniped-cli",
			Public:        true,
			RedirectURIs:  []string{"http://127.0.0.1/callback"},
			ResponseTypes: []string{"code"},
			GrantTypes:    []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:token-exchange"},
			Scopes:        []string{oidc.ScopeOpenID, oidc.ScopeOfflineAccess, "profile", "email", "pinniped:request-audience"},
		},
		DefaultOpenIDConnectClient: &fosite.DefaultOpenIDConnectClient{
			TokenEndpointAuthMethod: "none",
		},
		DefaultResponseModeClient: &fosite.DefaultResponseModeClient{
			ResponseModes: []fosite.ResponseModeType{fosite.ResponseModeDefault, fosite.ResponseModeQuery, fosite.ResponseModeFormPost},
		},
	}
}
