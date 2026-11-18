package e2e_tests

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/kedacore/keda/v2/pkg/generated/clientset/versioned/typed/keda/v1alpha1"
)

//go:embed kubernetes-versions.json
var kubernetesImages []byte

const (
	k3dVersion                 = "v5.8.3"
	heyVersion                 = "v0.1.5"
	podinfoVesrsion            = "v6.9.0"
	kedifyKedaHelmChartVersion = "v2.17.1-0"
	defaultTimeoutSec          = 600
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
	helm       string
}

func kubectl(args string) (string, error) {
	return execCmdOE("kubectl "+args, "")
}

func kubectlStdout(args string) (string, error) {
	return shell.RunCommandAndGetStdOutE(ctx.t, parseCommand("kubectl "+args, ""))
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
	return execCmdE(fmt.Sprintf("%s repo add %s %s", ctx.helm, repoName, repoUrl))
}

func helmRepoUpdate(repoName string) error {
	return execCmdE(fmt.Sprintf("%s repo update %s", ctx.helm, repoName))
}

func helmChartInstall(repoName string, params string) error {
	return execCmdE(fmt.Sprintf("%s upgrade -i %s %s/%s %s", ctx.helm, helmChartNames[repoName], repoName, helmChartNames[repoName], params))
}

func installHelmCli() error {
	ctx.helm = "helm"
	_, err := exec.LookPath("helm")
	if err != nil || isCI == "true" {
		err = execCmdE("curl -fsSL -o ./bin/get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3")
		require.NoErrorf(ctx.t, err, "cannot download helm installation shell script - %s", err)

		err = execCmdE("chmod 700 ./bin/get_helm.sh")
		require.NoErrorf(ctx.t, err, "cannot change permissions for helm installation script - %s", err)

		// The installer puts helm on PATH, including when it upgrades an existing installation.
		err = execCmdE("./bin/get_helm.sh")
		require.NoErrorf(ctx.t, err, "cannot download helm - %s", err)
	}
	err = execCmdE(ctx.helm + " version")
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
			workDir, err := os.Getwd()
			require.NoErrorf(ctx.t, err, "cannot resolve working directory for hey install: %s", err)

			err = execCmdE(
				fmt.Sprintf("go install github.com/rakyll/hey@%s", heyVersion),
				EnvVar{name: "GOBIN", value: filepath.Join(workDir, "bin")},
			)
			require.NoErrorf(ctx.t, err, "cannot install hey with go install: %s", err)
		}
	}
	cmd := parseCommand(ctx.hey+" -n 1 -c 1 http://google.com", "")
	cmd.Logger = logger.Discard
	err = shell.RunCommandE(ctx.t, cmd)

	require.NoErrorf(ctx.t, err, "cannot validate hey binary: %s", err)
	return err
}

func prepareCluster(name string, params string) error {
	image, expectedVersion, err := configuredKubernetesImage()
	if err != nil {
		return err
	}
	ctx.t.Log("Creating k3d cluster..")
	out, err := execCmdOE(fmt.Sprintf("k3d cluster create %s %s --image %s --wait", name, params, image), "")
	if err != nil {
		require.NoErrorf(ctx.t, err, "cannot create new k3d cluster: %s", out)
		return err
	}
	if _, err = kubectl("wait --for=condition=Ready node --all --timeout=2m"); err != nil {
		return err
	}
	server, err := kubectlStdout("version -o json")
	if err != nil {
		return err
	}
	nodes, err := kubectlStdout("get nodes -o json")
	if err != nil {
		return err
	}
	return checkClusterVersions(expectedVersion, []byte(server), []byte(nodes))
}

func configuredKubernetesImage() (string, string, error) {
	var images []string
	if err := json.Unmarshal(kubernetesImages, &images); err != nil {
		return "", "", err
	}
	if len(images) != 1 {
		return "", "", fmt.Errorf("expected one Kubernetes image, got %d", len(images))
	}
	parts := regexp.MustCompile(`^rancher/k3s:(v1\.[0-9]+\.[0-9]+)-k3s([0-9]+)@sha256:[a-f0-9]{64}$`).FindStringSubmatch(images[0])
	if len(parts) != 3 {
		return "", "", fmt.Errorf("expected a pinned stable k3s image, got %q", images[0])
	}
	return images[0], parts[1] + "+k3s" + parts[2], nil
}

func checkClusterVersions(expected string, serverJSON, nodesJSON []byte) error {
	var server struct {
		ServerVersion struct{ GitVersion string }
	}
	if err := json.Unmarshal(serverJSON, &server); err != nil {
		return err
	}
	if server.ServerVersion.GitVersion != expected {
		return fmt.Errorf("Kubernetes server is %q, expected %q", server.ServerVersion.GitVersion, expected)
	}
	var nodes struct {
		Items []struct {
			Metadata struct{ Name string }
			Status   struct {
				NodeInfo struct{ KubeletVersion string }
			}
		}
	}
	if err := json.Unmarshal(nodesJSON, &nodes); err != nil {
		return err
	}
	if len(nodes.Items) == 0 {
		return fmt.Errorf("cluster has no nodes")
	}
	for _, node := range nodes.Items {
		if node.Status.NodeInfo.KubeletVersion != expected {
			return fmt.Errorf("node %q is %q, expected %q", node.Metadata.Name, node.Status.NodeInfo.KubeletVersion, expected)
		}
	}
	return nil
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
