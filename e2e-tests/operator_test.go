package e2e_tests

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	opMaxReplicas = 3
)

func TestOperator(t *testing.T) {
	ctx = TestContext{
		t: t,
	}
	RegisterFailHandler(Fail)
}

var _ = BeforeEach(func() {
	getClients()
})

var _ = Describe("Helm chart (op):", func() {
	BeforeEach(func() {
		if only != "" && only != suiteOp {
			Skip("Skipping OTel operator tests")
		}
	})
	It("env var is set", func() {
		Expect(prBranch).NotTo(BeEmpty(), "PR_BRANCH should be set")
		Expect(ghPat).NotTo(BeEmpty(), "GH_PAT should be set")
	})
	for repoName, repoUrl := range helmChartRepos {
		Context(repoName, Ordered, func() {
			It("should be possible to add it", func() {
				err := addHelmRepo(repoName, repoUrl)
				Expect(err).To(SatisfyAny(
					Not(HaveOccurred()),
					Or(WithTransform(func(err error) string { return err.Error() }, ContainSubstring("already exists")))), "helm repo add failed: %s", err)
			})
			It("should be possible to update it", func() {
				err := helmRepoUpdate(repoName)
				Expect(err).NotTo(HaveOccurred(), "helm repo update failed: %s", err)
			})
		})
	}
	It("kedify/keda should be possible to install", func() {
		params := fmt.Sprintf("--version=%s --namespace keda --create-namespace --set webhooks.enabled=false", kedifyKedaHelmChartVersion)
		err := helmChartInstall("kedify", params)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade -i failed: %s", err)
	})
	It("keda-otel-scaler should be possible to install", func() {
		pwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		_, err = execCmdOE("helm dependency build", pwd+"/../helmchart/otel-add-on")
		Expect(err).NotTo(HaveOccurred())

		execCmdE("kubectl create ns keda")
		execCmdE(fmt.Sprintf("kubectl create secret -nkeda generic gh-token --from-literal=GH_PAT=%s", ghPat))
		time.Sleep(1 * time.Second)
		cmd := "helm upgrade -i keda-otel-scaler ../helmchart/otel-add-on --namespace keda --create-namespace -f ./testdata/scaler-operator-values.yaml"
		if len(otelScalerVersion) > 0 {
			cmd += fmt.Sprintf(" --set image.tag=%s", otelScalerVersion)
		}
		err = execCmdE(cmd)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade -i failed: %s", err)
	})
	When("OTel operator installed", func() {
		It("should become ready", func() {
			waitForDeployment("otel-operator", "keda", defaultTimeoutSec)
		})
	})
	When("keda installed", func() {
		It("should become ready", func() {
			waitForDeployment("keda-operator", "keda", defaultTimeoutSec)
			waitForDeployment("keda-operator-metrics-apiserver", "keda", defaultTimeoutSec)
		})
	})
	When("otel scaler installed", func() {
		It("should become ready", func() {
			waitForDeployment("keda-otel-scaler", "keda", defaultTimeoutSec)
		})
	})
	When("OTel collector installed", func() {
		It("should become ready", func() {
			waitForDeployment("otel-add-on-collector", "keda", defaultTimeoutSec)
		})
	})
	Context("Scaled Object", func() {
		When("is created", func() {
			It("should not fail", func() {
				// substitute the branch name in the SO and apply it
				_, err := execBashCmdOE("kubectl apply -f <(cat ./testdata/github-so.yaml | envsubst)", "")
				Expect(err).NotTo(HaveOccurred())
			})
			It("should eventually create HPA", func() {
				Eventually(func() error {
					return getHpa("keda-hpa-github-metrics", "keda")
				}).WithPolling(2 * time.Second).WithContext(context.TODO()).Should(Not(HaveOccurred()))
			})
			When("PR is opened for more than 2 minutes", func() {
				It("should eventually scale the otel-operator from 1 -> 3", func() {
					time.Sleep(1 * time.Second)
					ctx.t.Logf("        ->>>  Waiting for KEDA to scale the podinfo deployement        <<<-\n\n")
					ctx5min, _ := context.WithTimeout(context.TODO(), 5*time.Minute)
					Eventually(func(g Gomega) {
						out, err := kubectl("get hpa -nkeda keda-hpa-github-metrics -ojsonpath='{.status.desiredReplicas}'")
						g.Expect(err).Should(Not(HaveOccurred()))
						desiredReplicas, err := strconv.Atoi(strings.Trim(out, "'"))
						g.Expect(err).Should(Not(HaveOccurred()))
						g.Expect(desiredReplicas).Should(And(BeNumerically(">", minReplicas), BeNumerically("<=", opMaxReplicas)))
						ctx.t.Logf("\n        ->>>  otel operator successfuly scaled to %d        <<<-\n\n", desiredReplicas)
						GinkgoWriter.Println("        ->>>  otel operator successfuly scaled to")
					}).WithPolling(3 * time.Second).
						WithContext(ctx5min).
						Should(Succeed())
				})
			})
		})
	})
})

var _ = ReportAfterSuite("ReportAfterSuite", func(report Report) {
	if only != "" && only != suiteOp {
		Skip("Skipping for OTel operator")
	}
	if !report.SuiteSucceeded {
		ctx.t.Log("Test suite failed, leaving k3d cluster alive for inspection..")
		if printLogs == "true" {
			wrapInSection("HPA", "get -nkead hpa keda-hpa-github-metrics -oyaml")
			wrapInSection("SO", "get -nkeda so github-metrics -oyaml")
			wrapInSection("PODS", "get pods -A")
			for _, nameAndNs := range []string{
				"podinfo -ndefault",
				"keda-operator -nkeda",
				"otel-add-on-otc-collector -ndefault",
				"otelOperator -nkeda",
				"otel-add-on -nkeda"} {
				wrapInSection(fmt.Sprintf("Logs for %s", nameAndNs), fmt.Sprintf("logs -lapp.kubernetes.io/name=%s --tail=-1", nameAndNs))
			}
		}
	} else if deleteCluster == "true" {
		ctx.t.Log("Deleting k3d cluster..")
		err := execCmdE(fmt.Sprintf("k3d cluster delete %s", clusterName))
		Expect(err).NotTo(HaveOccurred())
	}
})
