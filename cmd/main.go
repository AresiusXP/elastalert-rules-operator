/*
Copyright 2025 Patricio Danos.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
	"github.com/AresiusXP/elastalert-rules-operator/internal/controller"
	"github.com/AresiusXP/elastalert-rules-operator/internal/policy"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(alertsv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

type options struct {
	metricsAddr          string
	metricsCertPath      string
	metricsCertName      string
	metricsCertKey       string
	probeAddr            string
	enableLeaderElection bool
	secureMetrics        bool
	enableHTTP2          bool
	showVersion          bool

	allowedTargets       string
	allowedSecretTargets string
	allowedAlerters      string
	allowedIndexPatterns string
	allowedHTTPHosts     string
	allowedExtraFields   string
	enableSecretRefs     bool
	secretResync         time.Duration
	elastalertDeployment string
}

func (o *options) bind(fs *flag.FlagSet) {
	fs.BoolVar(&o.showVersion, "version", false, "Show version information and exit")
	fs.StringVar(&o.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	fs.StringVar(&o.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	fs.BoolVar(&o.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	fs.BoolVar(&o.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	fs.StringVar(&o.metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	fs.StringVar(&o.metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	fs.StringVar(&o.metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	fs.BoolVar(&o.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics server")

	fs.StringVar(&o.allowedTargets, "allowed-targets", "",
		"Comma separated namespace/name list of ConfigMaps rules may write to. Rules targeting anything else are rejected.")
	fs.StringVar(&o.allowedSecretTargets, "allowed-secret-targets", "",
		"Comma separated namespace/name list of Secrets rules may write to.")
	fs.StringVar(&o.allowedAlerters, "allowed-alerters", strings.Join(policy.DefaultAlerters, ","),
		"Comma separated ElastAlert alerters rules may use.")
	fs.StringVar(&o.allowedIndexPatterns, "allowed-index-patterns", "",
		"Comma separated glob patterns rules may query. Empty allows every index.")
	fs.StringVar(&o.allowedHTTPHosts, "allowed-http-hosts", "",
		"Comma separated hostnames http_post_url and http_post2_url may use. Empty allows every host.")
	fs.StringVar(&o.allowedExtraFields, "allowed-additional-fields", "",
		"Comma separated extra ElastAlert settings additionalFields and secretRefs may use, on top of the built-in list.")
	fs.BoolVar(&o.enableSecretRefs, "enable-secret-refs", false,
		"Allow rules to read credentials from labelled Secrets in their own namespace through spec.secretRefs.")
	fs.DurationVar(&o.secretResync, "secret-resync", 5*time.Minute,
		"How often rules that depend on Secrets are re-rendered.")
	fs.StringVar(&o.elastalertDeployment, "elastalert-deployment", "",
		"namespace/name of the ElastAlert Deployment to restart when rules change. Empty disables rollouts.")
}

func main() {
	var o options
	o.bind(flag.CommandLine)
	zapOpts := zap.Options{}
	zapOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	if o.showVersion {
		fmt.Printf("version=%s commit=%s buildDate=%s\n", version, commit, date)
		os.Exit(0)
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	setupLog.Info("Starting ElastAlert Rules Operator", "version", version, "commit", commit, "buildDate", date)

	pol, err := buildPolicy(&o)
	if err != nil {
		setupLog.Error(err, "invalid configuration")
		os.Exit(1)
	}
	rollout, err := rolloutTarget(o.elastalertDeployment)
	if err != nil {
		setupLog.Error(err, "invalid configuration")
		os.Exit(1)
	}

	var tlsOpts []func(*tls.Config)
	if !o.enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.NextProtos = []string{"http/1.1"}
		})
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   o.metricsAddr,
		SecureServing: o.secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if o.secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	if len(o.metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", o.metricsCertPath, "metrics-cert-name", o.metricsCertName, "metrics-cert-key", o.metricsCertKey)
		metricsServerOptions.CertDir = o.metricsCertPath
		metricsServerOptions.CertName = o.metricsCertName
		metricsServerOptions.KeyName = o.metricsCertKey
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		HealthProbeBindAddress: o.probeAddr,
		LeaderElection:         o.enableLeaderElection,
		LeaderElectionID:       "dd789420.example.dev",
		Cache:                  cacheOptions(pol),
		Client: client.Options{
			Cache: &client.CacheOptions{
				DisableFor: []client.Object{&corev1.Secret{}, &corev1.ConfigMap{}, &appsv1.Deployment{}},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.ElastAlertRuleReconciler{
		Client:           mgr.GetClient(),
		APIReader:        mgr.GetAPIReader(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorder("elastalertrule-controller"),
		Policy:           pol,
		Rollout:          rollout,
		EnableSecretRefs: o.enableSecretRefs,
		SecretResync:     o.secretResync,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ElastAlertRule")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager", "configMapTargets", len(pol.Namespaces(alertsv1alpha1.TargetKindConfigMap)),
		"secretRefs", o.enableSecretRefs, "rollout", rollout != nil)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func buildPolicy(o *options) (policy.Policy, error) {
	cms, err := policy.ParseTargets(alertsv1alpha1.TargetKindConfigMap, o.allowedTargets)
	if err != nil {
		return policy.Policy{}, fmt.Errorf("--allowed-targets: %w", err)
	}
	secrets, err := policy.ParseTargets(alertsv1alpha1.TargetKindSecret, o.allowedSecretTargets)
	if err != nil {
		return policy.Policy{}, fmt.Errorf("--allowed-secret-targets: %w", err)
	}
	return policy.New(append(cms, secrets...),
		policy.SplitList(o.allowedAlerters),
		policy.SplitList(o.allowedIndexPatterns),
		policy.SplitList(o.allowedHTTPHosts),
		policy.SplitList(o.allowedExtraFields)...), nil
}

// rolloutTarget parses --elastalert-deployment, falling back to the legacy
// ELASTALERT_ROLLOUT_ENABLED environment variables.
func rolloutTarget(flagValue string) (*types.NamespacedName, error) {
	if flagValue == "" && os.Getenv("ELASTALERT_ROLLOUT_ENABLED") == "true" {
		name, ns := os.Getenv("ELASTALERT_DEPLOYMENT_NAME"), os.Getenv("ELASTALERT_DEPLOYMENT_NAMESPACE")
		if name != "" && ns != "" {
			flagValue = ns + "/" + name
		}
	}
	if flagValue == "" {
		return nil, nil
	}
	ns, name, ok := strings.Cut(flagValue, "/")
	if !ok || ns == "" || name == "" {
		return nil, fmt.Errorf("--elastalert-deployment %q must be namespace/name", flagValue)
	}
	return &types.NamespacedName{Namespace: ns, Name: name}, nil
}

// cacheOptions limits the ConfigMap cache to the namespaces that hold targets.
func cacheOptions(pol policy.Policy) cache.Options {
	byObject := map[client.Object]cache.ByObject{}
	if namespaces := pol.Namespaces(alertsv1alpha1.TargetKindConfigMap); len(namespaces) > 0 {
		cfgs := make(map[string]cache.Config, len(namespaces))
		for _, ns := range namespaces {
			cfgs[ns] = cache.Config{}
		}
		byObject[&corev1.ConfigMap{}] = cache.ByObject{Namespaces: cfgs}
	}
	return cache.Options{ByObject: byObject}
}
