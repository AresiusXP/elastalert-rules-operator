# ElastAlert Rules Operator

[![CI](https://github.com/AresiusXP/elastalert-rules-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/AresiusXP/elastalert-rules-operator/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

A Kubernetes operator that manages ElastAlert rules via Custom Resource Definitions (CRD). This operator automatically syncs ElastAlert rule definitions from Kubernetes CRs to a ConfigMap that can be mounted into your ElastAlert deployment.

> [!CAUTION]
> **⚠️ EXPERIMENTAL SOFTWARE - USE WITH CARE**
> 
> This operator has been primarily developed through AI-assisted coding ("vibe coding"). While it has comprehensive test coverage and has been validated in development environments, it should be treated as experimental software. 
>
> **Recommendations:**
> - Test thoroughly in non-production environments first
> - Monitor the operator logs closely when deploying to production
> - Review the generated ConfigMaps to ensure rule correctness
> - Have rollback procedures in place
> - Report any issues you encounter
>
> Contributions, bug reports, and feedback are welcome!

## Overview

The ElastAlert Rules Operator simplifies the management of ElastAlert rules in Kubernetes by:

- **Declarative Rule Management**: Define ElastAlert rules as Kubernetes custom resources
- **Automatic ConfigMap Sync**: Rules are automatically written to a pre-existing ConfigMap
- **Namespace Isolation**: Each rule is isolated by namespace
- **Validation**: Built-in validation ensures rules are correctly formatted before deployment
- **Optional Deployment Rollout**: Automatically trigger ElastAlert pod restarts when rules change
- **Merge Strategy**: Preserves existing ConfigMap keys when managing multiple rules

## Features

- ✅ CRD-based rule management
- ✅ Automatic ConfigMap reconciliation
- ✅ Finalizer-based cleanup on deletion
- ✅ Status conditions for observability
- ✅ Support for all ElastAlert rule types (frequency, spike, flatline, any, etc.)
- ✅ Native support for Opsgenie, email, and other alerters
- ✅ Field names match ElastAlert YAML format
- ✅ Optional automatic deployment rollout
- ✅ Comprehensive validation
- ✅ Unit and integration tests

## Architecture

```
┌─────────────────────┐
│  ElastAlertRule CR  │
│   (Kubernetes CRD)  │
└──────────┬──────────┘
           │
           │ Watches
           ▼
┌─────────────────────┐      ┌──────────────────┐
│  Operator/Controller│─────▶│   ConfigMap      │
│                     │      │  (elastalert-    │
│  - Validates spec   │      │   rules)         │
│  - Converts to YAML │      │                  │
│  - Manages keys     │      │ Data:            │
│  - Updates status   │      │   name.yaml      │
└─────────────────────┘      └────────┬─────────┘
                                      │
                                      │ Mounted as volume
                                      ▼
                            ┌──────────────────┐
                            │  ElastAlert Pod  │
                            │                  │
                            │  Reads rules and │
                            │  sends alerts    │
                            └──────────────────┘
```

## Installation

### Prerequisites

- Kubernetes cluster (v1.25+)
- kubectl configured
- Go 1.26+ (for development)
- kubebuilder 3.0+ (for development)

**Important**: The operator requires that ConfigMaps be pre-created before ElastAlertRules can reference them. This ensures proper configuration management and prevents accidental ConfigMap creation.

### Quick Start

1. **Install the operator using Helm:**

```bash
# Install the Helm chart
helm install elastalert-rules-operator helm/elastalert-rules-operator

# Or from GHCR
helm install elastalert-rules-operator \
  oci://ghcr.io/aresiusxp/charts/elastalert-rules-operator \
  --namespace elastalert-rules-operator-system --create-namespace

# Or from your own registry
helm install elastalert-rules-operator helm/elastalert-rules-operator \
  --set image.repository=<your-registry>/elastalert-rules-operator \
  --set image.tag=latest
```

2. **Create the target ConfigMap:**

```bash
kubectl create namespace logging
kubectl create configmap elastalert-rules -n logging
```

3. **Create a sample ElastAlert rule:**

```bash
kubectl apply -f - <<EOF
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: example-rule
  namespace: logging
spec:
  type: frequency
  index: logs-*
  timeframe:
    minutes: 5
  num_events: 10
  alert_text: "Example alert triggered"
  targetConfigMap:
    name: elastalert-rules
EOF
```

4. **Verify the ConfigMap was updated:**

```bash
kubectl get configmap elastalert-rules -n logging -o yaml
```

### Running Locally

For development and testing:

```bash
# Install CRDs into the cluster (applies helm/elastalert-rules-operator/crds/)
make install

# Run the controller locally
make run
```

## Usage

### Basic Example

Create a frequency-based alert rule:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: error-burst
  namespace: logging
spec:
  type: frequency
  index: logs-*
  timeframe:
    minutes: 5
  num_events: 25
  query_key: "kubernetes.container_name"
  filter:
    - term:
        level: "error"
  alert_text: "High error rate detected in {{query_key}}"
  alert:
    - email
  targetConfigMap:
    name: elastalert-rules
```

### Opsgenie Integration Example

Full example with Opsgenie alerting:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  namespace: logging
  name: application-error-alert
spec:
  type: frequency
  index: app-logs-*
  filter:
    - terms:
        level:
          - error
          - exception
    - query:
        match_phrase:
          namespace: checkout-service
  timeframe:
    minutes: 5
  num_events: 1
  alert_text: '{0}: {1} | {2} | {3}'
  alert_text_args:
    - component
    - logger_name
    - message
    - stack_trace
  alert:
    - opsgenie
  exponential_realert:
    minutes: 15
  include:
    - component
    - kubernetes.container_image
    - kubernetes.container_name
    - namespace
    - kubernetes.pod_name
    - logger_name
    - message
    - stack_trace
  name: '[team-platform|checkout-service] Error in checkout service'
  opsgenie_key: your-opsgenie-api-key
  opsgenie_message: '[team-platform|checkout-service] Error detected'
  opsgenie_priority: P3
  opsgenie_tags:
    - checkout-service
    - error
  opsgenie_teams:
    - team-platform
  realert:
    minutes: 5
  targetConfigMap:
    name: elastalert-rules
    namespace: default
```

### HTTP POST Alerter Example

Send alerts to a custom HTTP endpoint (e.g., for heartbeat monitoring or webhook integrations):

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: elastalert-heartbeat
  namespace: monitoring
spec:
  type: frequency
  index: elastalert_status
  num_events: 1
  timeframe:
    hours: 1
  alert:
    - post
  http_post_url: https://api.opsgenie.com/v2/heartbeats/elastalert/ping
  http_post_headers:
    Authorization: "GenieKey your-api-key"
  http_post_payload:
    _type: type
  name: '[monitoring] ElastAlert heartbeat'
  targetConfigMap:
    name: elastalert-rules
    namespace: default
```

### Percentage Match Example

Monitor when a percentage of matching documents exceeds thresholds (e.g., high error rate):

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: api-high-fail-rate
  namespace: monitoring
spec:
  type: percentage_match
  index: accesslogs-prod-*
  query_key: request_X-Target-Id.keyword
  filter:
    - query_string:
        query: '_exists_: OriginStatus AND _exists_: request_X-Target-Id AND ServiceName: "edge-proxy" AND NOT OriginStatus: 404'
  match_bucket_filter:
    - terms:
        OriginStatus:
          - 200
          - 201
  buffer_time:
    minutes: 15
  bucket_interval:
    minutes: 15
  min_denominator: 50
  min_percentage: 30
  fields:
    - request_X-Target-Id.keyword
    - percentage
  alert:
    - opsgenie
  opsgenie_key: your-api-key
  opsgenie_message: 'High error rate detected for target {request_X-Target-Id}: Only {percentage}% of requests succeeded in the last 15 minutes'
  opsgenie_priority: P4
  opsgenie_tags:
    - prod
  opsgenie_teams:
    - team-platform
  realert:
    hours: 4
  name: '[team-platform|prod] API high fail rate alert'
  targetConfigMap:
    name: elastalert-rules
    namespace: default
```

### Spike Detection Example

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: spike-detection
  namespace: logging
spec:
  type: spike
  index: metrics-*
  timeframe: 15m
  query_key: "service.name"
  filter:
    - range:
        response_time:
          gte: 1000
  alert_text: "Response time spike detected for service {{query_key}}"
  alert:
    - email
  additionalFields:
    spike_height: 2
    spike_type: "up"
    threshold_ref: 10
    threshold_cur: 20
    email:
      - "alerts@example.com"
  targetConfigMap:
    name: elastalert-rules
```

### Custom ConfigMap and Key Names

You can specify a different ConfigMap name and even a different namespace. This allows centralized rule management or cross-namespace configurations:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: my-custom-rule
  namespace: monitoring  # Rule is created here
spec:
  type: any
  index: security-*
  targetConfigMap:
    name: my-custom-configmap      # Custom ConfigMap name
    namespace: security-team        # ConfigMap looked up in this namespace
  keyName: custom-rule-name.yaml    # Custom key name in ConfigMap
```

**Note**: If `targetConfigMap.namespace` is not specified, the operator looks for the ConfigMap in the same namespace as the ElastAlertRule. The operator's namespace is never used for ConfigMap lookup.

## CRD Specification

### ElastAlertRuleSpec

**Note**: Field names are aligned with ElastAlert's YAML format for consistency.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | ElastAlert rule type (frequency, any, spike, flatline, new_term, cardinality, etc.) |
| `index` | string | Yes | OpenSearch/Elasticsearch index pattern |
| `filter` | list | No | List of filter clauses for the query |
| `timeframe` | object | No | Time window for rule evaluation (e.g., `{minutes: 5}`, `{hours: 1}`, `{days: 2}`) |
| `num_events` | integer | No* | Number of events to trigger alert (*required for frequency type) |
| `query_key` | string | No | Field to group by |
| `alert_text` | string | No | Text to include in the alert |
| `alert_text_args` | list | No | Fields to use in alert_text formatting |
| `alert` | list | No | List of alerters to use (e.g., ["email"], ["opsgenie"]) |
| `realert` | object | No | Minimum time between alerts |
| `exponential_realert` | object | No | Exponential backoff for realerting |
| `include` | list | No | List of fields to include in alerts |
| `opsgenie_key` | string | No | Opsgenie API key |
| `opsgenie_message` | string | No | Opsgenie alert message |
| `opsgenie_priority` | string | No | Opsgenie alert priority (P1-P5) |
| `opsgenie_tags` | list | No | Tags to add to Opsgenie alerts |
| `opsgenie_teams` | list | No | Opsgenie teams to notify |
| `http_post_url` | string | No | URL to POST alerts to (for HTTP POST alerter) |
| `http_post_payload` | object | No | Payload structure for HTTP POST alerts |
| `http_post_headers` | object | No | Headers for HTTP POST alerts |
| `buffer_time` | object | No | Time buffer for queries (percentage_match, metric aggregation) |
| `bucket_interval` | object | No | Time bucket interval for percentage calculation (percentage_match) |
| `min_denominator` | integer | No | Minimum number of documents for percentage calculation (percentage_match) |
| `min_percentage` | integer | No | Minimum percentage threshold to trigger alert (percentage_match) |
| `match_bucket_filter` | list | No | Filter for the match bucket in percentage calculations (percentage_match) |
| `fields` | list | No | List of fields to monitor (new_term rules) |
| `name` | string | No | Rule name (defaults to CR name) |
| `targetConfigMap.name` | string | No | Target ConfigMap name (default: "elastalert-rules") |
| `targetConfigMap.namespace` | string | No | Target ConfigMap namespace (default: CR namespace) |
| `keyName` | string | No | ConfigMap key name (default: "\<name\>.yaml") |
| `additionalFields` | object | No | Any other ElastAlert fields not explicitly defined above |

### ElastAlertRuleStatus

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | []Condition | Status conditions (Ready, ValidationFailed, Error) |
| `observedConfigMapResourceVersion` | string | Last observed ConfigMap resource version |
| `lastReconciled` | timestamp | Timestamp of last successful reconciliation |

### Supported Condition Types

- **Ready**: Rule successfully written to ConfigMap
- **ValidationFailed**: Rule spec validation failed
- **Error**: Error occurred during reconciliation

## Configuration

### Environment Variables

The operator supports optional environment variables for triggering ElastAlert deployment rollouts:

| Variable | Description | Example |
|----------|-------------|---------|
| `ELASTALERT_ROLLOUT_ENABLED` | Set to `"true"` to enable deployment rollouts | `true` |
| `ELASTALERT_DEPLOYMENT_NAME` | Name of the ElastAlert deployment | `elastalert` |
| `ELASTALERT_DEPLOYMENT_NAMESPACE` | Namespace of the ElastAlert deployment | `logging` |

When `ELASTALERT_ROLLOUT_ENABLED` is `"true"` and both the deployment name and
namespace are set, the operator will:
1. Calculate a SHA256 checksum of all rule keys in the ConfigMap
2. Update the deployment's pod template annotation `alerts.example.dev/rules-checksum`
3. Trigger a rolling update when the checksum changes

Example deployment configuration:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: elastalert-rules-operator
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: ELASTALERT_ROLLOUT_ENABLED
          value: "true"
        - name: ELASTALERT_DEPLOYMENT_NAME
          value: "elastalert"
        - name: ELASTALERT_DEPLOYMENT_NAMESPACE
          value: "logging"
```

## Development

### Building

```bash
# Generate manifests (CRDs, RBAC, etc.)
make manifests

# Build the operator binary
make build

# Build the Docker image
make docker-build IMG=your-registry/elastalert-rules-operator:tag
```

### Testing

```bash
# Run unit tests
make test

# Run tests with coverage
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Code Generation

After modifying the API types:

```bash
# Generate code (DeepCopy, etc.)
make generate

# Generate manifests (CRDs, RBAC, etc.)
make manifests
```

## Deployment

### Using Helm (Recommended)

```bash
# Deploy with default settings
helm install elastalert-rules-operator helm/elastalert-rules-operator

# Deploy with custom image
helm install elastalert-rules-operator helm/elastalert-rules-operator \
  --set image.repository=your-registry/elastalert-rules-operator \
  --set image.tag=v1.0.0

# Upgrade an existing installation
helm upgrade elastalert-rules-operator helm/elastalert-rules-operator

# Uninstall
helm uninstall elastalert-rules-operator
```

### Using Make (for Development)

```bash
# Deploy using Helm via Make
make deploy IMG=your-registry/elastalert-rules-operator:tag

# Undeploy
make undeploy
```

## Monitoring

### Check Operator Status

```bash
# View operator pods
kubectl get pods -n elastalert-rules-operator-system

# View operator logs
kubectl logs -n elastalert-rules-operator-system deployment/elastalert-rules-operator
```

### Check Rule Status

```bash
# List all ElastAlertRules
kubectl get elastalertrules -A

# Get detailed rule status
kubectl describe elastalertrule <name> -n <namespace>

# Check conditions
kubectl get elastalertrule <name> -n <namespace> -o jsonpath='{.status.conditions}'
```

## Troubleshooting

### Rule Not Applied

1. Check the rule status:
   ```bash
   kubectl describe elastalertrule <name> -n <namespace>
   ```

2. Look for validation errors in conditions:
   ```bash
   kubectl get elastalertrule <name> -n <namespace> -o yaml | grep -A 10 conditions
   ```

3. Check operator logs:
   ```bash
   kubectl logs -n elastalert-rules-operator-system deployment/elastalert-rules-operator
   ```

### ConfigMap Not Updated

1. Verify the ConfigMap exists (ConfigMaps must be created before creating ElastAlertRules):
   ```bash
   kubectl get configmap <configmap-name> -n <namespace>
   ```

2. If the ConfigMap doesn't exist, create it:
   ```bash
   kubectl create configmap <configmap-name> -n <namespace>
   ```

3. Check RBAC permissions:
   ```bash
   kubectl auth can-i update configmaps --as=system:serviceaccount:elastalert-rules-operator-system:elastalert-rules-operator
   ```

### Validation Errors

Common validation errors:

- **Missing required field**: Ensure `type` and `index` are specified
- **Invalid timeframe**: Use Go duration format (e.g., "5m", "1h")
- **Invalid numEvents**: Must be >= 1 for frequency rules
- **Path separator in keyName**: KeyName cannot contain "/"
- **ConfigMap does not exist**: Create the target ConfigMap before creating the ElastAlertRule

## Examples

The examples above demonstrate the various rule types and configurations supported by the operator.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Run `make test` and `make manifests`
6. Submit a pull request

## License

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

## Roadmap

- [ ] Webhook validation
- [ ] Metrics and alerting for operator health
- [ ] Support for ElastAlert 2.x
- [ ] Multi-cluster support
- [ ] Helm chart for operator deployment
- [ ] Advanced alert routing configurations

## Support

For issues, questions, or contributions, please open an issue in the GitHub repository.
