package helmchart_test

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"text/template"

	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/kedify/otel-add-on/metric"
)

type admissionAPIVersions struct{}

func (admissionAPIVersions) Has(version string) bool {
	return version == "admissionregistration.k8s.io/v1"
}

func admissionMetricQueryRegexp(t *testing.T) *regexp.Regexp {
	t.Helper()
	// Render the actual policy and its shared regex helper with the Go template
	// functions they use, so this test does not require a Helm binary or cluster.
	var chart *template.Template
	chart = template.New("admission-policy").Funcs(template.FuncMap{
		"include": func(name string, values any) (string, error) {
			var output bytes.Buffer
			err := chart.ExecuteTemplate(&output, name, values)
			return output.String(), err
		},
		"quote": strconv.Quote,
	})
	chart, err := chart.ParseFiles("otel-add-on/templates/_metric-query.tpl", "otel-add-on/templates/vap.yaml")
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{
		"Values":       map[string]any{"validatingAdmissionPolicy": map[string]any{"enabled": true, "name": "test-otel-scalers"}},
		"Capabilities": map[string]any{"APIVersions": admissionAPIVersions{}},
	}
	var output bytes.Buffer
	if err := chart.ExecuteTemplate(&output, "vap.yaml", values); err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Spec struct {
			Variables []struct {
				Name       string
				Expression string
			}
		}
	}
	if err := yaml.NewYAMLOrJSONDecoder(&output, 4096).Decode(&policy); err != nil {
		t.Fatal(err)
	}
	for _, variable := range policy.Spec.Variables {
		if variable.Name != "mqRegexp" {
			continue
		}
		// CEL raw strings preserve regex escapes after YAML decoding.
		pattern, ok := strings.CutPrefix(variable.Expression, `r"""`)
		if !ok {
			t.Fatalf("expected a CEL raw regex string, got %q", variable.Expression)
		}
		pattern, ok = strings.CutSuffix(pattern, `"""`)
		if !ok {
			t.Fatalf("unterminated CEL raw regex string: %q", variable.Expression)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatal(err)
		}
		return re
	}
	t.Fatal("rendered admission policy has no mqRegexp variable")
	return nil
}

func TestAdmissionPolicyMetricQueries(t *testing.T) {
	re := admissionMetricQueryRegexp(t)
	for _, query := range []string{
		"sum(avg(queue_depth))", "ln(queue_depth)", "1 + 2 * 3", "max(\nclamp_min(queue_depth, 1)\n)",
		"queue_depth", "avg(queue_depth)", "min(queue_depth)", "max(queue_depth)", "sum(queue_depth)", "count(queue_depth)",
		`queue_depth{queue=orders}`, `queue_depth{queue="orders"}`, `queue_depth{environment!="dev"}`,
		`queue_depth{queue=~"orders-.*",environment!="dev"}`, `count(queue_depth{queue!~".*-dev"})`,
		`queue_depth{queue=~"orders-[0-9]{1,3}"}`, `queue_depth{queue=~"orders-(us|eu){1,3}",environment!="dev"}`,
		`queue_depth{queue=~"orders-.*",queue!="orders-test",queue!="orders-dev"}`,
		`queue_depth{queue="a,b=c{}()",environment=""}`, `queue_depth{queue="quote\" comma, slash\\"}`,
		`queue_depth{queue='it\'s a queue',"http.method"='GET'}`,
		"queue_depth{queue=`raw,=\\value`}", "queue_depth{queue=`multi\nline`}",
		` avg( metric_name_test { queue =~ "orders-(a|b)", environment != 'dev', } ) `,
	} {
		t.Run(query, func(t *testing.T) {
			if _, err := metric.NewParser().Parse(query); err != nil {
				t.Fatalf("parser rejected supported query: %v", err)
			}
			if !re.MatchString(query) {
				t.Fatalf("admission policy rejected supported query %q", query)
			}
		})
	}
	// Admission deliberately leaves grammar and type checks to the application.
	for _, query := range []string{"unknown(queue_depth)", "sum(queue_depth", "queue_depth[5m]"} {
		if !re.MatchString(query) {
			t.Errorf("admission must defer expression syntax validation: %q", query)
		}
		if _, err := metric.NewParser().Parse(query); err == nil {
			t.Errorf("application accepted unsupported query: %q", query)
		}
	}
	for _, query := range []string{"", "  ", "\n\t\r "} {
		if re.MatchString(query) {
			t.Errorf("admission accepted blank query: %q", query)
		}
	}
}
