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

package v1alpha1

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func baseRule() *ElastAlertRule {
	n := 1
	return &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "ns"},
		Spec:       ElastAlertRuleSpec{Type: "frequency", Index: "logs-*", NumEvents: &n},
	}
}

func TestValidateSpecHardening(t *testing.T) {
	pct := 101
	tests := []struct {
		name    string
		mutate  func(*ElastAlertRule)
		wantErr string
	}{
		{"valid", func(r *ElastAlertRule) {}, ""},
		{"custom rule class", func(r *ElastAlertRule) { r.Spec.Type = "my_module.MyRule" }, "built-in"},
		{"unknown type", func(r *ElastAlertRule) { r.Spec.Type = "bogus" }, "built-in"},
		{"percentage too high", func(r *ElastAlertRule) { r.Spec.MinPercentage = &pct }, "min_percentage"},
		{"key without extension", func(r *ElastAlertRule) { r.Spec.KeyName = "rule" }, "keyName"},
		{"key with space", func(r *ElastAlertRule) { r.Spec.KeyName = "my rule.yaml" }, "keyName"},
		{"key yml ok", func(r *ElastAlertRule) { r.Spec.KeyName = "my-rule.yml" }, ""},
		{"bad target kind", func(r *ElastAlertRule) { r.Spec.TargetConfigMap.Kind = "Pod" }, "kind"},
		{"secretRefs need Secret target", func(r *ElastAlertRule) {
			r.Spec.SecretRefs = []SecretRef{{Field: "opsgenie_key", Name: "s", Key: "k"}}
		}, "Secret"},
		{"secretRefs on Secret target", func(r *ElastAlertRule) {
			r.Spec.TargetConfigMap.Kind = TargetKindSecret
			r.Spec.SecretRefs = []SecretRef{{Field: "opsgenie_key", Name: "s", Key: "k"}}
		}, ""},
		{"duplicate secretRef field", func(r *ElastAlertRule) {
			r.Spec.TargetConfigMap.Kind = TargetKindSecret
			r.Spec.SecretRefs = []SecretRef{
				{Field: "opsgenie_key", Name: "s", Key: "k"},
				{Field: "opsgenie_key", Name: "s", Key: "k2"},
			}
		}, "duplicate"},
		{"bad secretRef field", func(r *ElastAlertRule) {
			r.Spec.TargetConfigMap.Kind = TargetKindSecret
			r.Spec.SecretRefs = []SecretRef{{Field: "Bad-Field", Name: "s", Key: "k"}}
		}, "field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := baseRule()
			tt.mutate(r)
			err := r.ValidateSpec()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %v should contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRenderRuleSecretRefs(t *testing.T) {
	r := baseRule()
	r.Spec.TargetConfigMap.Kind = TargetKindSecret
	r.Spec.SecretRefs = []SecretRef{
		{Field: "opsgenie_key", Name: "s", Key: "k"},
		{Field: "http_post_headers", Name: "s", Key: "h", Format: "yaml"},
	}

	if _, err := r.RenderRule(nil); err == nil {
		t.Fatal("expected an error when secretRefs are not resolved")
	}

	out, err := r.RenderRule(map[string]interface{}{
		"opsgenie_key":      "abc-123",
		"http_post_headers": map[string]interface{}{"Authorization": "Bearer x"},
	})
	if err != nil {
		t.Fatalf("RenderRule() failed: %v", err)
	}
	for _, want := range []string{"opsgenie_key: abc-123", "Authorization: Bearer x"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRenderRuleRejectsFieldConflicts(t *testing.T) {
	t.Run("secretRef over spec field", func(t *testing.T) {
		r := baseRule()
		r.Spec.OpsgenieKey = "inline"
		r.Spec.TargetConfigMap.Kind = TargetKindSecret
		r.Spec.SecretRefs = []SecretRef{{Field: "opsgenie_key", Name: "s", Key: "k"}}
		if _, err := r.RenderRule(map[string]interface{}{"opsgenie_key": "x"}); err == nil {
			t.Fatal("expected conflict error")
		}
	})
	t.Run("additionalFields over spec field", func(t *testing.T) {
		r := baseRule()
		r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"index":"other-*"}`)}
		_, err := r.ToElastAlertYAML()
		if err == nil || !strings.Contains(err.Error(), "conflicts") {
			t.Fatalf("expected conflict error, got %v", err)
		}
	})
}

func TestHasPlaintextCredentials(t *testing.T) {
	r := baseRule()
	if r.HasPlaintextCredentials() {
		t.Fatal("clean rule flagged")
	}
	r.Spec.OpsgenieKey = "k"
	if !r.HasPlaintextCredentials() {
		t.Fatal("opsgenie_key not flagged")
	}
	r = baseRule()
	r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"slack_webhook_url":"https://x"}`)}
	if !r.HasPlaintextCredentials() {
		t.Fatal("additionalFields webhook not flagged")
	}
}
