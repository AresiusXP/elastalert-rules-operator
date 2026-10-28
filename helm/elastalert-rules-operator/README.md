# ElastAlert Rules Operator Helm Chart

A Kubernetes operator for managing ElastAlert rules via Custom Resource Definitions (CRDs).

> [!CAUTION]
> **⚠️ EXPERIMENTAL SOFTWARE - USE WITH CARE**
> 
> This operator has been primarily developed through AI-assisted coding. While it has comprehensive test coverage and has been validated in development environments, it should be treated as experimental software. Test thoroughly before using in production environments.

## Overview

This Helm chart deploys the ElastAlert Rules Operator, which allows you to manage ElastAlert rules as Kubernetes custom resources. The operator watches for `ElastAlertRule` CRs and writes them to ConfigMaps or Secrets that ElastAlert consumes. It only writes to the targets you list in the `targets` value, and the RBAC it gets is generated for exactly those objects.

## Prerequisites

- Kubernetes 1.25+
- Helm 3.8+
- Pre-created ConfigMaps (or Secrets) for storing ElastAlert rules, listed in the `targets` value

## Installation

### Install from OCI Registry (GHCR)

```bash
helm install elastalert-rules-operator \
  oci://ghcr.io/aresiusxp/charts/elastalert-rules-operator \
  --version 0.6.0 \
  --namespace elastalert-rules-operator-system \
  --create-namespace \
  --set 'targets[0].name=elastalert-rules' \
  --set 'targets[0].namespace=monitoring'
```

### Install from Local Chart

```bash
helm install elastalert-rules-operator ./helm/elastalert-rules-operator \
  --namespace elastalert-rules-operator-system \
  --create-namespace \
  --set 'targets[0].name=elastalert-rules' \
  --set 'targets[0].namespace=monitoring'
```

Without `targets[].namespace` the target is looked up in the release namespace. Helm does not upgrade CRDs: apply `crds/` yourself when upgrading across versions.

## Configuration

### Key Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of operator replicas | `1` |
| `image.repository` | Operator image repository | `ghcr.io/aresiusxp/elastalert-rules-operator` |
| `image.tag` | Operator image tag | Chart appVersion |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `256Mi` |
| `resources.requests.cpu` | CPU request | `10m` |
| `resources.requests.memory` | Memory request | `64Mi` |
| `targets` | ConfigMaps and Secrets rules may write to (`name`, `namespace`, `kind`) | `[{name: elastalert-rules, kind: ConfigMap}]` |
| `policy.allowedAlerters` | Alerters rules may use | opsgenie, post, slack, msteams, email, alertmanager, debug |
| `policy.allowedAdditionalFields` | Extra settings `additionalFields` and `secretRefs` may use | `[]` |
| `policy.allowedIndexPatterns` | Glob patterns of indices rules may query (empty: any) | `[]` |
| `policy.allowedHttpHosts` | Hosts HTTP POST alerters may use (empty: any) | `[]` |
| `secretRefs.enabled` | Let rules read labelled Secrets (grants cluster-wide Secret `get`) | `false` |
| `metrics.enabled` | Serve authenticated HTTPS metrics, create a Service | `false` |
| `metrics.readerSubjects` | Subjects bound to the `/metrics` reader ClusterRole (for example the Prometheus ServiceAccount) | `[]` |
| `metrics.serviceMonitor.enabled` | Create a Prometheus Operator ServiceMonitor | `false` |
| `networkPolicy.enabled` | Restrict ingress and egress of the operator pod | `false` |
| `podDisruptionBudget.enabled` | Create a PodDisruptionBudget | `false` |
| `rbac.create` | Create RBAC resources | `true` |
| `serviceAccount.create` | Create ServiceAccount | `true` |

### ElastAlert Integration

Configure automatic rollout of ElastAlert deployment when rules change:

```yaml
elastalert:
  enabled: true
  deploymentName: "elastalert"
  deploymentNamespace: "monitoring"
```

When enabled, the operator patches the ElastAlert deployment's pod template annotation when the target changes, triggering a rolling update. Its permission is limited to `get` and `patch` on that one Deployment.

## Usage

### Creating the Target ConfigMap

**Important**: Before creating ElastAlertRules, you must first create the target ConfigMap. The operator never creates targets, and refuses targets that are not in the `targets` value.

```bash
kubectl create namespace monitoring
kubectl create configmap elastalert-rules -n monitoring
```

### Creating an ElastAlert Rule

Once the operator is installed and the ConfigMap exists, you can create ElastAlert rules using the `ElastAlertRule` CRD:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: high-error-rate
  namespace: monitoring
spec:
  type: frequency
  index: "logs-*"
  timeframe:
    minutes: 5
  num_events: 10
  filter:
    - query:
        query_string:
          query: "level:ERROR"
  alert_text: "High error rate detected!"
  alert:
    - email
```

### Specifying Target ConfigMap

By default, rules are written to the `elastalert-rules` ConfigMap **in the same namespace as the rule**. You can override the name, namespace and kind (`ConfigMap` or `Secret`), as long as the target is in the `targets` value:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: my-rule
  namespace: default              # Rule created here
spec:
  type: frequency
  index: "logs-*"
  num_events: 5
  timeframe: "10m"
  targetConfigMap:
    name: "my-custom-configmap"   # Optional: defaults to "elastalert-rules"
    namespace: "monitoring"        # Optional: ConfigMap looked up here (defaults to rule's namespace)
```

This allows you to have rules in different namespaces all writing to a centralized ConfigMap, or keep them isolated per namespace.

### Opsgenie Integration

Full example with Opsgenie alerting. Provide the API key through ElastAlert's global configuration, or with `secretRefs` (see the [main README](../../README.md#secrets)); rules that carry it inline get a `PlaintextCredentials` condition:

```yaml
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: production-errors
  namespace: monitoring
spec:
  type: frequency
  index: "application-logs-*"
  timeframe: "15m"
  num_events: 20
  query_key: "host.name"
  alert_text: "Multiple failures on host {0}"
  alert_text_args:
    - host.name
  filter:
    - query:
        bool:
          must:
            - match:
                severity: "critical"
  alert:
    - opsgenie
  opsgenie_message: "Critical errors detected"
  opsgenie_priority: P1
  opsgenie_tags:
    - production
    - critical
  opsgenie_teams:
    - ops-team
  realert:
    minutes: 30
  include:
    - host.name
    - message
    - stack_trace
```

**HTTP POST Alerter Example** (for heartbeat monitoring or custom webhooks):

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
```

## ConfigMap Structure

The operator writes one key per rule, named `<namespace>_<name>.yaml` unless `keyName` is set, using server-side apply with a field manager per rule:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: elastalert-rules
  namespace: monitoring
data:
  monitoring_high-error-rate.yaml: |
    name: monitoring-high-error-rate
    type: frequency
    index: logs-*
    timeframe:
      minutes: 5
    num_events: 10
    filter:
      - query:
          query_string:
            query: "level:ERROR"
    alert_text: "High error rate detected!"
```

## Mounting ConfigMap in ElastAlert

Configure your ElastAlert deployment to mount the ConfigMap:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: elastalert
  namespace: monitoring
spec:
  template:
    spec:
      containers:
        - name: elastalert
          image: your-elastalert-image
          volumeMounts:
            - name: rules
              mountPath: /opt/elastalert/rules
              readOnly: true
      volumes:
        - name: rules
          configMap:
            name: elastalert-rules
```

## RBAC Permissions

With `rbac.create=true` the chart creates:

- A ClusterRole for `ElastAlertRules` (get, list, watch, patch, update, status and finalizers) and `events.k8s.io` events. Secret `get` is added only when `secretRefs.enabled=true`.
- One Role per target namespace: `get` and `patch` on the listed ConfigMaps or Secrets (by name), plus `list` and `watch` on ConfigMaps in that namespace for the operator's cache.
- A Role for the ElastAlert Deployment (`get` and `patch` by name) when `elastalert.enabled=true`.
- A Role for leader-election Leases (and their Events) in the release namespace.

## Upgrading

```bash
helm upgrade elastalert-rules-operator \
  oci://ghcr.io/aresiusxp/charts/elastalert-rules-operator \
  --version <new-version> \
  --namespace elastalert-rules-operator-system
```

## Uninstallation

```bash
# Delete all ElastAlertRule resources first
kubectl delete elastalertrules --all --all-namespaces

# Uninstall the chart
helm uninstall elastalert-rules-operator --namespace elastalert-rules-operator-system

# Clean up CRDs (optional)
kubectl delete crd elastalertrules.alerts.example.dev
```

## Troubleshooting

### Check Operator Logs

```bash
kubectl logs -n elastalert-rules-operator-system \
  -l app.kubernetes.io/name=elastalert-rules-operator \
  -f
```

### Check Rule Status

```bash
kubectl get elastalertrules -n monitoring
kubectl describe elastalertrule high-error-rate -n monitoring
```

Failure reasons such as `TargetNotAllowed`, `PolicyViolation`, `KeyConflict` and `DuplicateRuleName` are listed in the [main README](../../README.md#supported-condition-types).

### Verify ConfigMap

First ensure the ConfigMap exists:

```bash
kubectl get configmap elastalert-rules -n monitoring
```

If it doesn't exist, create it:

```bash
kubectl create configmap elastalert-rules -n monitoring
```

Then check the rule was added:

```bash
kubectl get configmap elastalert-rules -n monitoring -o yaml
```

## Examples

See the [main README](../../README.md#examples) for more ElastAlertRule examples.

## Support

- Repository: https://github.com/AresiusXP/elastalert-rules-operator
- Issues: https://github.com/AresiusXP/elastalert-rules-operator/issues

## License

Licensed under the Apache License, Version 2.0.
