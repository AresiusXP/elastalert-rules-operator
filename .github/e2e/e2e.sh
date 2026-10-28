#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${RELEASE_NAMESPACE:-elastalert-system}"
RELEASE="${RELEASE_NAME:-elastalert-rules-operator}"
CHART="${CHART_PATH:-helm/elastalert-rules-operator}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-elastalert-rules-operator}"
IMAGE_TAG="${IMAGE_TAG:-e2e}"
TARGET_CONFIGMAP="${TARGET_CONFIGMAP:-elastalert-rules}"
RULE_NAME="${RULE_NAME:-e2e-sample}"

dump_state() {
  echo "::group::debug"
  kubectl get pods,configmaps -n "${NAMESPACE}" -o wide || true
  kubectl get elastalertrules -A -o yaml || true
  kubectl logs -n "${NAMESPACE}" -l "app.kubernetes.io/name=elastalert-rules-operator" --tail=200 || true
  echo "::endgroup::"
}
trap 'rc=$?; if [ "${rc}" -ne 0 ]; then dump_state; fi; exit "${rc}"' EXIT

configmap_keys() {
  kubectl get configmap "${TARGET_CONFIGMAP}" -n "${NAMESPACE}" -o json |
    jq -r '(.data // {}) | keys[]'
}

wait_for_key() {
  local deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if configmap_keys | grep -E '\.yaml$' | grep -qF "${RULE_NAME}"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_key_removed() {
  local deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if ! configmap_keys | grep -E '\.yaml$' | grep -qF "${RULE_NAME}"; then
      return 0
    fi
    sleep 2
  done
  return 1
}

kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl label --overwrite namespace "${NAMESPACE}" pod-security.kubernetes.io/enforce=restricted
kubectl create configmap "${TARGET_CONFIGMAP}" -n "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install "${RELEASE}" "${CHART}" \
  --namespace "${NAMESPACE}" \
  --set image.repository="${IMAGE_REPOSITORY}" \
  --set image.tag="${IMAGE_TAG}" \
  --set image.pullPolicy=Never \
  --wait --timeout 180s

kubectl apply -n "${NAMESPACE}" -f - <<YAML
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: ${RULE_NAME}
spec:
  type: frequency
  index: logs-*
  num_events: 1
  timeframe:
    minutes: 5
  alert:
    - debug
YAML

kubectl wait --for=condition=Ready "elastalertrule/${RULE_NAME}" -n "${NAMESPACE}" --timeout=120s

echo "Asserting rule key present in ConfigMap ${TARGET_CONFIGMAP}"
wait_for_key || { echo "no .yaml key containing ${RULE_NAME} found" >&2; exit 1; }
configmap_keys | grep -qx "${NAMESPACE}_${RULE_NAME}.yaml" || { echo "expected key ${NAMESPACE}_${RULE_NAME}.yaml" >&2; exit 1; }

condition_reason() {
  kubectl get elastalertrule "$1" -n "${NAMESPACE}" \
    -o jsonpath='{.status.conditions[?(@.type=="ValidationFailed")].reason}'
}

wait_for_reason() {
  local deadline=$((SECONDS + 60))
  while [ "${SECONDS}" -lt "${deadline}" ]; do
    if [ "$(condition_reason "$1")" = "$2" ]; then
      return 0
    fi
    sleep 2
  done
  echo "rule $1 never reported ValidationFailed=$2 (got '$(condition_reason "$1")')" >&2
  return 1
}

echo "Asserting a target outside the allowlist is refused"
kubectl create namespace e2e-other --dry-run=client -o yaml | kubectl apply -f -
kubectl create configmap victim -n e2e-other --from-literal=keep=me --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -n "${NAMESPACE}" -f - <<YAML
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: e2e-forbidden-target
spec:
  type: any
  index: logs-*
  targetConfigMap:
    name: victim
    namespace: e2e-other
YAML
wait_for_reason e2e-forbidden-target TargetNotAllowed
[ "$(kubectl get configmap victim -n e2e-other -o json | jq -c '.data')" = '{"keep":"me"}' ] ||
  { echo "victim ConfigMap was modified" >&2; exit 1; }

echo "Asserting an alerter outside the allowlist is refused"
kubectl apply -n "${NAMESPACE}" -f - <<YAML
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: e2e-command-alerter
spec:
  type: any
  index: logs-*
  alert: [command]
YAML
wait_for_reason e2e-command-alerter PolicyViolation
configmap_keys | grep -q 'e2e-command-alerter' && { echo "rejected rule was written" >&2; exit 1; }

echo "Asserting the API server rejects unknown rule types"
if kubectl apply -n "${NAMESPACE}" -f - <<YAML
apiVersion: alerts.example.dev/v1alpha1
kind: ElastAlertRule
metadata:
  name: e2e-custom-type
spec:
  type: my_module.MyRule
  index: logs-*
YAML
then
  echo "custom rule type was admitted" >&2
  exit 1
fi
kubectl delete elastalertrule e2e-forbidden-target e2e-command-alerter -n "${NAMESPACE}" --timeout=120s

kubectl delete elastalertrule "${RULE_NAME}" -n "${NAMESPACE}" --timeout=120s

echo "Asserting rule key removed from ConfigMap ${TARGET_CONFIGMAP}"
wait_for_key_removed || { echo "key for ${RULE_NAME} still present after delete" >&2; exit 1; }

echo "e2e OK"
