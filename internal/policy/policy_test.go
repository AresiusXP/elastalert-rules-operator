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

package policy

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
)

func rule(mutate func(*alertsv1alpha1.ElastAlertRule)) *alertsv1alpha1.ElastAlertRule {
	r := &alertsv1alpha1.ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{Name: "r", Namespace: "team-a"},
		Spec:       alertsv1alpha1.ElastAlertRuleSpec{Type: "any", Index: "logs-app"},
	}
	if mutate != nil {
		mutate(r)
	}
	return r
}

func TestParseTargets(t *testing.T) {
	got, err := ParseTargets("ConfigMap", " a/b , c/d,")
	if err != nil || len(got) != 2 || got[1].Namespace != "c" || got[1].Name != "d" {
		t.Fatalf("ParseTargets() = %v, %v", got, err)
	}
	for _, bad := range []string{"nonamespace", "a/", "/b", "a/b/c"} {
		if _, err := ParseTargets("ConfigMap", bad); err == nil {
			t.Errorf("ParseTargets(%q) should fail", bad)
		}
	}
}

func TestCheckTarget(t *testing.T) {
	p := New([]Target{
		{Kind: "ConfigMap", Namespace: "elastalert", Name: "rules"},
		{Kind: "Secret", Namespace: "elastalert", Name: "secret-rules"},
	}, nil, nil, nil)

	tests := []struct {
		name string
		rule *alertsv1alpha1.ElastAlertRule
		ok   bool
	}{
		{"allowed configmap", rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: "elastalert", Name: "rules"}
		}), true},
		{"allowed secret", rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: "elastalert", Name: "secret-rules", Kind: "Secret"}
		}), true},
		{"other namespace", rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: "kube-system", Name: "aws-auth"}
		}), false},
		{"other name", rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: "elastalert", Name: "other"}
		}), false},
		{"kind mismatch", rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.TargetConfigMap = alertsv1alpha1.TargetConfigMap{Namespace: "elastalert", Name: "rules", Kind: "Secret"}
		}), false},
		{"defaults to rule namespace", rule(nil), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := p.CheckTarget(tt.rule); (err == nil) != tt.ok {
				t.Fatalf("CheckTarget() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
	if got := p.Namespaces("ConfigMap"); len(got) != 1 || got[0] != "elastalert" {
		t.Fatalf("Namespaces() = %v", got)
	}
}

func TestCheckRuleAlerters(t *testing.T) {
	p := New(nil, nil, nil, nil)
	for _, alerter := range []string{"opsgenie", "post", "slack", "debug"} {
		if err := p.CheckRule(rule(func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Alert = []string{alerter} })); err != nil {
			t.Errorf("alerter %q should be allowed: %v", alerter, err)
		}
	}
	for _, alerter := range []string{"command", "post2", "my_module.MyAlerter", "Command"} {
		err := p.CheckRule(rule(func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Alert = []string{alerter} }))
		if err == nil || !strings.Contains(err.Error(), "not allowed") {
			t.Errorf("alerter %q should be rejected, got %v", alerter, err)
		}
	}
	custom := New(nil, []string{"command"}, nil, nil)
	if err := custom.CheckRule(rule(func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Alert = []string{"command"} })); err != nil {
		t.Errorf("explicitly allowed alerter rejected: %v", err)
	}
}

func TestCheckRuleBlockedFields(t *testing.T) {
	p := New(nil, nil, nil, nil)
	blocked := []string{
		"command", "pipe_match_json", "pipe_alert_text", "import", "es_host", "es_password", "es_custom",
		"use_ssl", "verify_certs", "ca_certs", "client_key", "rules_folder", "alert_text_type", "alert", "index",
		"timestamp_type", "timestamp_format_expr", "timestamp_to_datetime_format_expr", "match_enhancements",
		"http_post2_payload", "slack_proxy", "http_post_ca_certs", "totally_unknown_setting",
		"alertmanager_labels", "alertmanager_annotations", "opsgenie_details",
	}
	for _, key := range blocked {
		r := rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"` + key + `":"x"}`)}
		})
		if err := p.CheckRule(r); err == nil {
			t.Errorf("additionalFields key %q should be rejected", key)
		}
		r = rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.SecretRefs = []alertsv1alpha1.SecretRef{{Field: key, Name: "s", Key: "k"}}
		})
		if err := p.CheckRule(r); err == nil {
			t.Errorf("secretRef field %q should be rejected", key)
		}
	}
	ok := rule(func(r *alertsv1alpha1.ElastAlertRule) {
		r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"spike_height":2,"slack_webhook_url":"x"}`)}
	})
	if err := p.CheckRule(ok); err != nil {
		t.Errorf("harmless fields rejected: %v", err)
	}
}

func TestCheckRuleIndex(t *testing.T) {
	p := New(nil, nil, []string{"logs-*", "audit"}, nil)

	for _, idx := range []string{"logs-app", "logs-app,audit"} {
		if err := p.CheckRule(rule(func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Index = idx })); err != nil {
			t.Errorf("index %q should be allowed: %v", idx, err)
		}
	}
	for _, idx := range []string{"*", "secrets-prod", "logs-app,secrets"} {
		if err := p.CheckRule(rule(func(r *alertsv1alpha1.ElastAlertRule) { r.Spec.Index = idx })); err == nil {
			t.Errorf("index %q should be rejected", idx)
		}
	}

}

func TestAdditionalFieldAllowlist(t *testing.T) {
	spec := func(key string) *alertsv1alpha1.ElastAlertRule {
		return rule(func(r *alertsv1alpha1.ElastAlertRule) {
			r.Spec.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{"` + key + `":"x"}`)}
		})
	}
	base := New(nil, nil, nil, nil)
	if err := base.CheckRule(spec("custom_setting")); err == nil {
		t.Error("unknown setting should be rejected by default")
	}
	extended := New(nil, nil, nil, nil, "custom_setting", "command")
	if err := extended.CheckRule(spec("custom_setting")); err != nil {
		t.Errorf("setting added with extra fields rejected: %v", err)
	}
	if err := extended.CheckRule(spec("command")); err == nil {
		t.Error("never-allowed setting must stay rejected even when listed as extra")
	}
	withPost2 := New(nil, []string{"post2"}, nil, nil)
	if err := withPost2.CheckRule(spec("http_post2_payload")); err != nil {
		t.Errorf("http_post2 options should be allowed when post2 is enabled: %v", err)
	}
}

func TestCheckRenderedHosts(t *testing.T) {
	open := New(nil, nil, nil, nil)
	if err := open.CheckRendered(map[string]interface{}{"http_post_url": "https://anything.example.org"}); err != nil {
		t.Errorf("unrestricted policy rejected URL: %v", err)
	}

	p := New(nil, nil, nil, []string{"hooks.example.com", "alerts.example.com"})
	tests := []struct {
		name string
		rule map[string]interface{}
		ok   bool
	}{
		{"allowed url", map[string]interface{}{"http_post_url": "https://hooks.example.com/a"}, true},
		{"allowed list", map[string]interface{}{"http_post2_url": []interface{}{"https://hooks.example.com", "https://alerts.example.com:8443/x"}}, true},
		{"host with port and no scheme", map[string]interface{}{"alertmanager_hosts": []interface{}{"alerts.example.com:9093"}}, true},
		{"other host", map[string]interface{}{"http_post_url": "https://evil.example.org"}, false},
		{"list with bad entry", map[string]interface{}{"http_post2_url": []interface{}{"https://hooks.example.com", "http://169.254.169.254"}}, false},
		{"webhook url", map[string]interface{}{"slack_webhook_url": "https://hooks.slack.com/x"}, false},
		{"wrong type", map[string]interface{}{"http_post_url": 5}, false},
		{"list of wrong type", map[string]interface{}{"http_post_url": []interface{}{1}}, false},
		{"non endpoint field ignored", map[string]interface{}{"alert_subject": "https://evil.example.org"}, true},
		{"opsgenie address", map[string]interface{}{"opsgenie_addr": "https://attacker.example/alerts"}, false},
		{"boolean url switch is not an endpoint", map[string]interface{}{"slack_attach_kibana_discover_url": false}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := p.CheckRendered(tt.rule); (err == nil) != tt.ok {
				t.Fatalf("CheckRendered() = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestCheckRenderedValues(t *testing.T) {
	p := New(nil, nil, nil, []string{"hooks.example.com"})

	for _, entry := range []string{"!file /etc/passwd", "  !file /dev/zero"} {
		for _, key := range []string{"blacklist", "whitelist"} {
			if err := p.CheckRendered(map[string]interface{}{key: []interface{}{"ok", entry}}); err == nil {
				t.Errorf("%s entry %q should be rejected", key, entry)
			}
		}
	}
	if err := p.CheckRendered(map[string]interface{}{"whitelist": []interface{}{"error", "fatal"}}); err != nil {
		t.Errorf("plain list rejected: %v", err)
	}

	for _, arg := range []string{"es_password", "smtp_password", "opsgenie_key", "slack_webhook_url", "http_post_headers", "command"} {
		for _, key := range []string{"alert_text_args", "alert_subject_args"} {
			if err := p.CheckRendered(map[string]interface{}{key: []interface{}{"message", arg}}); err == nil {
				t.Errorf("%s referencing %q should be rejected", key, arg)
			}
		}
	}
	if err := p.CheckRendered(map[string]interface{}{"alert_text_args": []interface{}{"message", "kubernetes.pod_name"}}); err != nil {
		t.Errorf("event fields rejected: %v", err)
	}
}

func TestErrorsDoNotLeakValues(t *testing.T) {
	p := New(nil, nil, nil, []string{"hooks.example.com"})
	for _, secret := range []string{"s3cr3t-token", "eyJhbGciOi.payload.sig", "https://user:pass@evil.example.org/x"} {
		err := p.CheckRendered(map[string]interface{}{"slack_webhook_url": secret})
		if err == nil {
			t.Fatalf("%q should be rejected", secret)
		}
		if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "pass@") || strings.Contains(err.Error(), "evil.example.org") {
			t.Errorf("error leaks the value: %v", err)
		}
	}
}
