package helmchart_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestHAChart(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("Helm is required for chart rendering tests")
	}
	if _, err := os.Stat("otel-add-on/charts/opentelemetry-collector-0.131.0.tgz"); err != nil {
		t.Skip("run helm dependency build helmchart/otel-add-on before chart rendering tests")
	}
	render := func(t *testing.T, values string, extra ...string) ([]map[string]any, []byte) {
		t.Helper()
		file := filepath.Join(t.TempDir(), "values.yaml")
		if err := os.WriteFile(file, []byte(values), 0600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"template", "scaling", "otel-add-on", "--namespace", "keda", "-f", file}, extra...)
		output, err := exec.Command(helm, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("helm template: %v\n%s", err, output)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(output), 4096)
		var objects []map[string]any
		for {
			var object map[string]any
			if err := decoder.Decode(&object); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			if object != nil {
				objects = append(objects, object)
			}
		}
		return objects, output
	}
	field := func(object map[string]any, keys ...string) any {
		var result any = object
		for _, key := range keys {
			result = result.(map[string]any)[key]
		}
		return result
	}
	t.Run("disabled", func(t *testing.T) {
		objects, original := render(t, "replicaCount: 2\n")
		_, explicit := render(t, "replicaCount: 2\nsettings:\n  ha:\n    enabled: false\n    replicas: 5\n")
		if !bytes.Equal(original, explicit) {
			t.Fatal("disabled HA settings changed the render")
		}
		for _, object := range objects {
			if object["kind"] == "StatefulSet" {
				t.Fatal("disabled HA rendered a StatefulSet")
			}
			if object["kind"] == "Deployment" && field(object, "spec", "replicas") != float64(2) {
				t.Fatal("legacy replicaCount changed")
			}
			if object["kind"] == "Service" && len(field(object, "spec", "ports").([]any)) != 4 {
				t.Fatal("legacy Service ports changed")
			}
		}
	})
	for _, values := range []string{
		"settings:\n  ha:\n    enabled: true\n    replicas: 3\n",
		"settings:\n  ha:\n    enabled: true\n    replicas: 3\nnamespace: telemetry\nfullnameOverride: scaler\nservice:\n  name: ingestion\n",
	} {
		t.Run(strings.ReplaceAll(values, "\n", "/"), func(t *testing.T) {
			values += "otelCollector:\n  enabled: true\notelOperatorCrs:\n  - enabled: true\n    name: managed\n"
			objects, _ := render(t, values)
			var scaler map[string]any
			for _, object := range objects {
				if object["kind"] == "StatefulSet" {
					scaler = object
					break
				}
			}
			if scaler == nil {
				t.Fatal("HA must create a StatefulSet")
			}
			governing := field(scaler, "spec", "serviceName").(string)
			expectedEndpoints := map[string]bool{}
			for ordinal := range 3 {
				expectedEndpoints[fmt.Sprintf("%s-%d.%s.%s.svc:4317", field(scaler, "metadata", "name"), ordinal, governing, field(scaler, "metadata", "namespace"))] = true
			}
			services, statefulsets, configs := 0, 0, 0
			for _, object := range objects {
				switch object["kind"] {
				case "Deployment":
					if field(object, "metadata", "name") != "otelcol" {
						t.Fatal("HA rendered a scaler Deployment")
					}
				case "StatefulSet":
					statefulsets++
					if field(object, "spec", "replicas") != float64(3) {
						t.Fatal("incorrect HA replica count")
					}
					container := field(object, "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
					if field(container, "readinessProbe", "httpGet", "path") != "/readyz" || field(container, "livenessProbe", "httpGet", "path") != "/info" {
						t.Fatal("HA must gate readiness while preserving liveness")
					}
					haEnabled := false
					for _, item := range container["env"].([]any) {
						env := item.(map[string]any)
						if env["name"] == "HA_ENABLED" && env["value"] == "true" {
							haEnabled = true
						}
					}
					if !haEnabled {
						t.Fatal("HA workload must enable runtime warmup")
					}
				case "Service":
					name := field(object, "metadata", "name").(string)
					if name == "otelcol" {
						continue
					}
					services++
					if strings.HasSuffix(name, "-h") {
						if name != governing {
							t.Fatal("StatefulSet must use the ingestion Service as its governing Service")
						}
						if field(object, "spec", "clusterIP") != "None" || field(object, "spec", "publishNotReadyAddresses") != true {
							t.Fatal("ingestion must bypass readiness")
						}
						if len(field(object, "spec", "ports").([]any)) != 1 {
							t.Fatal("ingestion Service must expose only OTLP")
						}
					} else if len(field(object, "spec", "ports").([]any)) != 3 {
						t.Fatal("query Service must expose the three other ports")
					} else if field(object, "spec", "publishNotReadyAddresses") == true {
						t.Fatal("query Service must respect Pod readiness")
					}
				case "ConfigMap":
					data := object["data"].(map[string]any)
					var config map[string]any
					if relay, ok := data["relay"]; ok {
						if err := yaml.Unmarshal([]byte(relay.(string)), &config); err != nil {
							t.Fatal(err)
						}
					} else if content, ok := data["content"]; ok {
						var cr map[string]any
						if err := yaml.Unmarshal([]byte(content.(string)), &cr); err != nil {
							t.Fatal(err)
						}
						config = field(cr, "spec", "config").(map[string]any)
					} else {
						continue
					}
					configs++
					var endpoints []string
					for name, raw := range config["exporters"].(map[string]any) {
						if name == "debug" {
							continue
						}
						if !strings.HasPrefix(name, "otlp/") {
							t.Fatalf("invalid exporter component ID: %s", name)
						}
						exporter := raw.(map[string]any)
						if !expectedEndpoints[exporter["endpoint"].(string)] {
							t.Fatalf("exporter does not address a StatefulSet replica: %v", exporter["endpoint"])
						}
						endpoints = append(endpoints, exporter["endpoint"].(string))
						if field(exporter, "sending_queue", "enabled") != true || field(exporter, "retry_on_failure", "enabled") != true {
							t.Fatal("replica delivery must have independent queues and retries")
						}
					}
					if len(endpoints) != 3 || endpoints[0] == endpoints[1] || endpoints[1] == endpoints[2] {
						t.Fatal("collector must target every replica separately")
					}
					if len(field(config, "service", "pipelines", "metrics", "exporters").([]any)) < 3 {
						t.Fatal("metrics pipeline did not enable fanout")
					}
				}
			}
			if services != 2 || statefulsets != 1 || configs != 2 {
				t.Fatalf("unexpected HA resources: services=%d StatefulSets=%d collector configs=%d", services, statefulsets, configs)
			}
		})
	}
}
