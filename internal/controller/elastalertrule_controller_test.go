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
	"encoding/json"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
)

// Helper function to create timeframe RawExtension for 5 minutes
func timeframe5Minutes() *runtime.RawExtension {
	data, _ := json.Marshal(map[string]int{"minutes": 5})
	return &runtime.RawExtension{Raw: data}
}

var _ = Describe("ElastAlertRule Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName = "test-rule"
			namespace    = "default"
			timeout      = time.Second * 10
			interval     = time.Millisecond * 250
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}

		BeforeEach(func() {
			// Ensure clean state before each test
			// Try to delete any existing resource
			resource := &alertsv1alpha1.ElastAlertRule{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				// Remove finalizer to allow immediate deletion
				if controllerutil.ContainsFinalizer(resource, alertsv1alpha1.FinalizerName) {
					patch := client.MergeFrom(resource.DeepCopy())
					controllerutil.RemoveFinalizer(resource, alertsv1alpha1.FinalizerName)
					_ = k8sClient.Patch(ctx, resource, patch)
				}
				_ = k8sClient.Delete(ctx, resource)
			}

			// Clean up ConfigMap if it exists
			cm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}
			err = k8sClient.Get(ctx, cmKey, cm)
			if err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}

			// Wait for all resources to be fully deleted
			Eventually(func() bool {
				resourceGone := apierrors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &alertsv1alpha1.ElastAlertRule{}))
				cmGone := apierrors.IsNotFound(k8sClient.Get(ctx, cmKey, &corev1.ConfigMap{}))
				return resourceGone && cmGone
			}, timeout, interval).Should(BeTrue(), "Resources should be deleted before test starts")
		})

		AfterEach(func() {
			// Clean up - remove finalizer first to allow deletion
			resource := &alertsv1alpha1.ElastAlertRule{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				if controllerutil.ContainsFinalizer(resource, alertsv1alpha1.FinalizerName) {
					controllerutil.RemoveFinalizer(resource, alertsv1alpha1.FinalizerName)
					_ = k8sClient.Update(ctx, resource)
				}
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should successfully reconcile and update ConfigMap key", func() {
			By("Creating the target ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile updates the ConfigMap
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the ConfigMap was updated with the rule")
			updatedCm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}
			Eventually(func() error {
				return k8sClient.Get(ctx, cmKey, updatedCm)
			}, timeout, interval).Should(Succeed())

			expectedKey := resourceName + ".yaml"
			Expect(updatedCm.Data).To(HaveKey(expectedKey))
			Expect(updatedCm.Data[expectedKey]).To(ContainSubstring("name: test-rule"))
			Expect(updatedCm.Data[expectedKey]).To(ContainSubstring("type: frequency"))
			Expect(updatedCm.Data[expectedKey]).To(ContainSubstring("index: logs-*"))

			By("Checking the status was updated")
			updatedResource := &alertsv1alpha1.ElastAlertRule{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedResource)
				if err != nil {
					return false
				}
				readyCondition := meta.FindStatusCondition(updatedResource.Status.Conditions, alertsv1alpha1.ConditionTypeReady)
				return readyCondition != nil && readyCondition.Status == metav1.ConditionTrue
			}, timeout, interval).Should(BeTrue())
		})

		It("should update ConfigMap key when rule is updated", func() {
			By("Creating the target ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile updates ConfigMap
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating the rule")
			updatedResource := &alertsv1alpha1.ElastAlertRule{}
			Eventually(func() error {
				return k8sClient.Get(ctx, typeNamespacedName, updatedResource)
			}, timeout, interval).Should(Succeed())

			newNumEvents := 20
			updatedResource.Spec.NumEvents = &newNumEvents
			updatedResource.Spec.AlertText = "Test alert"
			Expect(k8sClient.Update(ctx, updatedResource)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the ConfigMap was updated")
			updatedCm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}
			Eventually(func() string {
				err := k8sClient.Get(ctx, cmKey, updatedCm)
				if err != nil {
					return ""
				}
				expectedKey := resourceName + ".yaml"
				return updatedCm.Data[expectedKey]
			}, timeout, interval).Should(ContainSubstring("num_events: 20"))

			expectedKey := resourceName + ".yaml"
			Expect(updatedCm.Data[expectedKey]).To(ContainSubstring("alert_text: Test alert"))
		})

		It("should remove ConfigMap key when rule is deleted", func() {
			By("Creating the target ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile updates ConfigMap
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap key exists")
			updatedCm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}
			Eventually(func() error {
				return k8sClient.Get(ctx, cmKey, updatedCm)
			}, timeout, interval).Should(Succeed())

			expectedKey := resourceName + ".yaml"
			Expect(updatedCm.Data).To(HaveKey(expectedKey))

			By("Deleting the ElastAlertRule")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the ConfigMap key was removed")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cmKey, updatedCm)
				if err != nil {
					return false
				}
				_, exists := updatedCm.Data[expectedKey]
				return !exists
			}, timeout, interval).Should(BeTrue())
		})

		It("should set validation failed condition for invalid spec", func() {
			By("Creating an ElastAlertRule with frequency type but missing numEvents")
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "test-*",
					Timeframe: timeframe5Minutes(),
					// Missing numEvents which is required for frequency type
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			By("Reconciling the created resource")
			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile should detect validation failure
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking validation failed condition is set")
			updatedResource := &alertsv1alpha1.ElastAlertRule{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedResource)
				if err != nil {
					return false
				}
				validationCondition := meta.FindStatusCondition(updatedResource.Status.Conditions, alertsv1alpha1.ConditionTypeValidationFailed)
				return validationCondition != nil && validationCondition.Status == metav1.ConditionTrue
			}, timeout, interval).Should(BeTrue())
		})

		It("should preserve other ConfigMap keys", func() {
			By("Creating a ConfigMap with existing data")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{
					"existing.rule.yaml": "name: existing\ntype: any\nindex: test-*",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile updates ConfigMap
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking both keys exist in ConfigMap")
			updatedCm := &corev1.ConfigMap{}
			cmKey := types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, cmKey, updatedCm)
				if err != nil {
					return false
				}
				newKey := resourceName + ".yaml"
				return len(updatedCm.Data) == 2 &&
					updatedCm.Data["existing.rule.yaml"] != "" &&
					updatedCm.Data[newKey] != ""
			}, timeout, interval).Should(BeTrue())
		})

		It("should fail when target ConfigMap does not exist", func() {
			By("Creating an ElastAlertRule without creating the ConfigMap first")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile should fail because ConfigMap doesn't exist
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("target ConfigMap"))
			Expect(err.Error()).To(ContainSubstring("does not exist"))

			By("Checking the status shows an error condition")
			updatedResource := &alertsv1alpha1.ElastAlertRule{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, typeNamespacedName, updatedResource)
				if err != nil {
					return false
				}
				errorCondition := meta.FindStatusCondition(updatedResource.Status.Conditions, alertsv1alpha1.ConditionTypeError)
				return errorCondition != nil && errorCondition.Status == metav1.ConditionTrue
			}, timeout, interval).Should(BeTrue())
		})

		It("should create ConfigMap with Opsgenie fields", func() {
			By("Creating a ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating an ElastAlertRule with Opsgenie configuration")
			numEvents := 1
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "opsgenie-test-rule",
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:             "frequency",
					Index:            "logs-*",
					Timeframe:        timeframe5Minutes(),
					NumEvents:        &numEvents,
					AlertText:        "{0}: {1}",
					AlertTextArgs:    []string{"component", "message"},
					Alert:            []string{"opsgenie"},
					OpsgenieKey:      "test-key",
					OpsgenieMessage:  "Test message",
					OpsgeniePriority: "P1",
					OpsgenieTags:     []string{"test", "critical"},
					OpsgenieTeams:    []string{"ops-team"},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "opsgenie-test-rule", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Second reconcile updates ConfigMap
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "opsgenie-test-rule", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking ConfigMap contains Opsgenie fields")
			updatedCm := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}, updatedCm)
				if err != nil {
					return false
				}
				key := "opsgenie-test-rule.yaml"
				if ruleYaml, exists := updatedCm.Data[key]; exists {
					return strings.Contains(ruleYaml, "opsgenie_key: test-key") &&
						strings.Contains(ruleYaml, "opsgenie_message: Test message") &&
						strings.Contains(ruleYaml, "opsgenie_priority: P1") &&
						strings.Contains(ruleYaml, "opsgenie_teams:") &&
						strings.Contains(ruleYaml, "- ops-team") &&
						strings.Contains(ruleYaml, "opsgenie_tags:") &&
						strings.Contains(ruleYaml, "- test") &&
						strings.Contains(ruleYaml, "- critical") &&
						strings.Contains(ruleYaml, "alert_text_args:") &&
						strings.Contains(ruleYaml, "- component")
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should create ConfigMap with filter list", func() {
			By("Creating a ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating an ElastAlertRule with filter list")
			numEvents := 5
			filter1, _ := json.Marshal(map[string]interface{}{
				"term": map[string]string{"level": "error"},
			})
			filter2, _ := json.Marshal(map[string]interface{}{
				"range": map[string]interface{}{
					"timestamp": map[string]string{"gte": "now-1h"},
				},
			})

			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "filter-test-rule",
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					NumEvents: &numEvents,
					Filter: []runtime.RawExtension{
						{Raw: filter1},
						{Raw: filter2},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "filter-test-rule", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "filter-test-rule", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking ConfigMap contains filter list")
			updatedCm := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}, updatedCm)
				if err != nil {
					return false
				}
				key := "filter-test-rule.yaml"
				if ruleYaml, exists := updatedCm.Data[key]; exists {
					return strings.Contains(ruleYaml, "filter:") &&
						strings.Contains(ruleYaml, "level: error") &&
						strings.Contains(ruleYaml, "timestamp:")
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should use custom spec.name in ConfigMap", func() {
			By("Creating a ConfigMap first")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			By("Creating an ElastAlertRule with custom name")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cr-name",
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					NumEvents: &numEvents,
					Name:      "[team-platform] Custom Rule Name",
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// Reconcile
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "cr-name", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "cr-name", Namespace: namespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking ConfigMap uses CR name for key but custom name in YAML")
			updatedCm := &corev1.ConfigMap{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: "elastalert-rules", Namespace: namespace}, updatedCm)
				if err != nil {
					return false
				}
				key := "cr-name.yaml"
				if ruleYaml, exists := updatedCm.Data[key]; exists {
					// Should use custom name in YAML, not CR name
					return strings.Contains(ruleYaml, "name: '[team-platform] Custom Rule Name'") &&
						!strings.Contains(ruleYaml, "name: cr-name")
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should trigger deployment rollout when rollout is enabled", func() {
			const deploymentName = "elastalert"

			By("Enabling rollout via environment variables")
			GinkgoT().Setenv("ELASTALERT_ROLLOUT_ENABLED", "true")
			GinkgoT().Setenv("ELASTALERT_DEPLOYMENT_NAME", deploymentName)
			GinkgoT().Setenv("ELASTALERT_DEPLOYMENT_NAMESPACE", namespace)

			By("Creating the target ConfigMap and Deployment")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": deploymentName},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": deploymentName},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "elastalert", Image: "elastalert:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, deployment)
			})

			By("Creating and reconciling the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			// First reconcile adds finalizer, second writes ConfigMap + triggers rollout
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the deployment received the rules-checksum annotation")
			deployKey := types.NamespacedName{Name: deploymentName, Namespace: namespace}
			var firstChecksum string
			Eventually(func() string {
				updated := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, deployKey, updated); err != nil {
					return ""
				}
				firstChecksum = updated.Spec.Template.Annotations[rulesChecksumAnnotation]
				return firstChecksum
			}, timeout, interval).ShouldNot(BeEmpty())

			By("Verifying an unchanged checksum does not rewrite the deployment")
			beforeRV := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, deployKey, beforeRV)).To(Succeed())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			afterRV := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, deployKey, afterRV)).To(Succeed())
			Expect(afterRV.ResourceVersion).To(Equal(beforeRV.ResourceVersion),
				"deployment should not be updated when checksum is unchanged")
		})

		It("should not trigger rollout when rollout is disabled", func() {
			const deploymentName = "elastalert-disabled"

			By("Explicitly disabling rollout")
			GinkgoT().Setenv("ELASTALERT_ROLLOUT_ENABLED", "false")
			GinkgoT().Setenv("ELASTALERT_DEPLOYMENT_NAME", deploymentName)
			GinkgoT().Setenv("ELASTALERT_DEPLOYMENT_NAMESPACE", namespace)

			By("Creating the target ConfigMap and Deployment")
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "elastalert-rules",
					Namespace: namespace,
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      deploymentName,
					Namespace: namespace,
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": deploymentName},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": deploymentName},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "elastalert", Image: "elastalert:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, deployment)
			})

			By("Creating and reconciling the ElastAlertRule")
			numEvents := 10
			resource := &alertsv1alpha1.ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: alertsv1alpha1.ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					Timeframe: timeframe5Minutes(),
					NumEvents: &numEvents,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())

			controllerReconciler := &ElastAlertRuleReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the deployment was never annotated with a checksum")
			Consistently(func() map[string]string {
				updated := &appsv1.Deployment{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: deploymentName, Namespace: namespace}, updated); err != nil {
					return map[string]string{}
				}
				return updated.Spec.Template.Annotations
			}, time.Second*2, interval).ShouldNot(HaveKey(rulesChecksumAnnotation))
		})
	})
})
