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
- **Automatic Sync**: Rules are written to a pre-existing ConfigMap or Secret with server-side apply, one field manager per rule
- **Allowlisted Targets**: The operator only writes to targets listed at install time, and its RBAC is generated for exactly those objects
- **Rule Policy**: Built-in rule types only, an alerter allowlist, and a blocklist for fields that run commands or redirect ElastAlert
- **Secrets Stay Secret**: Credentials can be injected from labelled Secrets instead of living in the resource
- **Validation**: The CRD schema (including CEL rules) rejects bad rules at `kubectl apply` time
- **Optional Deployment Rollout**: Annotate the ElastAlert Deployment to restart it when rules change

## Features

- ✅ CRD-based rule management
- ✅ Server-side-apply reconciliation with per-rule key ownership and conflict reporting
- ✅ Target allowlist with generated least-privilege RBAC
- ✅ Rule policy: rule types, alerters, blocked fields, optional index and host allowlists
- ✅ Secret references for credentials
- ✅ Finalizer-based cleanup on deletion
- ✅ Status conditions for observability
- ✅ Support for all ElastAlert rule types (frequency, spike, flatline, any, etc.)
- ✅ Native support for Opsgenie, email, and other alerters
- ✅ Field names match ElastAlert YAML format
- ✅ Optional automatic deployment rollout
- ✅ Schema and CEL validation at admission time
- ✅ Unit and envtest tests, signed releases with SBOM and provenance

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
│  - Applies policy   │      │                  │
│  - Server-side apply│      │ Data:            │
│  - Updates status   │      │ <ns>_<name>.yaml │
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
- Helm 3.8+ (for OCI installs)
- kubectl configured
- Go 1.27+ (for development)
- kubebuilder 3.0+ (for development)

**Important**: Target ConfigMaps and Secrets must exist before rules reference them, and each one must be listed in the chart's `targets` value. The operator never creates targets and refuses to write anywhere else.

### Quick Start

1. **Install the operator using Helm:**

```bash
helm install elastalert-rules-operator \
  oci://ghcr.io/aresiusxp/charts/elastalert-rules-operator \
  --namespace elastalert-rules-operator-system --create-namespace \
  --set 'targets[0].name=elastalert-rules' \
  --set 'targets[0].namespace=logging'

# Or from a local checkout
helm install elastalert-rules-operator helm/elastalert-rules-operator \
  --namespace elastalert-rules-operator-system --create-namespace \
  --set 'targets[0].name=elastalert-rules' --set 'targets[0].namespace=logging'
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
```

The Opsgenie API key is not part of the rule. Set it in ElastAlert's global configuration, or inject it with `secretRefs` as shown in [Secrets](#secrets).

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
  http_post_payload:
    _type: type
  name: '[monitoring] ElastAlert heartbeat'
  targetConfigMap:
    name: elastalert-rules
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

You can point a rule at a different target and namespace, as long as that target is in the operator's allowlist. This allows centralized rule management:

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

**Note**: If `targetConfigMap.namespace` is not specified, the target is looked up in the rule's namespace. A target that is not on the allowlist makes the rule fail with `TargetNotAllowed`, and nothing is written.

### Secrets

Keep credentials out of the resource: store them in a Secret, label it, and reference it. The rule then has to target a `Secret` (the rendered rule contains the credential), and the operator must run with `secretRefs.enabled=true`.

```bash
kubectl create secret generic opsgenie -n logging --from-literal=key=<api-key>
kubectl label secret opsgenie -n logging alerts.example.dev/allow-source=true
```

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: error-alert
  namespace: logging
spec:
  type: any
  index: app-logs-*
  alert: [opsgenie]
  opsgenie_message: Error detected
  targetConfigMap:
    kind: Secret
    name: elastalert-rule-secrets
  secretRefs:
    - field: opsgenie_key
      name: opsgenie
      key: key
    - field: http_post_headers
      name: opsgenie
      key: headers
      format: yaml
```

Secrets are read only from the rule's namespace, only with the `alerts.example.dev/allow-source=true` label, and never when the Secret is itself a write target. Rules that still carry credentials inline get a `PlaintextCredentials` condition.

## Security Model

| Risk | Control |
|------|---------|
| A rule writes to an arbitrary ConfigMap (for example `kube-system/aws-auth`) | Only targets in `--allowed-targets` and `--allowed-secret-targets` are accepted. The generated RBAC covers exactly those objects, in their namespaces only. |
| A rule makes ElastAlert run commands, load code or talk to another cluster | `type` must be a built-in rule type. Alerters must be on `--allowed-alerters` (default: opsgenie, post, slack, msteams, email, alertmanager, debug; `post2` is off because ElastAlert renders its payload with an unsandboxed Jinja template). `additionalFields` and `secretRefs` only accept a built-in list of safe settings plus `--allowed-additional-fields`; `command`, `pipe_*`, `import`, `match_enhancements`, `es_*`, the `timestamp_*` expression settings, TLS settings, proxies, `rules_folder` and `alert_text_type` are always rejected. |
| One rule overwrites another | Default keys are `<namespace>_<name>.yaml`. Each rule applies with its own server-side-apply field manager, so key collisions surface as `KeyConflict`. Duplicate ElastAlert rule names are rejected (`DuplicateRuleName`). |
| Credentials in the spec or ConfigMap | `secretRefs` and Secret targets. |
| Reading arbitrary indices or posting to arbitrary hosts | Optional `--allowed-index-patterns` and `--allowed-http-hosts`. Host checks run on the final rule, including URLs that come from Secrets. |
| A rule that stops being acceptable keeps running | Rules that fail validation or policy are withdrawn from the target. A rule's target cannot be changed after creation. |
| Excess privileges | No cluster-wide write access to ConfigMaps or Deployments. Secret `get` is cluster-wide only when `secretRefs.enabled=true`. |

`blacklist` and `whitelist` entries may not use `!file`, and `alert_text_args` and `alert_subject_args` may not name ElastAlert settings, because ElastAlert falls back to rule and global configuration for names that are missing from an event. Error messages never include rejected values.

**Residual risk.** The policy is a default-deny filter in front of a powerful engine, not a sandbox. Anyone who can create an `ElastAlertRule` can still make ElastAlert query the allowed indices and post alert content to the allowed hosts, so restrict who may create rules and set the index and host allowlists on shared clusters. Add settings to `--allowed-additional-fields` only after checking ElastAlert's code for template rendering, `eval` or file access. Also harden the ElastAlert pod itself: no credentials in its global configuration beyond what it needs, a read-only root filesystem, a NetworkPolicy, and a restricted ServiceAccount.

## CRD Specification

### ElastAlertRuleSpec

**Note**: Field names are aligned with ElastAlert's YAML format for consistency.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Built-in ElastAlert rule type: any, blacklist, whitelist, change, frequency, spike, flatline, new_term, cardinality, metric_aggregation, spike_aggregation, percentage_match |
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
| `opsgenie_key` | string | No | Opsgenie API key (prefer `secretRefs`) |
| `opsgenie_message` | string | No | Opsgenie alert message |
| `opsgenie_priority` | string | No | Opsgenie alert priority (P1-P5) |
| `opsgenie_tags` | list | No | Tags to add to Opsgenie alerts |
| `opsgenie_teams` | list | No | Opsgenie teams to notify |
| `http_post_url` | string | No | URL to POST alerts to (for HTTP POST alerter) |
| `http_post_payload` | object | No | Payload structure for HTTP POST alerts |
| `http_post_headers` | object | No | Headers for HTTP POST alerts (prefer `secretRefs` for tokens) |
| `buffer_time` | object | No | Time buffer for queries (percentage_match, metric aggregation) |
| `bucket_interval` | object | No | Time bucket interval for percentage calculation (percentage_match) |
| `min_denominator` | integer | No | Minimum number of documents for percentage calculation (percentage_match) |
| `min_percentage` | integer | No | Minimum percentage threshold to trigger alert (percentage_match) |
| `match_bucket_filter` | list | No | Filter for the match bucket in percentage calculations (percentage_match) |
| `fields` | list | No | List of fields to monitor (new_term rules) |
| `name` | string | No | ElastAlert rule name (default: `<namespace>-<name>`) |
| `targetConfigMap.name` | string | No | Target name (default: "elastalert-rules") |
| `targetConfigMap.namespace` | string | No | Target namespace (default: CR namespace) |
| `targetConfigMap.kind` | string | No | `ConfigMap` (default) or `Secret`. The whole `targetConfigMap` block is immutable |
| `keyName` | string | No | Target key name, must end in `.yaml` or `.yml` (default: `<namespace>_<name>.yaml`) |
| `secretRefs` | list | No | Fields filled from labelled Secrets: `field`, `name`, `key`, `format` (`string` or `yaml`). Requires a Secret target |
| `additionalFields` | object | No | Other ElastAlert settings from the operator's allowed list. Keys that clash with spec fields or are not allowed are rejected |

### ElastAlertRuleStatus

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | []Condition | Status conditions |
| `observedGeneration` | integer | Generation the conditions were computed from |
| `ruleChecksum` | string | SHA-256 of the rule last written to the target |
| `lastReconciled` | timestamp | When the written rule last changed |

### Supported Condition Types

- **Ready**: Rule successfully written to the target
- **ValidationFailed**: Rejected by validation or policy. Reasons: `ValidationFailed`, `TargetNotAllowed`, `PolicyViolation`, `DuplicateRuleName`, `RenderFailed`
- **Error**: Failure while writing. Reasons: `TargetNotFound`, `KeyConflict`, `SecretRefFailed`, `ReconciliationFailed`
- **PlaintextCredentials**: The spec carries credentials inline

## Configuration

### Operator Flags

The Helm chart sets these from its values.

| Flag | Chart value | Description |
|------|-------------|-------------|
| `--allowed-targets` | `targets` (kind ConfigMap) | Comma separated `namespace/name` ConfigMaps rules may write to |
| `--allowed-secret-targets` | `targets` (kind Secret) | Same for Secrets |
| `--allowed-alerters` | `policy.allowedAlerters` | Alerters rules may use |
| `--allowed-additional-fields` | `policy.allowedAdditionalFields` | Extra settings `additionalFields` and `secretRefs` may use |
| `--allowed-index-patterns` | `policy.allowedIndexPatterns` | Glob patterns of indices rules may query (empty: any) |
| `--allowed-http-hosts` | `policy.allowedHttpHosts` | Hosts every `*_url`, `*_urls` and `*_hosts` setting may use (empty: any) |
| `--enable-secret-refs` | `secretRefs.enabled` | Allow `spec.secretRefs` |
| `--secret-resync` | `secretRefs.resync` | How often rules with Secret dependencies are re-rendered (default 5m) |
| `--elastalert-deployment` | `elastalert.*` | `namespace/name` of the ElastAlert Deployment to restart when rules change |
| `--leader-elect` | always on | Leader election |
| `--metrics-bind-address` | `metrics.*` | Authenticated HTTPS metrics endpoint |

With rollouts enabled the operator calculates a SHA-256 checksum of the target and patches the Deployment's pod template annotation `alerts.example.dev/rules-checksum`, which triggers a rolling update when it changes.

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
   kubectl auth can-i patch configmaps/elastalert-rules -n logging --as=system:serviceaccount:elastalert-rules-operator-system:elastalert-rules-operator
   ```

4. A `KeyConflict` means another rule or user owns the key. List the owners:
   ```bash
   kubectl get configmap <configmap-name> -n <namespace> --show-managed-fields -o yaml
   ```

### Validation Errors

Common validation errors:

- **Missing required field**: Ensure `type` and `index` are specified
- **Invalid timeframe**: Use Go duration format (e.g., "5m", "1h")
- **Invalid numEvents**: Must be >= 1 for frequency rules
- **Bad keyName**: Must match `^[-._a-zA-Z0-9]+\.ya?ml$`
- **TargetNotAllowed**: Add the target to the chart's `targets` value
- **PolicyViolation**: The alerter, index, host or additional field is not allowed
- **Target does not exist**: Create the target before creating the ElastAlertRule

## Examples

The examples above demonstrate the various rule types and configurations supported by the operator.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Run `make test`, `make lint` and `make manifests`
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

- [ ] Operator health alerts and dashboards
- [ ] Validating admission webhook for policy checks at admission time
- [ ] Multi-cluster support

## Support

For issues, questions, or contributions, please open an issue in the GitHub repository.
