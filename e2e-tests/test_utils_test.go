package e2e_tests

import (
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
