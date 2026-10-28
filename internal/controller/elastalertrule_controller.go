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

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
	"github.com/AresiusXP/elastalert-rules-operator/internal/policy"
)

const (
	rulesChecksumAnnotation = "alerts.example.dev/rules-checksum"
	// SourceLabel must be set to "true" on Secrets that rules may reference in secretRefs.
	SourceLabel = "alerts.example.dev/allow-source"

	fieldOwnerPrefix = "elastalert-rules-operator/"
	targetIndex      = "spec.target"
	identityIndex    = "spec.ruleIdentity"

	retryShort = 30 * time.Second
	retryLong  = time.Minute
)

// ElastAlertRuleReconciler reconciles a ElastAlertRule object
type ElastAlertRuleReconciler struct {
	client.Client
	// APIReader reads targets, source Secrets and the rollout Deployment without a cache.
	// Falls back to Client when nil.
	APIReader client.Reader
	Scheme    *runtime.Scheme
	Recorder  events.EventRecorder
	Policy    policy.Policy
	// Rollout is the Deployment annotated with a rules checksum after each change. Nil disables rollouts.
	Rollout *types.NamespacedName
	// EnableSecretRefs allows rules to read Secrets through spec.secretRefs.
	EnableSecretRefs bool
	// SecretResync is how often rules that depend on Secrets are re-rendered.
	SecretResync time.Duration

	indexed bool
}

// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules,verbs=get;list;watch;patch;update
// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules/finalizers,verbs=update
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile moves the target ConfigMap or Secret towards the rule's desired state.
func (r *ElastAlertRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	rule := &alertsv1alpha1.ElastAlertRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule)
	}

	if !controllerutil.ContainsFinalizer(rule, alertsv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(rule, alertsv1alpha1.FinalizerName)
		if err := r.Update(ctx, rule); err != nil {
			if apierrors.IsConflict(err) {
				return ctrl.Result{RequeueAfter: time.Second}, nil
			}
			return ctrl.Result{}, err
		}
	}

	if err := r.validate(ctx, rule); err != nil {
		log.Info("Rule rejected", "reason", err.reason, "error", err.Error())
		return r.reject(ctx, rule, err.reason, err)
	}

	resolved, err := r.resolveSecretRefs(ctx, rule)
	if err != nil {
		return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeError, "SecretRefFailed", err, retryLong, false)
	}

	content, verr := r.render(rule, resolved)
	if verr != nil {
		return r.reject(ctx, rule, verr.reason, verr)
	}
	checksum := sha256Hex(content)

	if err := r.applyTarget(ctx, rule, content); err != nil {
		switch {
		case apierrors.IsConflict(err):
			return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeError, "KeyConflict",
				fmt.Errorf("key %q is owned by another writer: %w", rule.GetKeyName(), err), retryLong, false)
		case errors.Is(err, errTargetNotFound):
			return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeError, "TargetNotFound", err, retryShort, false)
		default:
			return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeError, "ReconciliationFailed", err, 0, true)
		}
	}

	if err := r.markReady(ctx, rule, checksum); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.triggerRollout(ctx, rule); err != nil {
		log.Error(err, "Failed to trigger rollout (non-fatal)")
	}

	if r.dependsOnSecrets(rule) {
		return ctrl.Result{RequeueAfter: r.secretResync()}, nil
	}
	return ctrl.Result{}, nil
}

// reject records a validation or policy failure and withdraws anything the rule
// wrote earlier, so a rule that stops being acceptable does not keep running.
func (r *ElastAlertRuleReconciler) reject(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule, reason string, cause error) (ctrl.Result, error) {
	var retry time.Duration
	if reason == "DuplicateRuleName" {
		retry = retryLong
	}
	if r.Policy.CheckTarget(rule) == nil {
		err := r.applyTarget(ctx, rule, "")
		switch {
		case err == nil:
			rule.Status.RuleChecksum = ""
			if err := r.triggerRollout(ctx, rule); err != nil {
				logf.FromContext(ctx).Error(err, "Failed to trigger rollout after withdrawal (non-fatal)")
			}
		case !errors.Is(err, errTargetNotFound):
			return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeValidationFailed, reason,
				fmt.Errorf("%w; withdrawing the rule from its target failed: %s", cause, apierrors.ReasonForError(err)), 0, true)
		}
	}
	return r.fail(ctx, rule, alertsv1alpha1.ConditionTypeValidationFailed, reason, cause, retry, false)
}

// render builds the final rule, checks it against the policy and marshals it.
func (r *ElastAlertRuleReconciler) render(rule *alertsv1alpha1.ElastAlertRule, resolved map[string]interface{}) (string, *validationError) {
	built, err := rule.BuildRule(resolved)
	if err != nil {
		return "", &validationError{"RenderFailed", err}
	}
	if err := r.Policy.CheckRendered(built); err != nil {
		return "", &validationError{"PolicyViolation", err}
	}
	content, err := alertsv1alpha1.MarshalRule(built)
	if err != nil {
		return "", &validationError{"RenderFailed", err}
	}
	return content, nil
}

type validationError struct {
	reason string
	err    error
}

func (e *validationError) Error() string { return e.err.Error() }
func (e *validationError) Unwrap() error { return e.err }

func (r *ElastAlertRuleReconciler) validate(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) *validationError {
	if err := rule.ValidateSpec(); err != nil {
		return &validationError{"ValidationFailed", err}
	}
	if err := r.Policy.CheckTarget(rule); err != nil {
		return &validationError{"TargetNotAllowed", err}
	}
	if err := r.Policy.CheckRule(rule); err != nil {
		return &validationError{"PolicyViolation", err}
	}
	if err := r.checkDuplicate(ctx, rule); err != nil {
		return &validationError{"DuplicateRuleName", err}
	}
	return nil
}

// checkDuplicate rejects the rule when an older rule renders the same ElastAlert
// name for the same target, because ElastAlert skips duplicate rule names.
func (r *ElastAlertRuleReconciler) checkDuplicate(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	others, err := r.listRules(ctx, identityIndex, identityKey(rule))
	if err != nil {
		return nil
	}
	for i := range others {
		o := &others[i]
		if o.UID == rule.UID || !o.DeletionTimestamp.IsZero() || identityKey(o) != identityKey(rule) {
			continue
		}
		if olderThan(o, rule) {
			return fmt.Errorf("rule name %q is already used by %s/%s for the same target", rule.GetRuleName(), o.Namespace, o.Name)
		}
	}
	return nil
}

func olderThan(a, b *alertsv1alpha1.ElastAlertRule) bool {
	if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
		return a.CreationTimestamp.Before(&b.CreationTimestamp)
	}
	return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
}

func targetKey(rule *alertsv1alpha1.ElastAlertRule) string {
	return rule.GetTargetKind() + "/" + rule.GetConfigMapNamespace() + "/" + rule.GetConfigMapName()
}

func identityKey(rule *alertsv1alpha1.ElastAlertRule) string {
	return targetKey(rule) + "/" + rule.GetRuleName()
}

func (r *ElastAlertRuleReconciler) listRules(ctx context.Context, index, value string) ([]alertsv1alpha1.ElastAlertRule, error) {
	list := &alertsv1alpha1.ElastAlertRuleList{}
	var opts []client.ListOption
	if r.indexed {
		opts = append(opts, client.MatchingFields{index: value})
	}
	if err := r.List(ctx, list, opts...); err != nil {
		return nil, err
	}
	if r.indexed {
		return list.Items, nil
	}
	var out []alertsv1alpha1.ElastAlertRule
	for i := range list.Items {
		key := identityKey(&list.Items[i])
		if index == targetIndex {
			key = targetKey(&list.Items[i])
		}
		if key == value {
			out = append(out, list.Items[i])
		}
	}
	return out, nil
}

func (r *ElastAlertRuleReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

func (r *ElastAlertRuleReconciler) dependsOnSecrets(rule *alertsv1alpha1.ElastAlertRule) bool {
	return len(rule.Spec.SecretRefs) > 0 || rule.GetTargetKind() == alertsv1alpha1.TargetKindSecret
}

func (r *ElastAlertRuleReconciler) secretResync() time.Duration {
	if r.SecretResync > 0 {
		return r.SecretResync
	}
	return 5 * time.Minute
}

// resolveSecretRefs reads the Secret values for spec.secretRefs.
func (r *ElastAlertRuleReconciler) resolveSecretRefs(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) (map[string]interface{}, error) {
	if len(rule.Spec.SecretRefs) == 0 {
		return nil, nil
	}
	if !r.EnableSecretRefs {
		return nil, errors.New("secretRefs are disabled; start the operator with --enable-secret-refs")
	}
	resolved := make(map[string]interface{}, len(rule.Spec.SecretRefs))
	for _, ref := range rule.Spec.SecretRefs {
		if r.Policy.TargetAllowed(alertsv1alpha1.TargetKindSecret, rule.Namespace, ref.Name) {
			return nil, fmt.Errorf("secret %q is a write target and cannot be used as a secretRef source", ref.Name)
		}
		secret := &corev1.Secret{}
		if err := r.reader().Get(ctx, types.NamespacedName{Namespace: rule.Namespace, Name: ref.Name}, secret); err != nil {
			return nil, fmt.Errorf("secretRef %q: %w", ref.Name, err)
		}
		if secret.Labels[SourceLabel] != "true" {
			return nil, fmt.Errorf("secret %q must carry the label %s=true", ref.Name, SourceLabel)
		}
		raw, ok := secret.Data[ref.Key]
		if !ok {
			return nil, fmt.Errorf("secret %q has no key %q", ref.Name, ref.Key)
		}
		if ref.Format == "yaml" {
			var value interface{}
			if err := yaml.Unmarshal(raw, &value); err != nil {
				return nil, fmt.Errorf("secret %q key %q is not valid YAML", ref.Name, ref.Key)
			}
			resolved[ref.Field] = value
		} else {
			resolved[ref.Field] = string(raw)
		}
	}
	return resolved, nil
}

var errTargetNotFound = errors.New("target does not exist, create it first")

func fieldOwner(rule *alertsv1alpha1.ElastAlertRule) client.FieldOwner {
	return client.FieldOwner(fieldOwnerPrefix + string(rule.UID))
}

// applyTarget writes the rule key with server-side apply. Every rule has its
// own field manager, so a key owned by another rule or writer is reported as a
// conflict instead of being overwritten. An empty content removes the key.
func (r *ElastAlertRuleReconciler) applyTarget(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule, content string) error {
	name, ns, key := rule.GetConfigMapName(), rule.GetConfigMapNamespace(), rule.GetKeyName()
	nn := types.NamespacedName{Namespace: ns, Name: name}

	var applyConfig runtime.ApplyConfiguration
	if rule.GetTargetKind() == alertsv1alpha1.TargetKindSecret {
		if err := r.reader().Get(ctx, nn, &corev1.Secret{}); err != nil {
			return targetGetError(err, ns, name)
		}
		ac := corev1ac.Secret(name, ns)
		if content != "" {
			ac.WithData(map[string][]byte{key: []byte(content)})
		}
		applyConfig = ac
	} else {
		if err := r.reader().Get(ctx, nn, &corev1.ConfigMap{}); err != nil {
			return targetGetError(err, ns, name)
		}
		ac := corev1ac.ConfigMap(name, ns)
		if content != "" {
			ac.WithData(map[string]string{key: content})
		}
		applyConfig = ac
	}

	logf.FromContext(ctx).Info("Applying rule to target", "kind", rule.GetTargetKind(), "namespace", ns, "name", name, "key", key)
	return r.Apply(ctx, applyConfig, fieldOwner(rule))
}

func targetGetError(err error, ns, name string) error {
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("%s/%s: %w", ns, name, errTargetNotFound)
	}
	return fmt.Errorf("failed to get target %s/%s: %w", ns, name, err)
}

// markReady records success. The Reconciled event and lastReconciled are only
// updated when the rendered rule changed.
func (r *ElastAlertRuleReconciler) markReady(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule, checksum string) error {
	old := rule.Status.DeepCopy()
	changed := rule.Status.RuleChecksum != checksum
	wasReady := meta.IsStatusConditionTrue(rule.Status.Conditions, alertsv1alpha1.ConditionTypeReady)

	meta.RemoveStatusCondition(&rule.Status.Conditions, alertsv1alpha1.ConditionTypeValidationFailed)
	meta.RemoveStatusCondition(&rule.Status.Conditions, alertsv1alpha1.ConditionTypeError)
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               alertsv1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ReconciliationSucceeded",
		Message:            "Rule successfully written to the target",
		ObservedGeneration: rule.Generation,
	})
	if rule.HasPlaintextCredentials() {
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               alertsv1alpha1.ConditionTypePlaintextCredentials,
			Status:             metav1.ConditionTrue,
			Reason:             "InlineCredentials",
			Message:            "Credentials are stored in the resource spec; use spec.secretRefs instead",
			ObservedGeneration: rule.Generation,
		})
	} else {
		meta.RemoveStatusCondition(&rule.Status.Conditions, alertsv1alpha1.ConditionTypePlaintextCredentials)
	}

	rule.Status.ObservedGeneration = rule.Generation
	rule.Status.RuleChecksum = checksum
	if changed || !wasReady || rule.Status.LastReconciled == nil {
		now := metav1.Now()
		rule.Status.LastReconciled = &now
	}

	if equality.Semantic.DeepEqual(old, &rule.Status) {
		return nil
	}
	if err := r.persistStatus(ctx, rule); err != nil {
		return err
	}
	if changed || !wasReady {
		r.recordEvent(rule, corev1.EventTypeNormal, "Reconciled",
			fmt.Sprintf("Rule written to %s %s/%s key %q",
				rule.GetTargetKind(), rule.GetConfigMapNamespace(), rule.GetConfigMapName(), rule.GetKeyName()))
	}
	return nil
}

// fail records a failure on the status. With transient the error is returned so
// the controller retries with backoff; otherwise the rule is retried after
// retry, or not at all when retry is zero.
func (r *ElastAlertRuleReconciler) fail(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule, condType, reason string, cause error, retry time.Duration, transient bool) (ctrl.Result, error) {
	old := rule.Status.DeepCopy()
	previous := meta.FindStatusCondition(rule.Status.Conditions, condType)

	other := alertsv1alpha1.ConditionTypeError
	if condType == alertsv1alpha1.ConditionTypeError {
		other = alertsv1alpha1.ConditionTypeValidationFailed
	}
	meta.RemoveStatusCondition(&rule.Status.Conditions, other)
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type: condType, Status: metav1.ConditionTrue, Reason: reason,
		Message: cause.Error(), ObservedGeneration: rule.Generation,
	})
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type: alertsv1alpha1.ConditionTypeReady, Status: metav1.ConditionFalse, Reason: reason,
		Message: cause.Error(), ObservedGeneration: rule.Generation,
	})
	rule.Status.ObservedGeneration = rule.Generation

	if !equality.Semantic.DeepEqual(old, &rule.Status) {
		if err := r.persistStatus(ctx, rule); err != nil && !apierrors.IsNotFound(err) {
			logf.FromContext(ctx).Error(err, "Failed to update status")
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
	}
	if previous == nil || previous.Reason != reason || previous.Message != cause.Error() {
		r.recordEvent(rule, corev1.EventTypeWarning, reason, cause.Error())
	}

	if transient {
		return ctrl.Result{}, cause
	}
	return ctrl.Result{RequeueAfter: retry}, nil
}

// persistStatus refetches the rule to obtain the latest resourceVersion, copies
// the in-memory status onto it, and writes the status subresource.
func (r *ElastAlertRuleReconciler) persistStatus(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	latest := &alertsv1alpha1.ElastAlertRule{}
	if err := r.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, latest); err != nil {
		return err
	}
	latest.Status = rule.Status
	return r.Status().Update(ctx, latest)
}

func (r *ElastAlertRuleReconciler) reconcileDelete(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(rule, alertsv1alpha1.FinalizerName) {
		return ctrl.Result{}, nil
	}

	if r.Policy.CheckTarget(rule) == nil {
		err := r.applyTarget(ctx, rule, "")
		if err != nil && !errors.Is(err, errTargetNotFound) {
			log.Error(err, "Failed to remove rule from target")
			return ctrl.Result{}, err
		}
		if err == nil {
			if err := r.triggerRollout(ctx, rule); err != nil {
				log.Error(err, "Failed to trigger rollout after deletion (non-fatal)")
			}
		}
	}

	patch := client.MergeFrom(rule.DeepCopy())
	controllerutil.RemoveFinalizer(rule, alertsv1alpha1.FinalizerName)
	if err := r.Patch(ctx, rule, patch); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// triggerRollout annotates the ElastAlert pod template with a checksum of the
// target so the Deployment restarts when rules change.
func (r *ElastAlertRuleReconciler) triggerRollout(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	if r.Rollout == nil {
		return nil
	}

	checksum, err := r.targetChecksum(ctx)
	if err != nil {
		return fmt.Errorf("failed to calculate target checksum: %w", err)
	}

	deployment := &appsv1.Deployment{}
	if err := r.reader().Get(ctx, *r.Rollout, deployment); err != nil {
		return fmt.Errorf("failed to get Deployment: %w", err)
	}
	if deployment.Spec.Template.Annotations[rulesChecksumAnnotation] == checksum {
		return nil
	}

	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{%q:%q}}}}}`, rulesChecksumAnnotation, checksum)
	logf.FromContext(ctx).Info("Triggering deployment rollout", "deployment", r.Rollout.String(), "checksum", checksum)
	if err := r.Patch(ctx, deployment, client.RawPatch(types.StrategicMergePatchType, []byte(patch))); err != nil {
		return fmt.Errorf("failed to patch Deployment: %w", err)
	}

	r.recordEvent(rule, corev1.EventTypeNormal, "RolloutTriggered",
		fmt.Sprintf("Triggered rollout of deployment %s", r.Rollout.String()))
	return nil
}

// targetChecksum hashes every allowed target, so one Deployment annotation
// stays stable when several targets exist.
func (r *ElastAlertRuleReconciler) targetChecksum(ctx context.Context) (string, error) {
	targets := append([]policy.Target(nil), r.Policy.Targets...)
	sort.Slice(targets, func(i, j int) bool { return targets[i].String() < targets[j].String() })

	data := map[string][]byte{}
	for _, t := range targets {
		nn := types.NamespacedName{Namespace: t.Namespace, Name: t.Name}
		prefix := t.String() + "\x00"
		if t.Kind == alertsv1alpha1.TargetKindSecret {
			secret := &corev1.Secret{}
			if err := r.reader().Get(ctx, nn, secret); err != nil {
				if apierrors.IsNotFound(err) {
					continue
				}
				return "", err
			}
			for k, v := range secret.Data {
				data[prefix+k] = v
			}
			continue
		}
		cm := &corev1.ConfigMap{}
		if err := r.reader().Get(ctx, nn, cm); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return "", err
		}
		for k, v := range cm.Data {
			data[prefix+k] = []byte(v)
		}
	}
	return checksumOf(data), nil
}

func checksumOf(data map[string][]byte) string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(data[k])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// recordEvent emits an Event for the rule when a Recorder is configured.
func (r *ElastAlertRuleReconciler) recordEvent(rule *alertsv1alpha1.ElastAlertRule, eventType, reason, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Eventf(rule, nil, eventType, reason, reason, "%s", message)
}

// SetupWithManager sets up the controller with the Manager.
func (r *ElastAlertRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()
	idx := mgr.GetFieldIndexer()
	if err := idx.IndexField(ctx, &alertsv1alpha1.ElastAlertRule{}, targetIndex, func(o client.Object) []string {
		return []string{targetKey(o.(*alertsv1alpha1.ElastAlertRule))}
	}); err != nil {
		return err
	}
	if err := idx.IndexField(ctx, &alertsv1alpha1.ElastAlertRule{}, identityIndex, func(o client.Object) []string {
		return []string{identityKey(o.(*alertsv1alpha1.ElastAlertRule))}
	}); err != nil {
		return err
	}
	r.indexed = true

	b := ctrl.NewControllerManagedBy(mgr).
		For(&alertsv1alpha1.ElastAlertRule{}, builder.WithPredicates(ruleChanged())).
		Named("elastalertrule")

	if len(r.Policy.Namespaces(alertsv1alpha1.TargetKindConfigMap)) > 0 {
		b = b.Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findRulesForConfigMap),
			builder.WithPredicates(configMapDataChanged()))
	}
	return b.Complete(r)
}

// ruleChanged filters out status-only updates.
func ruleChanged() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew != nil && e.ObjectNew.GetDeletionTimestamp() != nil
		}},
	)
}

func configMapDataChanged() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		oldCM, ok1 := e.ObjectOld.(*corev1.ConfigMap)
		newCM, ok2 := e.ObjectNew.(*corev1.ConfigMap)
		return !ok1 || !ok2 || !reflect.DeepEqual(oldCM.Data, newCM.Data)
	}}
}

// findRulesForConfigMap maps a ConfigMap change to the rules that target it.
func (r *ElastAlertRuleReconciler) findRulesForConfigMap(ctx context.Context, cm client.Object) []reconcile.Request {
	key := alertsv1alpha1.TargetKindConfigMap + "/" + cm.GetNamespace() + "/" + cm.GetName()
	rules, err := r.listRules(ctx, targetIndex, key)
	if err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(rules))
	for i := range rules {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: rules[i].Name, Namespace: rules[i].Namespace},
		})
	}
	return requests
}
