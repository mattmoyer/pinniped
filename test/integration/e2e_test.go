// Copyright 2020 the Pinniped contributors. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	authv1alpha "go.pinniped.dev/generated/1.19/apis/concierge/authentication/v1alpha1"
	configv1alpha1 "go.pinniped.dev/generated/1.19/apis/supervisor/config/v1alpha1"
	idpv1alpha1 "go.pinniped.dev/generated/1.19/apis/supervisor/idp/v1alpha1"
	"go.pinniped.dev/internal/certauthority"
	"go.pinniped.dev/internal/testutil"
	"go.pinniped.dev/test/library"
	"go.pinniped.dev/test/library/browsertest"
)

// TestE2EFullIntegration tests a full integration scenario that combines the supervisor, concierge, and CLI.
func TestE2EFullIntegration(t *testing.T) {
	env := library.IntegrationEnv(t).WithCapability(library.ClusterSigningKeyIsAvailable)

	// If anything in this test crashes, dump out the supervisor and proxy pod logs.
	defer library.DumpLogs(t, env.SupervisorNamespace, "")
	defer library.DumpLogs(t, "dex", "app=proxy")

	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancelFunc()

	// Build pinniped CLI.
	pinnipedExe := library.PinnipedCLIPath(t)
	tempDir := testutil.TempDir(t)

	// Start the browser driver.
	page := browsertest.Open(t)

	// Infer the downstream issuer URL from the callback associated with the upstream test client registration.
	issuerURL, err := url.Parse(env.SupervisorTestUpstream.CallbackURL)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(issuerURL.Path, "/callback"))
	issuerURL.Path = strings.TrimSuffix(issuerURL.Path, "/callback")
	t.Logf("testing with downstream issuer URL %s", issuerURL.String())

	// Generate a CA bundle with which to serve this provider.
	t.Logf("generating test CA")
	ca, err := certauthority.New(pkix.Name{CommonName: "Downstream Test CA"}, 1*time.Hour)
	require.NoError(t, err)

	// Save that bundle plus the one that signs the upstream issuer, for test purposes.
	testCABundlePath := filepath.Join(tempDir, "test-ca.pem")
	testCABundlePEM := []byte(string(ca.Bundle()) + "\n" + env.SupervisorTestUpstream.CABundle)
	testCABundleBase64 := base64.StdEncoding.EncodeToString(testCABundlePEM)
	require.NoError(t, ioutil.WriteFile(testCABundlePath, testCABundlePEM, 0600))

	// Use the CA to issue a TLS server cert.
	t.Logf("issuing test certificate")
	tlsCert, err := ca.Issue(
		pkix.Name{CommonName: issuerURL.Hostname()},
		[]string{issuerURL.Hostname()},
		nil,
		1*time.Hour,
	)
	require.NoError(t, err)
	certPEM, keyPEM, err := certauthority.ToPEM(tlsCert)
	require.NoError(t, err)

	// Write the serving cert to a secret.
	certSecret := library.CreateTestSecret(t,
		env.SupervisorNamespace,
		"oidc-provider-tls",
		"kubernetes.io/tls",
		map[string]string{"tls.crt": string(certPEM), "tls.key": string(keyPEM)},
	)

	// Create the downstream FederationDomain and expect it to go into the success status condition.
	downstream := library.CreateTestFederationDomain(ctx, t,
		issuerURL.String(),
		certSecret.Name,
		configv1alpha1.SuccessFederationDomainStatusCondition,
	)

	// Create upstream OIDC provider and wait for it to become ready.
	library.CreateTestOIDCIdentityProvider(t, idpv1alpha1.OIDCIdentityProviderSpec{
		Issuer: env.SupervisorTestUpstream.Issuer,
		TLS: &idpv1alpha1.TLSSpec{
			CertificateAuthorityData: base64.StdEncoding.EncodeToString([]byte(env.SupervisorTestUpstream.CABundle)),
		},
		AuthorizationConfig: idpv1alpha1.OIDCAuthorizationConfig{
			AdditionalScopes: []string{"email"},
		},
		Claims: idpv1alpha1.OIDCClaims{
			Username: "email",
		},
		Client: idpv1alpha1.OIDCClient{
			SecretName: library.CreateClientCredsSecret(t, env.SupervisorTestUpstream.ClientID, env.SupervisorTestUpstream.ClientSecret).Name,
		},
	}, idpv1alpha1.PhaseReady)

	// Create a JWTAuthenticator that will validate the tokens from the downstream issuer.
	clusterAudience := "test-cluster-" + library.RandHex(t, 8)
	authenticator := library.CreateTestJWTAuthenticator(ctx, t, authv1alpha.JWTAuthenticatorSpec{
		Issuer:   downstream.Spec.Issuer,
		Audience: clusterAudience,
		TLS:      &authv1alpha.TLSSpec{CertificateAuthorityData: testCABundleBase64},
	})

	// Create a ClusterRoleBinding to give our test user from the upstream read-only access to the cluster.
	library.CreateTestClusterRoleBinding(t,
		rbacv1.Subject{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: env.SupervisorTestUpstream.Username},
		rbacv1.RoleRef{Kind: "ClusterRole", APIGroup: rbacv1.GroupName, Name: "view"},
	)

	// Use a specific session cache for this test.
	sessionCachePath := tempDir + "/sessions.yaml"

	// Run "pinniped get kubeconfig" to get a kubeconfig YAML.
	kubeconfigYAML, stderr := runPinnipedCLI(t, pinnipedExe, "get", "kubeconfig",
		"--concierge-namespace", env.ConciergeNamespace,
		"--concierge-authenticator-type", "jwt",
		"--concierge-authenticator-name", authenticator.Name,
		"--oidc-skip-browser",
		"--oidc-ca-bundle", testCABundlePath,
		"--oidc-session-cache", sessionCachePath,
	)
	require.Equal(t, "", stderr)

	restConfig := library.NewRestConfigFromKubeconfig(t, kubeconfigYAML)
	require.NotNil(t, restConfig.ExecProvider)
	require.Equal(t, []string{"login", "oidc"}, restConfig.ExecProvider.Args[:2])
	kubeconfigPath := filepath.Join(tempDir, "kubeconfig.yaml")
	require.NoError(t, ioutil.WriteFile(kubeconfigPath, []byte(kubeconfigYAML), 0600))

	// Wait 10 seconds for the JWTAuthenticator to become initialized.
	// TODO: remove this sleep once we have fixed the initialization problem.
	t.Log("sleeping 10s to wait for JWTAuthenticator to become initialized")
	time.Sleep(10 * time.Second)

	// Run "kubectl get namespaces" which should trigger a browser login via the plugin.
	start := time.Now()
	kubectlCmd := exec.CommandContext(ctx, "kubectl", "get", "namespace", "--kubeconfig", kubeconfigPath)
	kubectlCmd.Env = append(os.Environ(), env.ProxyEnv()...)
	stderrPipe, err := kubectlCmd.StderrPipe()
	require.NoError(t, err)
	stdoutPipe, err := kubectlCmd.StdoutPipe()
	require.NoError(t, err)

	t.Logf("starting kubectl subprocess")
	require.NoError(t, kubectlCmd.Start())
	t.Cleanup(func() {
		err := kubectlCmd.Wait()
		t.Logf("kubectl subprocess exited with code %d", kubectlCmd.ProcessState.ExitCode())
		stdout, stdoutErr := ioutil.ReadAll(stdoutPipe)
		if stdoutErr != nil {
			stdout = []byte("<error reading stdout: " + stdoutErr.Error() + ">")
		}
		stderr, stderrErr := ioutil.ReadAll(stderrPipe)
		if stderrErr != nil {
			stderr = []byte("<error reading stderr: " + stderrErr.Error() + ">")
		}
		require.NoErrorf(t, err, "kubectl process did not exit cleanly, stdout/stderr: %q/%q", string(stdout), string(stderr))
	})

	// Start a background goroutine to read stderr from the CLI and parse out the login URL.
	loginURLChan := make(chan string)
	spawnTestGoroutine(t, func() (err error) {
		defer func() {
			closeErr := stderrPipe.Close()
			if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
				return
			}
			if err == nil {
				err = fmt.Errorf("stderr stream closed with error: %w", closeErr)
			}
		}()

		reader := bufio.NewReader(library.NewLoggerReader(t, "stderr", stderrPipe))
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("could not read login URL line from stderr: %w", err)
		}
		const prompt = "Please log in: "
		if !strings.HasPrefix(line, prompt) {
			return fmt.Errorf("expected %q to have prefix %q", line, prompt)
		}
		loginURLChan <- strings.TrimPrefix(line, prompt)
		return readAndExpectEmpty(reader)
	})

	// Start a background goroutine to read stdout from kubectl and return the result as a string.
	kubectlOutputChan := make(chan string)
	spawnTestGoroutine(t, func() (err error) {
		defer func() {
			closeErr := stdoutPipe.Close()
			if closeErr == nil || errors.Is(closeErr, os.ErrClosed) {
				return
			}
			if err == nil {
				err = fmt.Errorf("stdout stream closed with error: %w", closeErr)
			}
		}()
		output, err := ioutil.ReadAll(stdoutPipe)
		if err != nil {
			return err
		}
		t.Logf("kubectl output:\n%s\n", output)
		kubectlOutputChan <- string(output)
		return nil
	})

	// Wait for the CLI to print out the login URL and open the browser to it.
	t.Logf("waiting for CLI to output login URL")
	var loginURL string
	select {
	case <-time.After(1 * time.Minute):
		require.Fail(t, "timed out waiting for login URL")
	case loginURL = <-loginURLChan:
	}
	t.Logf("navigating to login page")
	require.NoError(t, page.Navigate(loginURL))

	// Expect to be redirected to the upstream provider and log in.
	browsertest.LoginToUpstream(t, page, env.SupervisorTestUpstream)

	// Expect to be redirected to the localhost callback.
	t.Logf("waiting for redirect to callback")
	browsertest.WaitForURL(t, page, regexp.MustCompile(`\Ahttp://127\.0\.0\.1:[0-9]+/callback\?.+\z`))

	// Wait for the "pre" element that gets rendered for a `text/plain` page, and
	// assert that it contains the success message.
	t.Logf("verifying success page")
	browsertest.WaitForVisibleElements(t, page, "pre")
	msg, err := page.First("pre").Text()
	require.NoError(t, err)
	require.Equal(t, "you have been logged in and may now close this tab", msg)

	// Expect the CLI to output a list of namespaces in JSON format.
	t.Logf("waiting for kubectl to output namespace list JSON")
	var kubectlOutput string
	select {
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for kubectl output")
	case kubectlOutput = <-kubectlOutputChan:
	}
	require.Greaterf(t, len(strings.Split(kubectlOutput, "\n")), 2, "expected some namespaces to be returned, got %q", kubectlOutput)
	t.Logf("first kubectl command took %s", time.Since(start).String())

	// 	Run kubectl again, which should work with no browser interaction.
	kubectlCmd2 := exec.CommandContext(ctx, "kubectl", "get", "namespace", "--kubeconfig", kubeconfigPath)
	kubectlCmd2.Env = append(os.Environ(), env.ProxyEnv()...)
	start = time.Now()
	kubectlOutput2, err := kubectlCmd2.CombinedOutput()
	require.NoError(t, err)
	require.Greaterf(t, len(bytes.Split(kubectlOutput2, []byte("\n"))), 2, "expected some namespaces to be returned again")
	t.Logf("second kubectl command took %s", time.Since(start).String())
}