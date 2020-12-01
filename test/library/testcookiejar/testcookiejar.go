// Copyright 2020 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package testcookiejar

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/publicsuffix"
)

// Option is a configuration for New().
type Option func(*testJar)

// WithIgnoreSecureFlag configures the cookie jar to ignore the "Secure" flag which normally prevents cookies from
// being set or read over non-HTTPS connections.
func WithIgnoreSecureFlag() Option { return func(j *testJar) { j.ignoreSecure = true } }

// WithVerboseLogging configures the cookie jar to log cookie reads/writes into the test log.
func WithVerboseLogging() Option { return func(j *testJar) { j.verbose = true } }

// New returns an http.CookieJar suitable for test scenarios.
func New(t *testing.T, opts ...Option) http.CookieJar {
	t.Helper()
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	require.NoError(t, err, "could not initialize cookie storage")
	result := testJar{t: t, wrapped: jar}
	for _, opt := range opts {
		opt(&result)
	}
	return &result
}

type testJar struct {
	t            *testing.T
	wrapped      http.CookieJar
	verbose      bool
	ignoreSecure bool
}

// SetCookies handles the receipt of the cookies in a reply for the
// given URL.  It may or may not choose to save the cookies, depending
// on the jar's policy and implementation.
func (j *testJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	for _, c := range cookies {
		if j.verbose {
			u := *u
			u.RawQuery = "[...]"
			j.t.Logf("setting cookie from %s: %q", u.String(), maskTokens(c.String()))
		}
		if j.ignoreSecure && c.Secure {
			j.t.Logf("clearing 'Secure' flag from cookie %q for testing", c.Name)
			c.Secure = false
		}
	}
	j.wrapped.SetCookies(u, cookies)
}

func (j *testJar) Cookies(u *url.URL) []*http.Cookie {
	result := j.wrapped.Cookies(u)

	if j.verbose {
		names := make([]string, 0, len(result))
		for _, c := range result {
			names = append(names, c.Name)
		}
		sort.Strings(names)
		u := *u
		u.RawQuery = "[...]"
		j.t.Logf("sending %d cookie(s) on request to %s: %v", len(result), u.String(), names)
	}

	return result
}

//nolint: gochecknoglobals
var tokenLike = regexp.MustCompile(`(?mi)[a-zA-Z0-9._-]{30,}|[a-zA-Z0-9]{20,}`)

func maskTokens(in string) string {
	return tokenLike.ReplaceAllStringFunc(in, func(t string) string {
		return fmt.Sprintf("[...%d bytes...]", len(t))
	})
}
