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
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
	"github.com/AresiusXP/elastalert-rules-operator/internal/policy"
)

var nsCounter atomic.Int32

func newNamespace() string {
	name := fmt.Sprintf("t%d", nsCounter.Add(1))
	Expect(k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}})).To(Succeed())
	return name
}

func newTarget(ns, name string, data map[string]string) {
	Expect(k8sClient.Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}, Data: data,
	})).To(Succeed())
}

func newReconciler(targets ...policy.Target) *ElastAlertRuleReconciler {
	return &ElastAlertRuleReconciler{
		Client:   k8sClient,
		Scheme:   k8sClient.Scheme(),
		Recorder: events.NewFakeRecorder(1000),
		Policy:   policy.New(targets, nil, nil, nil),
	}
}

func cmTarget(ns, name string) policy.Target {
	return policy.Target{Kind: "ConfigMap", Namespace: ns, Name: name}
}

func secretTarget(ns, name string) policy.Target {
	return policy.Target{Kind: "Secret", Namespace: ns, Name: name}
}

func frequencyRule(ns, name string, mutate ...func(*alertsv1alpha1.ElastAlertRule)) *alertsv1alpha1.ElastAlertRule {
	n := 5
	rule := &alertsv1alpha1.ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: alertsv1alpha1.ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &n,
			Timeframe: &runtime.RawExtension{Raw: []byte(`{"minutes":5}`)},
		},
	}
	for _, m := range mutate {
		m(rule)
	}
	return rule
}

func createRule(rule *alertsv1alpha1.ElastAlertRule) {
	Expect(k8sClient.Create(context.Background(), rule)).To(Succeed())
}

func reconcileRule(r *ElastAlertRuleReconciler, rule *alertsv1alpha1.ElastAlertRule) (reconcile.Result, error) {
	return r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Namespace: rule.Namespace, Name: rule.Name},
	})
}

func getRule(rule *alertsv1alpha1.ElastAlertRule) *alertsv1alpha1.ElastAlertRule {
	out := &alertsv1alpha1.ElastAlertRule{}
	Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rule), out)).To(Succeed())
	return out
}

func getCM(ns, name string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, cm)).To(Succeed())
	return cm
}

func condition(rule *alertsv1alpha1.ElastAlertRule, condType string) *metav1.Condition {
	return meta.FindStatusCondition(getRule(rule).Status.Conditions, condType)
}

func expectFailure(rule *alertsv1alpha1.ElastAlertRule, condType, reason string) {
	c := condition(rule, condType)
	ExpectWithOffset(1, c).NotTo(BeNil())
	ExpectWithOffset(1, c.Status).To(Equal(metav1.ConditionTrue))
	ExpectWithOffset(1, c.Reason).To(Equal(reason))
	ready := condition(rule, alertsv1alpha1.ConditionTypeReady)
	ExpectWithOffset(1, ready).NotTo(BeNil())
	ExpectWithOffset(1, ready.Status).To(Equal(metav1.ConditionFalse))
}

var _ = Describe("ElastAlertRule controller", func() {
	var (
		ns string
		r  *ElastAlertRuleReconciler
	)

	BeforeEach(func() {
		ns = newNamespace()
		newTarget(ns, "elastalert-rules", map[string]string{})
		r = newReconciler(cmTarget(ns, "elastalert-rules"))
	})

	Describe("writing rules", func() {
		It("writes the rule under a namespaced key and reports Ready", func() {
			rule := frequencyRule(ns, "high-errors")
			createRule(rule)

			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			cm := getCM(ns, "elastalert-rules")
			key := ns + "_high-errors.yaml"
			Expect(cm.Data).To(HaveKey(key))
			Expect(cm.Data[key]).To(ContainSubstring("name: " + ns + "-high-errors"))
			Expect(cm.Data[key]).To(ContainSubstring("type: frequency"))
			Expect(cm.Data[key]).To(ContainSubstring("index: logs-*"))

			got := getRule(rule)
			Expect(got.Finalizers).To(ContainElement(alertsv1alpha1.FinalizerName))
			ready := meta.FindStatusCondition(got.Status.Conditions, alertsv1alpha1.ConditionTypeReady)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(got.Status.ObservedGeneration).To(Equal(got.Generation))
			Expect(got.Status.RuleChecksum).NotTo(BeEmpty())
			Expect(got.Status.LastReconciled).NotTo(BeNil())
		})

		It("updates the key when the spec changes and leaves the status alone when nothing changes", func() {
			rule := frequencyRule(ns, "r1")
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			first := getRule(rule)

			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			second := getRule(rule)
			Expect(second.ResourceVersion).To(Equal(first.ResourceVersion))
			Expect(second.Status.LastReconciled).To(Equal(first.Status.LastReconciled))

			second.Spec.Index = "other-*"
			Expect(k8sClient.Update(context.Background(), second)).To(Succeed())
			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			Expect(getCM(ns, "elastalert-rules").Data[ns+"_r1.yaml"]).To(ContainSubstring("index: other-*"))
			Expect(getRule(rule).Status.RuleChecksum).NotTo(Equal(first.Status.RuleChecksum))
		})

		It("keeps unrelated keys and other rules' keys", func() {
			cm := getCM(ns, "elastalert-rules")
			cm.Data = map[string]string{"manual.yaml": "name: manual\n"}
			Expect(k8sClient.Update(context.Background(), cm)).To(Succeed())

			a, b := frequencyRule(ns, "a"), frequencyRule(ns, "b")
			createRule(a)
			createRule(b)
			for _, rule := range []*alertsv1alpha1.ElastAlertRule{a, b} {
				_, err := reconcileRule(r, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(getCM(ns, "elastalert-rules").Data).To(And(
				HaveKey("manual.yaml"), HaveKey(ns+"_a.yaml"), HaveKey(ns+"_b.yaml")))
		})

		It("keeps same-named rules from different namespaces apart", func() {
			other := newNamespace()
			a := frequencyRule(ns, "same")
			b := frequencyRule(other, "same", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: ns, Name: "elastalert-rules"}
			})
			createRule(a)
			createRule(b)
			for _, rule := range []*alertsv1alpha1.ElastAlertRule{a, b} {
				_, err := reconcileRule(r, rule)
				Expect(err).NotTo(HaveOccurred())
			}
			Expect(getCM(ns, "elastalert-rules").Data).To(And(
				HaveKey(ns+"_same.yaml"), HaveKey(other+"_same.yaml")))
		})

		It("removes only its own key on delete", func() {
			a, b := frequencyRule(ns, "a"), frequencyRule(ns, "b")
			createRule(a)
			createRule(b)
			for _, rule := range []*alertsv1alpha1.ElastAlertRule{a, b} {
				_, err := reconcileRule(r, rule)
				Expect(err).NotTo(HaveOccurred())
			}

			Expect(k8sClient.Delete(context.Background(), a)).To(Succeed())
			_, err := reconcileRule(r, a)
			Expect(err).NotTo(HaveOccurred())

			Expect(getCM(ns, "elastalert-rules").Data).To(And(HaveKey(ns+"_b.yaml"), Not(HaveKey(ns+"_a.yaml"))))
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(a), &alertsv1alpha1.ElastAlertRule{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("rejecting rules", func() {
		It("refuses targets outside the allowlist", func() {
			kubeSystemLike := newNamespace()
			newTarget(kubeSystemLike, "aws-auth", map[string]string{"mapRoles": "original"})
			rule := frequencyRule(ns, "evil", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: kubeSystemLike, Name: "aws-auth"}
				r.Spec.KeyName = "mapRoles.yaml"
			})
			createRule(rule)

			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "TargetNotAllowed")
			Expect(getCM(kubeSystemLike, "aws-auth").Data).To(Equal(map[string]string{"mapRoles": "original"}))
		})

		It("does not touch a disallowed target when the rule is deleted", func() {
			other := newNamespace()
			newTarget(other, "elastalert-rules", map[string]string{"keep.yaml": "x"})
			rule := frequencyRule(ns, "gone", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: other, Name: "elastalert-rules"}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(context.Background(), rule)).To(Succeed())
			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			Expect(getCM(other, "elastalert-rules").Data).To(Equal(map[string]string{"keep.yaml": "x"}))
			err = k8sClient.Get(context.Background(), client.ObjectKeyFromObject(rule), &alertsv1alpha1.ElastAlertRule{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("rejects alerters that are not on the allowlist", func() {
			rule := frequencyRule(ns, "cmd", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.Alert = []string{"command"}
				r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"command":["/bin/sh"]}`)}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "PolicyViolation")
			Expect(getCM(ns, "elastalert-rules").Data).To(BeEmpty())
		})

		It("rejects additionalFields that point ElastAlert elsewhere", func() {
			rule := frequencyRule(ns, "es", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"es_host":"attacker.example.org"}`)}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "PolicyViolation")
		})

		It("reports a missing target", func() {
			rule := frequencyRule(ns, "orphan", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.TargetConfigMap.Name = "missing"
			})
			createRule(rule)
			r.Policy = policy.New([]policy.Target{cmTarget(ns, "missing")}, nil, nil, nil)

			res, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(BeNumerically(">", 0))
			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "TargetNotFound")
			Expect(condition(rule, alertsv1alpha1.ConditionTypeError).Message).To(ContainSubstring("does not exist"))
		})

		It("rejects the younger of two rules with the same rule name", func() {
			a := frequencyRule(ns, "first", func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Name = "shared name" })
			createRule(a)
			b := frequencyRule(ns, "second", func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Name = "shared name" })
			createRule(b)

			_, err := reconcileRule(r, a)
			Expect(err).NotTo(HaveOccurred())
			_, err = reconcileRule(r, b)
			Expect(err).NotTo(HaveOccurred())

			expectFailure(b, alertsv1alpha1.ConditionTypeValidationFailed, "DuplicateRuleName")
			Expect(condition(a, alertsv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
			Expect(getCM(ns, "elastalert-rules").Data).To(And(HaveKey(ns+"_first.yaml"), Not(HaveKey(ns+"_second.yaml"))))
		})

		It("reports a conflict instead of overwriting a key owned by someone else", func() {
			cm := getCM(ns, "elastalert-rules")
			cm.Data = map[string]string{"shared.yaml": "name: owned-by-someone-else\n"}
			Expect(k8sClient.Update(context.Background(), cm)).To(Succeed())

			rule := frequencyRule(ns, "clash", func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.KeyName = "shared.yaml" })
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "KeyConflict")
			Expect(getCM(ns, "elastalert-rules").Data["shared.yaml"]).To(Equal("name: owned-by-someone-else\n"))
		})

		It("reports a conflict between two rules using the same keyName", func() {
			a := frequencyRule(ns, "a", func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.KeyName = "same.yaml" })
			b := frequencyRule(ns, "b", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.KeyName = "same.yaml"
				r.Spec.Index = "different-*"
			})
			createRule(a)
			createRule(b)
			_, err := reconcileRule(r, a)
			Expect(err).NotTo(HaveOccurred())
			_, err = reconcileRule(r, b)
			Expect(err).NotTo(HaveOccurred())

			expectFailure(b, alertsv1alpha1.ConditionTypeError, "KeyConflict")
			Expect(getCM(ns, "elastalert-rules").Data["same.yaml"]).To(ContainSubstring("index: logs-*"))
		})
	})

	Describe("policy changes", func() {
		It("withdraws a rule that stops being acceptable", func() {
			rule := frequencyRule(ns, "withdrawn", func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Alert = []string{"slack"} })
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(getCM(ns, "elastalert-rules").Data).To(HaveKey(ns + "_withdrawn.yaml"))

			r.Policy = policy.New(r.Policy.Targets, []string{"debug"}, nil, nil)
			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "PolicyViolation")
			Expect(getCM(ns, "elastalert-rules").Data).NotTo(HaveKey(ns + "_withdrawn.yaml"))
		})

		It("rejects URL fields smuggled in through additionalFields", func() {
			r.Policy = policy.New(r.Policy.Targets, nil, nil, []string{"hooks.example.com"})
			rule := frequencyRule(ns, "smuggle", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.Alert = []string{"post"}
				r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"http_post_url":["https://evil.example.org"]}`)}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "PolicyViolation")
			Expect(getCM(ns, "elastalert-rules").Data).To(BeEmpty())
		})

		It("keeps the target immutable", func() {
			rule := frequencyRule(ns, "pinned")
			createRule(rule)
			stored := getRule(rule)
			stored.Spec.TargetConfigMap.Name = "elsewhere"
			Expect(k8sClient.Update(context.Background(), stored)).NotTo(Succeed())
		})
	})

	Describe("admission", func() {
		It("is enforced by the CRD schema", func() {
			ctx := context.Background()
			Expect(k8sClient.Create(ctx, frequencyRule(ns, "t1", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.Type = "my_module.MyRule"
			}))).NotTo(Succeed())
			Expect(k8sClient.Create(ctx, frequencyRule(ns, "t2", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.NumEvents = nil
			}))).NotTo(Succeed())
			Expect(k8sClient.Create(ctx, frequencyRule(ns, "t3", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.KeyName = "no-extension"
			}))).NotTo(Succeed())
			Expect(k8sClient.Create(ctx, frequencyRule(ns, "t4", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.SecretRefs = []alertsv1alpha1.SecretRef{{Field: "opsgenie_key", Name: "s", Key: "k"}}
			}))).NotTo(Succeed())
			Expect(k8sClient.Create(ctx, frequencyRule(ns, "ok", func(r *alertsv1alpha1.ElastAlertRule) {
				r.Spec.TargetConfigMap.Kind = alertsv1alpha1.TargetKindSecret
				r.Spec.SecretRefs = []alertsv1alpha1.SecretRef{{Field: "opsgenie_key", Name: "s", Key: "k"}}
			}))).To(Succeed())
		})
	})

	Describe("secrets", func() {
		var secretRule func(mutate ...func(*alertsv1alpha1.ElastAlertRule)) *alertsv1alpha1.ElastAlertRule

		BeforeEach(func() {
			Expect(k8sClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "elastalert-rules", Namespace: ns},
			})).To(Succeed())
			r = newReconciler(secretTarget(ns, "elastalert-rules"))
			r.EnableSecretRefs = true

			secretRule = func(mutate ...func(*alertsv1alpha1.ElastAlertRule)) *alertsv1alpha1.ElastAlertRule {
				return frequencyRule(ns, "with-secret", append([]func(*alertsv1alpha1.ElastAlertRule){
					func(rule *alertsv1alpha1.ElastAlertRule) {
						rule.Spec.TargetConfigMap.Kind = alertsv1alpha1.TargetKindSecret
						rule.Spec.Alert = []string{"opsgenie"}
						rule.Spec.SecretRefs = []alertsv1alpha1.SecretRef{{Field: "opsgenie_key", Name: "credentials", Key: "key"}}
					},
				}, mutate...)...)
			}
		})

		credentials := func(labels map[string]string) {
			Expect(k8sClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: ns, Labels: labels},
				Data:       map[string][]byte{"key": []byte("s3cr3t"), "headers": []byte("Authorization: Bearer abc\n")},
			})).To(Succeed())
		}

		It("writes the rule with the secret value into a Secret target", func() {
			credentials(map[string]string{SourceLabel: "true"})
			rule := secretRule()
			createRule(rule)

			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			target := &corev1.Secret{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "elastalert-rules"}, target)).To(Succeed())
			Expect(string(target.Data[ns+"_with-secret.yaml"])).To(ContainSubstring("opsgenie_key: s3cr3t"))
			Expect(condition(rule, alertsv1alpha1.ConditionTypeReady).Status).To(Equal(metav1.ConditionTrue))
			Expect(getRule(rule).Status.Conditions).NotTo(ContainElement(HaveField("Message", ContainSubstring("s3cr3t"))))
		})

		It("parses yaml secretRefs into structured values", func() {
			credentials(map[string]string{SourceLabel: "true"})
			rule := secretRule(func(rule *alertsv1alpha1.ElastAlertRule) {
				rule.Spec.SecretRefs = append(rule.Spec.SecretRefs,
					alertsv1alpha1.SecretRef{Field: "http_post_headers", Name: "credentials", Key: "headers", Format: "yaml"})
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())

			target := &corev1.Secret{}
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "elastalert-rules"}, target)).To(Succeed())
			Expect(string(target.Data[ns+"_with-secret.yaml"])).To(ContainSubstring("Authorization: Bearer abc"))
		})

		It("does not leak secret content through YAML errors", func() {
			Expect(k8sClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: ns, Labels: map[string]string{SourceLabel: "true"}},
				Data:       map[string][]byte{"headers": []byte("!!int TOP-SECRET-VALUE")},
			})).To(Succeed())
			rule := secretRule(func(rule *alertsv1alpha1.ElastAlertRule) {
				rule.Spec.SecretRefs = []alertsv1alpha1.SecretRef{
					{Field: "http_post_headers", Name: "credentials", Key: "headers", Format: "yaml"}}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "SecretRefFailed")
			for _, c := range getRule(rule).Status.Conditions {
				Expect(c.Message).NotTo(ContainSubstring("TOP-SECRET-VALUE"))
			}
		})

		It("checks URL values that come from a Secret against the host allowlist", func() {
			Expect(k8sClient.Create(context.Background(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "credentials", Namespace: ns, Labels: map[string]string{SourceLabel: "true"}},
				Data:       map[string][]byte{"url": []byte("https://evil.example.org/hook")},
			})).To(Succeed())
			r.Policy = policy.New(r.Policy.Targets, nil, nil, []string{"hooks.example.com"})
			rule := secretRule(func(rule *alertsv1alpha1.ElastAlertRule) {
				rule.Spec.Alert = []string{"slack"}
				rule.Spec.SecretRefs = []alertsv1alpha1.SecretRef{{Field: "slack_webhook_url", Name: "credentials", Key: "url"}}
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeValidationFailed, "PolicyViolation")
		})

		It("refuses Secrets without the source label", func() {
			credentials(nil)
			rule := secretRule()
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "SecretRefFailed")
			Expect(condition(rule, alertsv1alpha1.ConditionTypeError).Message).To(ContainSubstring(SourceLabel))
		})

		It("refuses secretRefs when the feature is disabled", func() {
			credentials(map[string]string{SourceLabel: "true"})
			r.EnableSecretRefs = false
			rule := secretRule()
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "SecretRefFailed")
		})

		It("refuses to read a write target as a source", func() {
			rule := secretRule(func(rule *alertsv1alpha1.ElastAlertRule) {
				rule.Spec.SecretRefs[0].Name = "elastalert-rules"
			})
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			expectFailure(rule, alertsv1alpha1.ConditionTypeError, "SecretRefFailed")
			Expect(condition(rule, alertsv1alpha1.ConditionTypeError).Message).To(ContainSubstring("write target"))
		})

		It("warns about inline credentials", func() {
			rule := frequencyRule(ns, "inline", func(rule *alertsv1alpha1.ElastAlertRule) {
				rule.Spec.Alert = []string{"opsgenie"}
				rule.Spec.OpsgenieKey = "inline-key"
			})
			r = newReconciler(cmTarget(ns, "elastalert-rules"))
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			c := condition(rule, alertsv1alpha1.ConditionTypePlaintextCredentials)
			Expect(c).NotTo(BeNil())
			Expect(c.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Describe("rollout", func() {
		var deployKey types.NamespacedName

		BeforeEach(func() {
			deployKey = types.NamespacedName{Namespace: ns, Name: "elastalert"}
			labels := map[string]string{"app": "elastalert"}
			Expect(k8sClient.Create(context.Background(), &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "elastalert", Namespace: ns},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: labels},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "busybox"}}},
					},
				},
			})).To(Succeed())
		})

		annotation := func() string {
			d := &appsv1.Deployment{}
			Expect(k8sClient.Get(context.Background(), deployKey, d)).To(Succeed())
			return d.Spec.Template.Annotations[rulesChecksumAnnotation]
		}

		It("annotates the Deployment and only changes the annotation when rules change", func() {
			r.Rollout = &deployKey
			rule := frequencyRule(ns, "roll")
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			first := annotation()
			Expect(first).NotTo(BeEmpty())

			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotation()).To(Equal(first))

			stored := getRule(rule)
			stored.Spec.Index = "changed-*"
			Expect(k8sClient.Update(context.Background(), stored)).To(Succeed())
			_, err = reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotation()).NotTo(Equal(first))
		})

		It("does nothing when rollout is not configured", func() {
			rule := frequencyRule(ns, "noroll")
			createRule(rule)
			_, err := reconcileRule(r, rule)
			Expect(err).NotTo(HaveOccurred())
			Expect(annotation()).To(BeEmpty())
		})
	})
})
