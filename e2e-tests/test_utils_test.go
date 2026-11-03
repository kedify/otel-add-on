package e2e_tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallHelmCli(t *testing.T) {
	for _, tc := range []struct {
		name          string
		ci            string
		helmInstalled bool
		installOutput string
	}{
		{name: "existing local installation", helmInstalled: true},
		{name: "fresh installation", installOutput: "helm installed into /usr/local/bin/helm"},
		{name: "CI upgrade", ci: "true", helmInstalled: true, installOutput: "helm installed into /usr/local/bin/helm"},
		{name: "CI already latest", ci: "true", helmInstalled: true, installOutput: "Helm v3.22.1 is already latest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			originalCtx, originalCI := ctx, isCI
			ctx, isCI = TestContext{t: t}, tc.ci
			t.Cleanup(func() { ctx, isCI = originalCtx, originalCI })

			workDir := t.TempDir()
			originalDir, err := os.Getwd()
			require.NoError(t, err)
			require.NoError(t, os.Chdir(workDir))
			t.Cleanup(func() { require.NoError(t, os.Chdir(originalDir)) })
			require.NoError(t, os.Mkdir("bin", 0o755))

			pathBin := filepath.Join(workDir, "path-bin")
			require.NoError(t, os.Mkdir(pathBin, 0o755))
			t.Setenv("PATH", pathBin+string(os.PathListSeparator)+"/usr/bin:/bin")
			t.Setenv("TEST_HELM_BIN", pathBin)
			t.Setenv("TEST_HELM_INSTALL_OUTPUT", tc.installOutput)
			versionCalled := filepath.Join(workDir, "version-called")
			t.Setenv("TEST_HELM_VERSION_CALLED", versionCalled)

			helmScript := []byte("#!/bin/sh\n[ \"$1\" = version ] || exit 1\ntouch \"$TEST_HELM_VERSION_CALLED\"\n")
			helmSource := filepath.Join(workDir, "helm-source")
			require.NoError(t, os.WriteFile(helmSource, helmScript, 0o755))
			t.Setenv("TEST_HELM_SOURCE", helmSource)
			if tc.helmInstalled {
				require.NoError(t, os.WriteFile(filepath.Join(pathBin, "helm"), helmScript, 0o755))
			}

			installer := filepath.Join(workDir, "installer")
			require.NoError(t, os.WriteFile(installer, []byte(`#!/bin/sh
set -eu
cp "$TEST_HELM_SOURCE" "$TEST_HELM_BIN/helm"
printf '%s\n' "$TEST_HELM_INSTALL_OUTPUT"
`), 0o755))
			t.Setenv("TEST_HELM_INSTALLER", installer)
			require.NoError(t, os.WriteFile(filepath.Join(pathBin, "curl"), []byte(`#!/bin/sh
set -eu
touch download-called
cp "$TEST_HELM_INSTALLER" ./bin/get_helm.sh
`), 0o755))

			require.NoError(t, installHelmCli())
			require.FileExists(t, versionCalled, "the Helm binary on PATH must be executed")
			if tc.installOutput != "" {
				require.FileExists(t, "download-called")
			} else {
				require.NoFileExists(t, "download-called", "an existing local Helm should be reused")
			}
		})
	}
}

func TestCheckClusterVersions(t *testing.T) {
	const server = `{"serverVersion":{"gitVersion":"v1.37.1+k3s1"}}`
	const node = `{"metadata":{"name":"server-0"},"status":{"nodeInfo":{"kubeletVersion":"v1.37.1+k3s1"}}}`
	for _, tc := range []struct {
		name, server, nodes string
		wantError           bool
	}{
		{name: "matching versions", server: server, nodes: `{"items":[` + node + `]}`},
		{name: "wrong server", server: `{"serverVersion":{"gitVersion":"v1.36.4+k3s1"}}`, nodes: `{"items":[` + node + `]}`, wantError: true},
		{name: "one wrong node", server: server, nodes: `{"items":[` + node + `,{"metadata":{"name":"agent-0"},"status":{"nodeInfo":{"kubeletVersion":"v1.36.4+k3s1"}}}]}`, wantError: true},
		{name: "no nodes", server: server, nodes: `{"items":[]}`, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkClusterVersions("v1.37.1+k3s1", []byte(tc.server), []byte(tc.nodes))
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPrepareClusterWithKubectlWarnings(t *testing.T) {
	_, version, err := configuredKubernetesImage()
	require.NoError(t, err)
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("command failure=%t", fail), func(t *testing.T) {
			originalCtx := ctx
			ctx = TestContext{t: t}
			t.Cleanup(func() { ctx = originalCtx })

			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
			t.Setenv("TEST_KUBECTL_FAIL", fmt.Sprint(fail))
			t.Setenv("TEST_SERVER_JSON", fmt.Sprintf(`{"serverVersion":{"gitVersion":%q}}`, version))
			t.Setenv("TEST_NODES_JSON", fmt.Sprintf(`{"items":[{"status":{"nodeInfo":{"kubeletVersion":%q}}}]}`, version))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "k3d"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(bin, "kubectl"), []byte(`#!/bin/sh
set -eu
echo 'Warning: kubectl fixture warning' >&2
case "$*" in
  'wait --for=condition=Ready node --all --timeout=2m') ;;
  'version -o json')
    printf '%s\n' "$TEST_SERVER_JSON"
    [ "$TEST_KUBECTL_FAIL" = false ] || exit 42
    ;;
  'get nodes -o json') printf '%s\n' "$TEST_NODES_JSON" ;;
  *) exit 1 ;;
esac
`), 0o755))

			err := prepareCluster("warning-fixture", "")
			if fail {
				require.ErrorContains(t, err, "kubectl fixture warning")
			} else {
				require.NoError(t, err, "stderr warnings must not corrupt valid Kubernetes JSON")
			}
		})
	}
}
