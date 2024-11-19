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
	clusterName = "test-podinfo-cluster"
	minReplicas = 1
	maxReplicas = 20
)

func TestPodinfo(t *testing.T) {
	ctx = TestContext{
		t: t,
	}
	RegisterFailHandler(Fail)
	RunSpecs(t, "Podinfo test Suite")
}

var _ = BeforeSuite(func() {
	err := installHelmCli()
	Expect(err).NotTo(HaveOccurred())

	err = installK3d()
	Expect(err).NotTo(HaveOccurred())

	err = installHey()
	Expect(err).NotTo(HaveOccurred())

	execCmd(fmt.Sprintf("k3d cluster delete %s", clusterName))
	err = prepareCluster(clusterName, "-p 8181:31198@server:0")
	Expect(err).NotTo(HaveOccurred())
	time.Sleep(5 * time.Second)
})

var _ = BeforeEach(func() {
	getClients()
})

var _ = XDescribe("k8s access", func() {
	k8sCl, crdCl, crdRest, err := getClients()
	It("should be possible get the clients", func() {
		Expect(err).NotTo(HaveOccurred(), "cannot create k8s clients: %s", err)
		Expect(k8sCl).NotTo(BeNil())
		Expect(crdCl).NotTo(BeNil())
		Expect(crdRest).NotTo(BeNil())
	})
})

var _ = Describe("Helm chart", func() {
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
	It("podinfo should be possible to install", func() {
		params := fmt.Sprintf("--version=%s -f ./testdata/podinfo-values.yaml", podinfoVesrsion)
		err := helmChartInstall("podinfo", params)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade -i failed: %s", err)
	})
	It("kedify-otel should be possible to install", func() {
		pwd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		_, err = execCmdOE("helm dependency build", pwd+"/../helmchart/otel-add-on")
		Expect(err).NotTo(HaveOccurred())
		cmd := "helm upgrade -i kedify-otel ../helmchart/otel-add-on -f ./testdata/scaler-values.yaml"
		if len(otelScalerVersion) > 0 {
			cmd += fmt.Sprintf(" --set image.tag=%s", otelScalerVersion)
		}
		err = execCmdE(cmd)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade -i failed: %s", err)
	})
	It("kedify/keda should be possible to install", func() {
		params := fmt.Sprintf("--version=%s --namespace keda --create-namespace", kedifyKedaHelmChartVersion)
		err := helmChartInstall("kedify", params)
		Expect(err).NotTo(HaveOccurred(), "helm upgrade -i failed: %s", err)
	})
	When("podinfo installed", func() {
		It("should become ready", func() {
			waitForDeployment("podinfo", "default", defaultTimeoutSec)
		})
	})
	When("keda installed", func() {
		It("should become ready", func() {
			waitForDeployment("keda-operator", "keda", defaultTimeoutSec)
			waitForDeployment("keda-operator-metrics-apiserver", "keda", defaultTimeoutSec)
		})
	})
	When("kedify-otel installed", func() {
		It("should become ready", func() {
			waitForDeployment("otel-add-on-scaler", "default", defaultTimeoutSec)
		})
	})
	Context("Scaled Object", func() {
		When("is created", func() {
			It("should not fail", func() {
				_, err := kapply("./testdata/podinfo-so.yaml")
				Expect(err).NotTo(HaveOccurred())
			})
			It("should eventually create HPA", func() {
				Eventually(func() error {
					return getHpa("keda-hpa-podinfo-pull-example", "default")
				}).WithPolling(2 * time.Second).WithContext(context.TODO()).Should(Not(HaveOccurred()))
			})
			When("and traffic hits the workload", func() {
				It("should eventually scale the podinfo from 1 -> N", func() {
					cancelHey := make(chan bool)
					go func() {
						for {
							select {
							case <-cancelHey:
								return
							default:
								hey("-n 9999 -z 35s http://localhost:8181/delay/1")
							}
						}
					}()
					time.Sleep(1 * time.Second)
					ctx.t.Logf("        ->>>  Waiting for KEDA to scale the podinfo deployement        <<<-\n\n")
					Eventually(func(g Gomega) {
						out, err := kubectl("get hpa keda-hpa-podinfo-pull-example -ojsonpath='{.status.desiredReplicas}'")
						g.Expect(err).Should(Not(HaveOccurred()))
						desiredReplicas, err := strconv.Atoi(strings.Trim(out, "'"))
						g.Expect(err).Should(Not(HaveOccurred()))
						g.Expect(desiredReplicas).Should(And(BeNumerically(">", minReplicas), BeNumerically("<=", maxReplicas)))
						ctx.t.Logf("        ->>>  Pod info successfuly scaled to %d        <<<-\n\n\n", desiredReplicas)
						cancelHey <- true
					}).WithPolling(3 * time.Second).WithContext(context.TODO()).Should(Succeed())
				})
				time.Sleep(5 * time.Second)
				It("should eventually scale the podinfo from N -> 1", func() {
					Eventually(func(g Gomega) {
						out, err := kubectl("get hpa keda-hpa-podinfo-pull-example -ojsonpath='{.status.desiredReplicas}'")
						g.Expect(err).Should(Not(HaveOccurred()))
						desiredReplicas, err := strconv.Atoi(strings.Trim(out, "'"))
						g.Expect(err).Should(Not(HaveOccurred()))
						g.Expect(desiredReplicas).Should(Equal(minReplicas))
						ctx.t.Logf("        ->>>  Pod info successfuly scaled back to %d        <<<-\n\n\n", desiredReplicas)
					}).WithPolling(3 * time.Second).WithContext(context.TODO()).Should(Succeed())
				})
			})
		})
	})
})

var _ = ReportAfterSuite("ReportAfterSuite", func(report Report) {
	if !report.SuiteSucceeded {
		ctx.t.Log("Test suite failed, leaving k3d cluster alive for inspection..")
		if printLogs == "true" {
			kubectl("get hpa keda-hpa-podinfo-pull-example -oyaml")
			kubectl("get so podinfo-pull-example -oyaml")
			kubectl("get pods -A")
			for _, nameAndNs := range []string{"podinfo -ndefault", "keda-operator -nkeda", "opentelemetry-collector -ndefault", "otel-add-on -ndefault"} {
				ctx.t.Logf("\n\n\n        ->>>  Logs for %s        <<<-\n\n\n", nameAndNs)
				kubectl(fmt.Sprintf("logs -lapp.kubernetes.io/name=%s --tail=-1", nameAndNs))
			}
		}
	} else if deleteCluster != "false" {
		ctx.t.Log("Deleting k3d cluster..")
		err := execCmdE(fmt.Sprintf("k3d cluster delete %s", clusterName))
		Expect(err).NotTo(HaveOccurred())
	}
})
