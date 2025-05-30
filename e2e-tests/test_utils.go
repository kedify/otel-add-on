package e2e_tests

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	k3dVersion                 = "v5.8.3"
	podinfoVesrsion            = "v6.9.0"
	kedifyKedaHelmChartVersion = "v2.17.1-0"
	defaultTimeoutSec          = 300
	suiteOp                    = "operator"
	suitePi                    = "podinfo"
)

type EnvVar struct {
	name  string
	value string
}

var (
	ctx TestContext
	// repoName -> url mapping
	helmChartRepos = map[string]string{
		"podinfo":                 "https://stefanprodan.github.io/podinfo",
		"kedify":                  "https://kedify.github.io/charts",
		"kedify-otel":             "https://kedify.github.io/otel-add-on",
		"opentelemetry-collector": "https://open-telemetry.github.io/opentelemetry-helm-charts",
		"opentelemetry-operator":  "https://open-telemetry.github.io/opentelemetry-helm-charts",
	}

	// repoName -> helmChartName mapping
	helmChartNames = map[string]string{
		"podinfo":     "podinfo",
		"kedify":      "keda",
		"kedify-otel": "otel-add-on",
	}
	isCI, _              = os.LookupEnv("CI")
	prBranch, _          = os.LookupEnv("PR_BRANCH")
	ghPat, _             = os.LookupEnv("GH_PAT")
	only, _              = os.LookupEnv("ONLY")
	deleteCluster, _     = os.LookupEnv("E2E_DELETE_CLUSTER")
	printLogs, _         = os.LookupEnv("E2E_PRINT_LOGS")
	otelScalerVersion, _ = os.LookupEnv("OTEL_SCALER_VERSION")
)

type TestContext struct {
	t          *testing.T
	k8sClient  *kubernetes.Clientset
	kedaClient *v1alpha1.KedaV1alpha1Client
	k8sConfig  *rest.Config
	hey        string
}

func kubectl(args string) (string, error) {
	return execCmdOE("kubectl "+args, "")
}

func kapply(path string) (string, error) {
	return kubectl("apply -f" + path)
}

func execCmdOE(cmdWithArgs string, workDir string, envVars ...EnvVar) (string, error) {
	cmd := parseCommand(cmdWithArgs, workDir, envVars...)
	return shell.RunCommandAndGetOutputE(ctx.t, cmd)
}

func execBashCmdOE(cmdWithArgs string, workDir string, envVars ...EnvVar) (string, error) {
	envVarsMap := make(map[string]string, len(envVars))
	if len(envVars) > 0 {
		for _, envVar := range envVars {
			envVarsMap[envVar.name] = envVar.value
		}
	}

	cmd := shell.Command{
		Command: "bash",
		Args:    []string{"-c", cmdWithArgs},
		Env:     envVarsMap,
	}
	if len(workDir) > 0 {
		cmd.WorkingDir = workDir
	}
	return shell.RunCommandAndGetOutputE(ctx.t, cmd)
}

func execCmd(cmdWithArgs string, envVars ...EnvVar) {
	execCmdOE(cmdWithArgs, "", envVars...)
}

func execCmdE(cmdWithArgs string, envVars ...EnvVar) error {
	_, e := execCmdOE(cmdWithArgs, "", envVars...)
	return e
}

func hey(params string) error {
	return execCmdE(fmt.Sprintf("%s %s", ctx.hey, params))
}

func parseCommand(cmdWithArgs string, workDir string, envVars ...EnvVar) shell.Command {
	quoted := false
	splitCmd := strings.FieldsFunc(cmdWithArgs, func(r rune) bool {
		if r == '\'' {
			quoted = !quoted
		}
		return !quoted && r == ' '
	})
	for i, s := range splitCmd {
		if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") {
			splitCmd[i] = s[1 : len(s)-1]
		}
	}
	envVarsMap := make(map[string]string, len(envVars))
	if len(envVars) > 0 {
		for _, envVar := range envVars {
			envVarsMap[envVar.name] = envVar.value
		}
	}

	cmd := shell.Command{
		Command: splitCmd[0],
		Args:    splitCmd[1:],
		Env:     envVarsMap,
	}
	if len(workDir) > 0 {
		cmd.WorkingDir = workDir
	}
	return cmd
}

func getClients() (*kubernetes.Clientset, *v1alpha1.KedaV1alpha1Client, *rest.Config, error) {
	if ctx.k8sClient != nil && ctx.kedaClient != nil && ctx.k8sConfig != nil {
		return ctx.k8sClient, ctx.kedaClient, ctx.k8sConfig, nil
	}
	var err error
	if ctx.k8sConfig, err = config.GetConfig(); err != nil {
		return nil, nil, nil, err
	}
	if ctx.k8sClient, err = kubernetes.NewForConfig(ctx.k8sConfig); err != nil {
		return nil, nil, nil, err
	}
	if ctx.kedaClient, err = v1alpha1.NewForConfig(ctx.k8sConfig); err != nil {
		return nil, nil, nil, err
	}
	return ctx.k8sClient, ctx.kedaClient, ctx.k8sConfig, nil
}

func addHelmRepo(repoName string, repoUrl string) error {
	return execCmdE(fmt.Sprintf("helm repo add %s %s", repoName, repoUrl))
}

func helmRepoUpdate(repoName string) error {
	return execCmdE(fmt.Sprintf("helm repo update %s", repoName))
}

func helmChartInstall(repoName string, params string) error {
	return execCmdE(fmt.Sprintf("helm upgrade -i %s %s/%s %s", helmChartNames[repoName], repoName, helmChartNames[repoName], params))
}

func installHelmCli() error {
	_, err := exec.LookPath("helm")
	if err != nil {
		err = execCmdE("curl -fsSL -o ./bin/get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3")
		require.NoErrorf(ctx.t, err, "cannot download helm installation shell script - %s", err)

		err = execCmdE("chmod 700 ./bin/get_helm.sh")
		require.NoErrorf(ctx.t, err, "cannot change permissions for helm installation script - %s", err)

		err = execCmdE("./bin/get_helm.sh")
		require.NoErrorf(ctx.t, err, "cannot download helm - %s", err)
	}
	err = execCmdE("helm version")
	return err
}

func installK3d() error {
	_, err := exec.LookPath("k3d")
	if err != nil {
		err = execCmdE("curl -fsSL -o ./bin/get_k3d.sh https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh")
		require.NoErrorf(ctx.t, err, "cannot download k3d installation shell script: %s", err)

		err = execCmdE("chmod 700 ./bin/get_k3d.sh")
		require.NoErrorf(ctx.t, err, "cannot change permissions for k3d installation script: %s", err)

		err = execCmdE("./bin/get_k3d.sh", EnvVar{name: "TAG", value: k3dVersion})
		require.NoErrorf(ctx.t, err, "cannot download k3d - %s", err)
	}
	err = execCmdE("k3d version")
	return err
}

func installHey() error {
	ctx.hey = "hey"
	_, err := exec.LookPath(ctx.hey)
	if err != nil {
		ctx.hey = "./bin/hey"
		_, err := exec.LookPath(ctx.hey)
		if err != nil {
			url := fmt.Sprintf("https://hey-release.s3.us-east-2.amazonaws.com/hey_%s_amd64", runtime.GOOS)
			err = execCmdE("curl -fsSL -o ./bin/hey " + url)
			require.NoErrorf(ctx.t, err, "cannot download hey binary: %s", err)

			err = execCmdE("chmod 700 ./bin/hey")
			require.NoErrorf(ctx.t, err, "cannot change permissions for hey binary: %s", err)
		}
	}
	cmd := parseCommand(ctx.hey+" -n 1 -c 1 http://google.com", "")
	cmd.Logger = logger.Discard
	err = shell.RunCommandE(ctx.t, cmd)

	require.NoErrorf(ctx.t, err, "cannot change permissions for hey binary: %s", err)
	return err
}

func prepareCluster(name string, params string) error {
	ctx.t.Log("Creating k3d cluster..")
	out, err := execCmdOE(fmt.Sprintf("k3d cluster create %s %s", name, params), "")
	if err != nil {
		require.NoErrorf(ctx.t, err, "cannot create new k3d cluster: %s", out)
	}
	return err
}

func deploy(name string, namespace string, timeoutSec int) (string, error) {
	return kubectl(fmt.Sprintf("rollout status -n %s --timeout=%ds deploy/%s", namespace, timeoutSec, name))
}

func waitForDeployment(name string, namespace string, timeoutSec int) {
	GinkgoHelper()
	c := context.TODO()
	Eventually(func(g Gomega) {
		out, err := deploy(name, namespace, timeoutSec)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(out).To(ContainSubstring("successfully rolled out"))
	}).WithContext(c).WithTimeout(5 * time.Minute).WithPolling(20 * time.Second).Should(Succeed())
}

func getHpa(name, namespace string) error {
	_, err := kubectl(fmt.Sprintf("get hpa -n %s %s", namespace, name))
	return err
}

func skipIfNeeded(t *testing.T, name string) {
	if only != "" && only != name {
		t.Skip(fmt.Sprintf("Skipping test suite for %s", name))
	}
}
