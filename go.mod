module github.com/kedify/otel-add-on

go 1.24.0

toolchain go1.24.3

require (
	github.com/fatih/color v1.18.0
	github.com/gin-gonic/gin v1.10.1
	github.com/go-logr/logr v1.4.2
	github.com/kedacore/keda/v2 v2.17.1
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/prometheus/client_golang v1.22.0
	github.com/swaggo/files v1.0.1
	github.com/swaggo/gin-swagger v1.6.0
	github.com/swaggo/swag v1.16.4
	go.opentelemetry.io/collector/component v1.32.0
	go.opentelemetry.io/collector/component/componentstatus v0.126.0
	go.opentelemetry.io/collector/component/componenttest v0.126.0
	go.opentelemetry.io/collector/config/configgrpc v0.126.0
	go.opentelemetry.io/collector/config/confignet v1.32.0
	go.opentelemetry.io/collector/consumer v1.32.0
	go.opentelemetry.io/collector/consumer/consumererror v0.126.0
	go.opentelemetry.io/collector/pdata v1.32.0
	go.opentelemetry.io/collector/receiver v1.32.0
	go.opentelemetry.io/collector/receiver/otlpreceiver v0.126.0
	go.opentelemetry.io/collector/receiver/receiverhelper v0.126.0
	go.uber.org/mock v0.5.2
	go.uber.org/zap v1.27.0
	golang.org/x/sync v0.14.0
	google.golang.org/grpc v1.72.1
	google.golang.org/protobuf v1.36.6
	k8s.io/apimachinery v0.33.1
	k8s.io/code-generator v0.33.1
	sigs.k8s.io/controller-runtime v0.20.4
	sigs.k8s.io/kustomize/kustomize/v5 v5.6.0
)

require (
	github.com/KyleBanks/depth v1.2.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/bytedance/sonic v1.13.2 // indirect
	github.com/bytedance/sonic/loader v0.2.4 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.5 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/foxboron/go-tpm-keyfiles v0.0.0-20250323135004-b31fac66206e // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/fxamacker/cbor/v2 v2.8.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.9 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.1 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/spec v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.1 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.26.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.3.0 // indirect
	github.com/gobwas/glob v0.2.3 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.6.9 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/go-tpm v0.9.5 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-version v1.7.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/knadh/koanf/providers/confmap v1.0.0 // indirect
	github.com/knadh/koanf/v2 v2.2.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/mostynb/go-grpc-compression v1.2.3 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pierrec/lz4/v4 v4.1.22 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.64.0 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/rs/cors v1.11.1 // indirect
	github.com/sergi/go-diff v1.3.1 // indirect
	github.com/spf13/cobra v1.9.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/collector v0.126.0 // indirect
	go.opentelemetry.io/collector/client v1.32.0 // indirect
	go.opentelemetry.io/collector/config/configauth v0.126.0 // indirect
	go.opentelemetry.io/collector/config/configcompression v1.32.0 // indirect
	go.opentelemetry.io/collector/config/confighttp v0.126.0 // indirect
	go.opentelemetry.io/collector/config/configmiddleware v0.126.0 // indirect
	go.opentelemetry.io/collector/config/configopaque v1.32.0 // indirect
	go.opentelemetry.io/collector/config/configtls v1.32.0 // indirect
	go.opentelemetry.io/collector/confmap v1.32.0 // indirect
	go.opentelemetry.io/collector/consumer/xconsumer v0.126.0 // indirect
	go.opentelemetry.io/collector/extension/extensionauth v1.32.0 // indirect
	go.opentelemetry.io/collector/extension/extensionmiddleware v0.126.0 // indirect
	go.opentelemetry.io/collector/featuregate v1.32.0 // indirect
	go.opentelemetry.io/collector/internal/sharedcomponent v0.126.0 // indirect
	go.opentelemetry.io/collector/internal/telemetry v0.126.0 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.126.0 // indirect
	go.opentelemetry.io/collector/pipeline v0.126.0 // indirect
	go.opentelemetry.io/collector/receiver/xreceiver v0.126.0 // indirect
	go.opentelemetry.io/contrib/bridges/otelzap v0.10.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.60.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.60.0 // indirect
	go.opentelemetry.io/otel v1.35.0 // indirect
	go.opentelemetry.io/otel/log v0.11.0 // indirect
	go.opentelemetry.io/otel/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk v1.35.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.35.0 // indirect
	go.opentelemetry.io/otel/trace v1.35.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/arch v0.17.0 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/mod v0.24.0 // indirect
	golang.org/x/net v0.40.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sys v0.33.0 // indirect
	golang.org/x/term v0.32.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	golang.org/x/time v0.11.0 // indirect
	golang.org/x/tools v0.33.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.5.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250519155744-55703ea1f237 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/api v0.33.1 // indirect
	k8s.io/apiextensions-apiserver v0.33.1 // indirect
	k8s.io/client-go v0.33.1 // indirect
	k8s.io/gengo/v2 v2.0.0-20250513215321-e3bc6f1e78b4 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250318190949-c8a335a9a2ff // indirect
	k8s.io/utils v0.0.0-20250502105355-0f33e8f1c979 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/kustomize/api v0.19.0 // indirect
	sigs.k8s.io/kustomize/cmd/config v0.19.0 // indirect
	sigs.k8s.io/kustomize/kyaml v0.19.0 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.7.0 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

replace (
	// pin k8s.io to v0.29.4
	github.com/google/cel-go => github.com/google/cel-go v0.17.8
	github.com/open-policy-agent/cert-controller => github.com/open-policy-agent/cert-controller v0.11.0
	github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.54.0
//k8s.io/api => k8s.io/api v0.29.4
//k8s.io/apimachinery => k8s.io/apimachinery v0.29.4
//k8s.io/apiserver => k8s.io/apiserver v0.29.4
//k8s.io/client-go => k8s.io/client-go v0.29.4
//k8s.io/code-generator => k8s.io/code-generator v0.29.4
//k8s.io/component-base => k8s.io/component-base v0.29.4
//k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00
//k8s.io/metrics => k8s.io/metrics v0.29.4
)
