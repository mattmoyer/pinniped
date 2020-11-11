// Copyright 2020 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package upstreamwatcher

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"go.pinniped.dev/generated/1.19/apis/supervisor/idp/v1alpha1"
	pinnipedfake "go.pinniped.dev/generated/1.19/client/supervisor/clientset/versioned/fake"
	pinnipedinformers "go.pinniped.dev/generated/1.19/client/supervisor/informers/externalversions"
	"go.pinniped.dev/internal/controllerlib"
	"go.pinniped.dev/internal/oidc/provider"
	"go.pinniped.dev/internal/testutil/testlogger"
)

func TestController(t *testing.T) {
	t.Parallel()
	now := metav1.NewTime(time.Now().UTC())
	earlier := metav1.NewTime(now.Add(-1 * time.Hour).UTC())

	// Start another test server that answers discovery successfully.
	testIssuer := newTestIssuer(t)
	testIssuerAuthorizeURL, err := url.Parse("https://example.com/authorize")
	require.NoError(t, err)

	var (
		testNamespace        = "test-namespace"
		testName             = "test-name"
		testSecretName       = "test-client-secret"
		testAdditionalScopes = []string{"scope1", "scope2", "scope3"}
		testExpectedScopes   = []string{"openid", "scope1", "scope2", "scope3"}
		testClientID         = "test-oidc-client-id"
		testClientSecret     = "test-oidc-client-secret"
		testValidSecretData  = map[string][]byte{"clientID": []byte(testClientID), "clientSecret": []byte(testClientSecret)}
	)
	tests := []struct {
		name                   string
		inputUpstreams         []runtime.Object
		inputSecrets           []runtime.Object
		wantErr                string
		wantLogs               []string
		wantResultingCache     []provider.UpstreamOIDCIdentityProvider
		wantResultingUpstreams []v1alpha1.UpstreamOIDCProvider
	}{
		{
			name: "no upstreams",
		},
		{
			name: "missing secret",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL,
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{},
			wantErr:      controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="secret \"test-client-secret\" not found" "reason"="SecretNotFound" "status"="False" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="discovered issuer configuration" "reason"="Success" "status"="True" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="secret \"test-client-secret\" not found" "name"="test-name" "namespace"="test-namespace" "reason"="SecretNotFound" "type"="ClientCredentialsValid"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "SecretNotFound",
							Message:            `secret "test-client-secret" not found`,
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "discovered issuer configuration",
						},
					},
				},
			}},
		},
		{
			name: "secret has wrong type",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL,
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "some-other-type",
				Data:       testValidSecretData,
			}},
			wantErr: controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="referenced Secret \"test-client-secret\" has wrong type \"some-other-type\" (should be \"secrets.pinniped.dev/oidc-client\")" "reason"="SecretWrongType" "status"="False" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="discovered issuer configuration" "reason"="Success" "status"="True" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="referenced Secret \"test-client-secret\" has wrong type \"some-other-type\" (should be \"secrets.pinniped.dev/oidc-client\")" "name"="test-name" "namespace"="test-namespace" "reason"="SecretWrongType" "type"="ClientCredentialsValid"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "SecretWrongType",
							Message:            `referenced Secret "test-client-secret" has wrong type "some-other-type" (should be "secrets.pinniped.dev/oidc-client")`,
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "discovered issuer configuration",
						},
					},
				},
			}},
		},
		{
			name: "secret is missing key",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL,
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
			}},
			wantErr: controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="referenced Secret \"test-client-secret\" is missing required keys [\"clientID\" \"clientSecret\"]" "reason"="SecretMissingKeys" "status"="False" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="discovered issuer configuration" "reason"="Success" "status"="True" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="referenced Secret \"test-client-secret\" is missing required keys [\"clientID\" \"clientSecret\"]" "name"="test-name" "namespace"="test-namespace" "reason"="SecretMissingKeys" "type"="ClientCredentialsValid"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "SecretMissingKeys",
							Message:            `referenced Secret "test-client-secret" is missing required keys ["clientID" "clientSecret"]`,
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "discovered issuer configuration",
						},
					},
				},
			}},
		},
		{
			name: "issuer is invalid URL",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              "invalid-url",
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
				Data:       testValidSecretData,
			}},
			wantErr: controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="loaded client credentials" "reason"="Success" "status"="True" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="failed to perform OIDC discovery against \"invalid-url\": Get \"invalid-url/.well-known/openid-configuration\": unsupported protocol scheme \"\"" "reason"="Unreachable" "status"="False" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="failed to perform OIDC discovery against \"invalid-url\": Get \"invalid-url/.well-known/openid-configuration\": unsupported protocol scheme \"\"" "name"="test-name" "namespace"="test-namespace" "reason"="Unreachable" "type"="OIDCDiscoverySucceeded"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "loaded client credentials",
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "Unreachable",
							Message:            `failed to perform OIDC discovery against "invalid-url": Get "invalid-url/.well-known/openid-configuration": unsupported protocol scheme ""`,
						},
					},
				},
			}},
		},
		{
			name: "issuer returns invalid authorize URL",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL + "/invalid",
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
				Data:       testValidSecretData,
			}},
			wantErr: controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="loaded client credentials" "reason"="Success" "status"="True" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="failed to parse authorization endpoint URL: parse \"%\": invalid URL escape \"%\"" "reason"="InvalidResponse" "status"="False" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="failed to parse authorization endpoint URL: parse \"%\": invalid URL escape \"%\"" "name"="test-name" "namespace"="test-namespace" "reason"="InvalidResponse" "type"="OIDCDiscoverySucceeded"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "loaded client credentials",
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "InvalidResponse",
							Message:            `failed to parse authorization endpoint URL: parse "%": invalid URL escape "%"`,
						},
					},
				},
			}},
		},
		{
			name: "issuer returns insecure authorize URL",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL + "/insecure",
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
				Data:       testValidSecretData,
			}},
			wantErr: controllerlib.ErrSyntheticRequeue.Error(),
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="loaded client credentials" "reason"="Success" "status"="True" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="authorization endpoint URL scheme must be \"https\", not \"http\"" "reason"="InvalidResponse" "status"="False" "type"="OIDCDiscoverySucceeded"`,
				`upstream-observer "error"="UpstreamOIDCProvider has a failing condition" "msg"="found failing condition" "message"="authorization endpoint URL scheme must be \"https\", not \"http\"" "name"="test-name" "namespace"="test-namespace" "reason"="InvalidResponse" "type"="OIDCDiscoverySucceeded"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{
							Type:               "ClientCredentialsValid",
							Status:             "True",
							LastTransitionTime: now,
							Reason:             "Success",
							Message:            "loaded client credentials",
						},
						{
							Type:               "OIDCDiscoverySucceeded",
							Status:             "False",
							LastTransitionTime: now,
							Reason:             "InvalidResponse",
							Message:            `authorization endpoint URL scheme must be "https", not "http"`,
						},
					},
				},
			}},
		},
		{
			name: "upstream becomes valid",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: "test-name"},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL,
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: append(testAdditionalScopes, "xyz", "openid")},
				},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Error",
					Conditions: []v1alpha1.Condition{
						{Type: "ClientCredentialsValid", Status: "False", LastTransitionTime: earlier, Reason: "SomeError1", Message: "some previous error 1"},
						{Type: "OIDCDiscoverySucceeded", Status: "False", LastTransitionTime: earlier, Reason: "SomeError2", Message: "some previous error 2"},
					},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
				Data:       testValidSecretData,
			}},
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="loaded client credentials" "reason"="Success" "status"="True" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="discovered issuer configuration" "reason"="Success" "status"="True" "type"="OIDCDiscoverySucceeded"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{{
				Name:             testName,
				ClientID:         testClientID,
				AuthorizationURL: *testIssuerAuthorizeURL,
				Scopes:           append(testExpectedScopes, "xyz"),
			}},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Ready",
					Conditions: []v1alpha1.Condition{
						{Type: "ClientCredentialsValid", Status: "True", LastTransitionTime: now, Reason: "Success", Message: "loaded client credentials"},
						{Type: "OIDCDiscoverySucceeded", Status: "True", LastTransitionTime: now, Reason: "Success", Message: "discovered issuer configuration"},
					},
				},
			}},
		},
		{
			name: "existing valid upstream",
			inputUpstreams: []runtime.Object{&v1alpha1.UpstreamOIDCProvider{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName, Generation: 1234},
				Spec: v1alpha1.UpstreamOIDCProviderSpec{
					Issuer:              testIssuer.URL,
					Client:              v1alpha1.OIDCClient{SecretName: testSecretName},
					AuthorizationConfig: v1alpha1.OIDCAuthorizationConfig{AdditionalScopes: testAdditionalScopes},
				},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Ready",
					Conditions: []v1alpha1.Condition{
						{Type: "ClientCredentialsValid", Status: "True", LastTransitionTime: earlier, Reason: "Success", Message: "loaded client credentials"},
						{Type: "OIDCDiscoverySucceeded", Status: "True", LastTransitionTime: earlier, Reason: "Success", Message: "discovered issuer configuration"},
					},
				},
			}},
			inputSecrets: []runtime.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testSecretName},
				Type:       "secrets.pinniped.dev/oidc-client",
				Data:       testValidSecretData,
			}},
			wantLogs: []string{
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="loaded client credentials" "reason"="Success" "status"="True" "type"="ClientCredentialsValid"`,
				`upstream-observer "level"=0 "msg"="updated condition" "name"="test-name" "namespace"="test-namespace" "message"="discovered issuer configuration" "reason"="Success" "status"="True" "type"="OIDCDiscoverySucceeded"`,
			},
			wantResultingCache: []provider.UpstreamOIDCIdentityProvider{{
				Name:             testName,
				ClientID:         testClientID,
				AuthorizationURL: *testIssuerAuthorizeURL,
				Scopes:           testExpectedScopes,
			}},
			wantResultingUpstreams: []v1alpha1.UpstreamOIDCProvider{{
				ObjectMeta: metav1.ObjectMeta{Namespace: testNamespace, Name: testName, Generation: 1234},
				Status: v1alpha1.UpstreamOIDCProviderStatus{
					Phase: "Ready",
					Conditions: []v1alpha1.Condition{
						{Type: "ClientCredentialsValid", Status: "True", LastTransitionTime: earlier, Reason: "Success", Message: "loaded client credentials", ObservedGeneration: 1234},
						{Type: "OIDCDiscoverySucceeded", Status: "True", LastTransitionTime: earlier, Reason: "Success", Message: "discovered issuer configuration", ObservedGeneration: 1234},
					},
				},
			}},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fakePinnipedClient := pinnipedfake.NewSimpleClientset(tt.inputUpstreams...)
			pinnipedInformers := pinnipedinformers.NewSharedInformerFactory(fakePinnipedClient, 0)
			fakeKubeClient := fake.NewSimpleClientset(tt.inputSecrets...)
			kubeInformers := informers.NewSharedInformerFactory(fakeKubeClient, 0)
			testLog := testlogger.New(t)
			cache := provider.NewDynamicUpstreamIDPProvider()
			cache.SetIDPList([]provider.UpstreamOIDCIdentityProvider{{Name: "initial-entry"}})

			controller := New(
				cache,
				fakePinnipedClient,
				pinnipedInformers.IDP().V1alpha1().UpstreamOIDCProviders(),
				kubeInformers.Core().V1().Secrets(),
				testLog)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			pinnipedInformers.Start(ctx.Done())
			kubeInformers.Start(ctx.Done())
			controllerlib.TestRunSynchronously(t, controller)

			syncCtx := controllerlib.Context{Context: ctx, Key: controllerlib.Key{}}

			if err := controllerlib.TestSync(t, controller, syncCtx); tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, strings.Join(tt.wantLogs, "\n"), strings.Join(testLog.Lines(), "\n"))
			require.ElementsMatch(t, tt.wantResultingCache, cache.GetIDPList())

			actualUpstreams, err := fakePinnipedClient.IDPV1alpha1().UpstreamOIDCProviders(testNamespace).List(ctx, metav1.ListOptions{})
			require.NoError(t, err)

			// Preprocess the set of upstreams a bit so that they're easier to assert against.
			require.ElementsMatch(t, tt.wantResultingUpstreams, normalizeUpstreams(actualUpstreams.Items, now))

			// Running the sync() a second time should be idempotent except for logs, and should return the same error.
			// This also helps exercise code paths where the OIDC provider discovery hits cache.
			if err := controllerlib.TestSync(t, controller, syncCtx); tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func normalizeUpstreams(upstreams []v1alpha1.UpstreamOIDCProvider, now metav1.Time) []v1alpha1.UpstreamOIDCProvider {
	result := make([]v1alpha1.UpstreamOIDCProvider, 0, len(upstreams))
	for _, u := range upstreams {
		normalized := u.DeepCopy()

		// We're only interested in comparing the status, so zero out the spec.
		normalized.Spec = v1alpha1.UpstreamOIDCProviderSpec{}

		// Round down the LastTransitionTime values to `now` if they were just updated. This makes
		// it much easier to encode assertions about the expected timestamps.
		for i := range normalized.Status.Conditions {
			if time.Since(normalized.Status.Conditions[i].LastTransitionTime.Time) < 5*time.Second {
				normalized.Status.Conditions[i].LastTransitionTime = now
			}
		}
		result = append(result, *normalized)
	}

	return result
}

func newTestIssuer(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	testServer := httptest.NewServer(mux)
	t.Cleanup(testServer.Close)

	type providerJSON struct {
		Issuer   string `json:"issuer"`
		AuthURL  string `json:"authorization_endpoint"`
		TokenURL string `json:"token_endpoint"`
		JWKSURL  string `json:"jwks_uri"`
	}

	// At the root of the server, serve an issuer with a valid discovery response.
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(&providerJSON{
			Issuer:  testServer.URL,
			AuthURL: "https://example.com/authorize",
		})
	})

	// At "/invalid", serve an issuer that returns an invalid authorization URL (not parseable).
	mux.HandleFunc("/invalid/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(&providerJSON{
			Issuer:  testServer.URL + "/invalid",
			AuthURL: "%",
		})
	})

	// At "/insecure", serve an issuer that returns an insecure authorization URL (not https://).
	mux.HandleFunc("/insecure/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(&providerJSON{
			Issuer:  testServer.URL + "/insecure",
			AuthURL: "http://example.com/authorize",
		})
	})

	return testServer
}
