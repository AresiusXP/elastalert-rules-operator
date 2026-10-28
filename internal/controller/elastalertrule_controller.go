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
	"fmt"
	"os"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
)

// ElastAlertRuleReconciler reconciles a ElastAlertRule object
type ElastAlertRuleReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

const (
	// Annotation key for rules checksum on Deployment
	rulesChecksumAnnotation = "alerts.example.dev/rules-checksum"
)

// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=alerts.example.dev,resources=elastalertrules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ElastAlertRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the ElastAlertRule instance
	rule := &alertsv1alpha1.ElastAlertRule{}
	err := r.Get(ctx, req.NamespacedName, rule)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, could have been deleted
			log.Info("ElastAlertRule resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get ElastAlertRule")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(rule, alertsv1alpha1.FinalizerName) {
		controllerutil.AddFinalizer(rule, alertsv1alpha1.FinalizerName)
		if err := r.Update(ctx, rule); err != nil {
			if apierrors.IsConflict(err) {
				// Conflict, requeue for retry
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate the rule
	if err := rule.ValidateSpec(); err != nil {
		log.Error(err, "Validation failed")
		r.recordEvent(rule, corev1.EventTypeWarning, "ValidationFailed", err.Error())
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               alertsv1alpha1.ConditionTypeValidationFailed,
			Status:             metav1.ConditionTrue,
			Reason:             "ValidationFailed",
			Message:            err.Error(),
			ObservedGeneration: rule.Generation,
		})
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               alertsv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ValidationFailed",
			Message:            err.Error(),
			ObservedGeneration: rule.Generation,
		})

		// Refetch rule before updating status
		if statusErr := r.persistStatus(ctx, rule); statusErr != nil {
			if apierrors.IsConflict(statusErr) {
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(statusErr, "Failed to update status")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil
	}

	// Clear validation failed condition if it exists
	meta.RemoveStatusCondition(&rule.Status.Conditions, alertsv1alpha1.ConditionTypeValidationFailed)

	// Reconcile the ConfigMap
	if err := r.reconcileConfigMap(ctx, rule); err != nil {
		// A conflict means another writer updated the ConfigMap concurrently;
		// requeue to retry with a fresh copy rather than flagging an error.
		if apierrors.IsConflict(err) {
			log.Info("ConfigMap update conflict, will retry")
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to reconcile ConfigMap")
		r.recordEvent(rule, corev1.EventTypeWarning, "ReconciliationFailed", err.Error())
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               alertsv1alpha1.ConditionTypeError,
			Status:             metav1.ConditionTrue,
			Reason:             "ReconciliationFailed",
			Message:            err.Error(),
			ObservedGeneration: rule.Generation,
		})
		meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
			Type:               alertsv1alpha1.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             "ReconciliationFailed",
			Message:            err.Error(),
			ObservedGeneration: rule.Generation,
		})

		// Refetch rule before updating status
		if statusErr := r.persistStatus(ctx, rule); statusErr != nil {
			if apierrors.IsConflict(statusErr) {
				// Requeue to retry with fresh resource version
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(statusErr, "Failed to update status")
		}
		return ctrl.Result{}, err
	}

	// Update status to ready
	now := metav1.Now()
	rule.Status.LastReconciled = &now
	meta.RemoveStatusCondition(&rule.Status.Conditions, alertsv1alpha1.ConditionTypeError)
	meta.SetStatusCondition(&rule.Status.Conditions, metav1.Condition{
		Type:               alertsv1alpha1.ConditionTypeReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ReconciliationSucceeded",
		Message:            "Rule successfully written to ConfigMap",
		ObservedGeneration: rule.Generation,
	})

	// Refetch the rule to get the latest resourceVersion before updating status
	if err := r.persistStatus(ctx, rule); err != nil {
		if apierrors.IsNotFound(err) {
			// Rule was deleted between reconcile and status update; nothing to do.
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to update status")
		// Return with requeue - likely a conflict, will be retried
		return ctrl.Result{Requeue: true}, nil
	}

	r.recordEvent(rule, corev1.EventTypeNormal, "Reconciled",
		fmt.Sprintf("Rule written to ConfigMap %s/%s key %q",
			rule.GetConfigMapNamespace(), rule.GetConfigMapName(), rule.GetKeyName()))

	// Trigger rollout if configured
	if err := r.triggerRollout(ctx, rule); err != nil {
		log.Error(err, "Failed to trigger rollout (non-fatal)")
		// Non-fatal error, don't fail reconciliation
	}

	return ctrl.Result{}, nil
}

// persistStatus refetches the rule to obtain the latest resourceVersion, copies
// the in-memory status onto it, and writes the status subresource. It returns
// the error from the status update (which callers can inspect with
// apierrors.IsConflict) or any refetch error.
func (r *ElastAlertRuleReconciler) persistStatus(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	latestRule := &alertsv1alpha1.ElastAlertRule{}
	if err := r.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, latestRule); err != nil {
		return err
	}
	latestRule.Status = rule.Status
	return r.Status().Update(ctx, latestRule)
}

func (r *ElastAlertRuleReconciler) reconcileConfigMap(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	log := logf.FromContext(ctx)

	// Generate the ElastAlert YAML
	yamlContent, err := rule.ToElastAlertYAML()
	if err != nil {
		return fmt.Errorf("failed to convert rule to YAML: %w", err)
	}

	// Get the ConfigMap (must already exist)
	cmName := rule.GetConfigMapName()
	cmNamespace := rule.GetConfigMapNamespace()
	keyName := rule.GetKeyName()

	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: cmName, Namespace: cmNamespace}

	err = r.Get(ctx, cmKey, cm)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("target ConfigMap %s/%s does not exist - please create it first", cmNamespace, cmName)
		}
		return fmt.Errorf("failed to get ConfigMap: %w", err)
	}

	// Patch only the single rule key so concurrent reconciliations targeting
	// the same shared ConfigMap don't clobber each other's keys.
	patch := client.MergeFrom(cm.DeepCopy())
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}
	cm.Data[keyName] = yamlContent

	log.Info("Updating ConfigMap", "name", cmName, "namespace", cmNamespace, "key", keyName)
	if err := r.Patch(ctx, cm, patch); err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	rule.Status.ObservedConfigMapResourceVersion = cm.ResourceVersion
	return nil
}

func (r *ElastAlertRuleReconciler) reconcileDelete(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(rule, alertsv1alpha1.FinalizerName) {
		// Remove the key from the ConfigMap
		cmName := rule.GetConfigMapName()
		cmNamespace := rule.GetConfigMapNamespace()
		keyName := rule.GetKeyName()

		cm := &corev1.ConfigMap{}
		cmKey := types.NamespacedName{Name: cmName, Namespace: cmNamespace}

		err := r.Get(ctx, cmKey, cm)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				log.Error(err, "Failed to get ConfigMap for cleanup")
				return ctrl.Result{}, err
			}
			// ConfigMap doesn't exist, nothing to clean up
		} else {
			// Remove the key
			if cm.Data != nil {
				patch := client.MergeFrom(cm.DeepCopy())
				delete(cm.Data, keyName)
				log.Info("Removing key from ConfigMap", "name", cmName, "namespace", cmNamespace, "key", keyName)
				if err := r.Patch(ctx, cm, patch); err != nil {
					if apierrors.IsConflict(err) {
						// Conflict during patch, requeue
						log.Info("ConfigMap patch conflict during deletion, will retry")
						return ctrl.Result{Requeue: true}, nil
					}
					log.Error(err, "Failed to update ConfigMap during cleanup")
					return ctrl.Result{}, err
				}

				// Trigger rollout after deletion
				if err := r.triggerRollout(ctx, rule); err != nil {
					log.Error(err, "Failed to trigger rollout after deletion (non-fatal)")
				}
			}
		}

		// Refetch the rule to get the latest resourceVersion before removing finalizer
		latestRule := &alertsv1alpha1.ElastAlertRule{}
		if err := r.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, latestRule); err != nil {
			if apierrors.IsNotFound(err) {
				// Resource already gone, nothing to do
				return ctrl.Result{}, nil
			}
			log.Error(err, "Failed to refetch rule before removing finalizer")
			return ctrl.Result{}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(latestRule, alertsv1alpha1.FinalizerName)
		if err := r.Update(ctx, latestRule); err != nil {
			if apierrors.IsNotFound(err) {
				// Resource already deleted, that's fine
				return ctrl.Result{}, nil
			}
			if apierrors.IsConflict(err) {
				// Conflict, requeue for retry
				log.Info("Conflict removing finalizer, will retry")
				return ctrl.Result{Requeue: true}, nil
			}
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *ElastAlertRuleReconciler) triggerRollout(ctx context.Context, rule *alertsv1alpha1.ElastAlertRule) error {
	log := logf.FromContext(ctx)

	// Rollout is opt-in: gated on ELASTALERT_ROLLOUT_ENABLED=true with both
	// target deployment name and namespace provided.
	if os.Getenv("ELASTALERT_ROLLOUT_ENABLED") != "true" {
		return nil
	}

	deploymentName := os.Getenv("ELASTALERT_DEPLOYMENT_NAME")
	deploymentNamespace := os.Getenv("ELASTALERT_DEPLOYMENT_NAMESPACE")

	if deploymentName == "" || deploymentNamespace == "" {
		// Rollout enabled but target not fully configured
		return nil
	}

	// Get the ConfigMap to calculate checksum
	cmName := rule.GetConfigMapName()
	cmNamespace := rule.GetConfigMapNamespace()

	cm := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{Name: cmName, Namespace: cmNamespace}

	if err := r.Get(ctx, cmKey, cm); err != nil {
		return fmt.Errorf("failed to get ConfigMap for checksum calculation: %w", err)
	}

	// Calculate checksum of all rule keys
	checksum := calculateConfigMapChecksum(cm)

	// Get the Deployment
	deployment := &appsv1.Deployment{}
	deployKey := types.NamespacedName{Name: deploymentName, Namespace: deploymentNamespace}

	if err := r.Get(ctx, deployKey, deployment); err != nil {
		return fmt.Errorf("failed to get Deployment: %w", err)
	}

	// Check if checksum has changed
	currentChecksum := ""
	if deployment.Spec.Template.Annotations != nil {
		currentChecksum = deployment.Spec.Template.Annotations[rulesChecksumAnnotation]
	}

	if currentChecksum == checksum {
		// Checksum unchanged, no rollout needed
		return nil
	}

	// Update the annotation
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations[rulesChecksumAnnotation] = checksum

	log.Info("Triggering deployment rollout", "deployment", deploymentName, "namespace", deploymentNamespace, "checksum", checksum)
	if err := r.Update(ctx, deployment); err != nil {
		return fmt.Errorf("failed to update Deployment: %w", err)
	}

	r.recordEvent(rule, corev1.EventTypeNormal, "RolloutTriggered",
		fmt.Sprintf("Triggered rollout of deployment %s/%s", deploymentNamespace, deploymentName))

	return nil
}

// recordEvent emits a Kubernetes event for the rule when a Recorder is configured.
// It is nil-safe so unit tests can construct the reconciler without a Recorder.
func (r *ElastAlertRuleReconciler) recordEvent(rule *alertsv1alpha1.ElastAlertRule, eventType, reason, message string) {
	if r.Recorder == nil {
		return
	}
	r.Recorder.Event(rule, eventType, reason, message)
}

func calculateConfigMapChecksum(cm *corev1.ConfigMap) string {
	// Sort keys for deterministic checksum
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Calculate SHA256
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(cm.Data[k]))
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// SetupWithManager sets up the controller with the Manager.
func (r *ElastAlertRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&alertsv1alpha1.ElastAlertRule{}).
		// The target ConfigMap is shared and pre-existing (no owner reference is
		// set), so watch ConfigMaps explicitly and map changes back to the rules
		// that target them rather than relying on Owns().
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findElastAlertRulesForConfigMap),
		).
		Named("elastalertrule").
		Complete(r)
}

// findElastAlertRulesForConfigMap returns reconcile requests for ElastAlertRules
// that target a given ConfigMap
func (r *ElastAlertRuleReconciler) findElastAlertRulesForConfigMap(ctx context.Context, cm client.Object) []reconcile.Request {
	rules := &alertsv1alpha1.ElastAlertRuleList{}
	if err := r.List(ctx, rules); err != nil {
		return []reconcile.Request{}
	}

	requests := []reconcile.Request{}
	for _, rule := range rules.Items {
		if rule.GetConfigMapName() == cm.GetName() && rule.GetConfigMapNamespace() == cm.GetNamespace() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				},
			})
		}
	}

	return requests
}
