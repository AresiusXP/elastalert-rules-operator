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
	"encoding/json"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestToElastAlertYAMLBasic(t *testing.T) {
	numEvents := 10
	timeframe, _ := json.Marshal(map[string]int{"minutes": 5})
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			Timeframe: &runtime.RawExtension{Raw: timeframe},
			NumEvents: &numEvents,
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	// Check required fields
	requiredFields := []string{
		"name: test-rule",
		"type: frequency",
		"index: logs-*",
		"num_events: 10",
		"minutes: 5",
	}

	for _, field := range requiredFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing required field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithOpsgenieFields(t *testing.T) {
	numEvents := 1
	timeframe, _ := json.Marshal(map[string]int{"minutes": 5})
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "opsgenie-rule",
			Namespace: "monitoring",
		},
		Spec: ElastAlertRuleSpec{
			Type:             "frequency",
			Index:            "logs-*",
			Timeframe:        &runtime.RawExtension{Raw: timeframe},
			NumEvents:        &numEvents,
			AlertText:        "{0}: {1}",
			AlertTextArgs:    []string{"component", "message"},
			Alert:            []string{"opsgenie"},
			OpsgenieKey:      "test-api-key",
			OpsgenieMessage:  "Test alert",
			OpsgeniePriority: "P1",
			OpsgenieTags:     []string{"production", "critical"},
			OpsgenieTeams:    []string{"ops-team"},
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	expectedFields := []string{
		"alert_text: '{0}: {1}'",
		"alert_text_args:",
		"- component",
		"- message",
		"alert:",
		"- opsgenie",
		"opsgenie_key: test-api-key",
		"opsgenie_message: Test alert",
		"opsgenie_priority: P1",
		"opsgenie_tags:",
		"- production",
		"- critical",
		"opsgenie_teams:",
		"- ops-team",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithFilterList(t *testing.T) {
	numEvents := 5

	// Create filter items as RawExtension
	filter1, _ := json.Marshal(map[string]interface{}{
		"term": map[string]string{"level": "error"},
	})
	filter2, _ := json.Marshal(map[string]interface{}{
		"range": map[string]interface{}{
			"timestamp": map[string]string{"gte": "now-1h"},
		},
	})

	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "filter-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &numEvents,
			Filter: []runtime.RawExtension{
				{Raw: filter1},
				{Raw: filter2},
			},
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	expectedFields := []string{
		"filter:",
		"level: error",
		"timestamp:",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithRealert(t *testing.T) {
	numEvents := 10
	realert, _ := json.Marshal(map[string]interface{}{
		"minutes": 30,
	})
	exponentialRealert, _ := json.Marshal(map[string]interface{}{
		"minutes": 15,
	})

	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "realert-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:               "frequency",
			Index:              "logs-*",
			NumEvents:          &numEvents,
			Realert:            &runtime.RawExtension{Raw: realert},
			ExponentialRealert: &runtime.RawExtension{Raw: exponentialRealert},
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	expectedFields := []string{
		"realert:",
		"minutes: 30",
		"exponential_realert:",
		"minutes: 15",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithInclude(t *testing.T) {
	numEvents := 5
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "include-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &numEvents,
			Include: []string{
				"component",
				"message",
				"stack_trace",
			},
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	expectedFields := []string{
		"include:",
		"- component",
		"- message",
		"- stack_trace",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithAdditionalFields(t *testing.T) {
	numEvents := 10
	additional, _ := json.Marshal(map[string]interface{}{
		"spike_height": 2,
		"spike_type":   "up",
		"custom_field": "custom_value",
	})

	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "additional-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:             "spike",
			Index:            "logs-*",
			NumEvents:        &numEvents,
			AdditionalFields: &runtime.RawExtension{Raw: additional},
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	expectedFields := []string{
		"spike_height: 2",
		"spike_type: up",
		"custom_field: custom_value",
	}

	for _, field := range expectedFields {
		if !strings.Contains(yaml, field) {
			t.Errorf("YAML missing field: %s\nGenerated YAML:\n%s", field, yaml)
		}
	}
}

func TestToElastAlertYAMLWithCustomName(t *testing.T) {
	numEvents := 5
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-rule-cr",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &numEvents,
			Name:      "[team-platform] Custom Rule Name",
		},
	}

	yaml, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}

	if !strings.Contains(yaml, "name: '[team-platform] Custom Rule Name'") {
		t.Errorf("YAML should contain custom name\nGenerated YAML:\n%s", yaml)
	}

	if strings.Contains(yaml, "name: my-rule-cr") {
		t.Errorf("YAML should not contain CR name when custom name is set\nGenerated YAML:\n%s", yaml)
	}
}

func TestTimeframeFormats(t *testing.T) {
	tests := []struct {
		name        string
		timeframe   map[string]int
		contains    string
		notContains string
	}{
		{
			name:        "seconds format",
			timeframe:   map[string]int{"seconds": 30},
			contains:    "seconds: 30",
			notContains: "seconds: \"30\"",
		},
		{
			name:        "minutes format",
			timeframe:   map[string]int{"minutes": 5},
			contains:    "minutes: 5",
			notContains: "minutes: \"5\"",
		},
		{
			name:        "hours format",
			timeframe:   map[string]int{"hours": 2},
			contains:    "hours: 2",
			notContains: "hours: \"2\"",
		},
		{
			name:        "days format",
			timeframe:   map[string]int{"days": 1},
			contains:    "days: 1",
			notContains: "days: \"1\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			numEvents := 10
			timeframeRaw, _ := json.Marshal(tt.timeframe)
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: ElastAlertRuleSpec{
					Type:      "frequency",
					Index:     "logs-*",
					NumEvents: &numEvents,
					Timeframe: &runtime.RawExtension{Raw: timeframeRaw},
				},
			}

			yaml, err := rule.ToElastAlertYAML()
			if err != nil {
				t.Fatalf("ToElastAlertYAML() failed: %v", err)
			}

			if !strings.Contains(yaml, tt.contains) {
				t.Errorf("YAML should contain %s\nGenerated YAML:\n%s", tt.contains, yaml)
			}

			if strings.Contains(yaml, tt.notContains) {
				t.Errorf("YAML should NOT contain %s (number should not be quoted)\nGenerated YAML:\n%s", tt.notContains, yaml)
			}
		})
	}
}

func TestGetConfigMapName(t *testing.T) {
	tests := []struct {
		name      string
		configMap TargetConfigMap
		want      string
	}{
		{
			name:      "default config map name",
			configMap: TargetConfigMap{},
			want:      "elastalert-rules",
		},
		{
			name: "custom config map name",
			configMap: TargetConfigMap{
				Name: "custom-rules",
			},
			want: "custom-rules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: ElastAlertRuleSpec{
					TargetConfigMap: tt.configMap,
				},
			}
			got := rule.GetConfigMapName()
			if got != tt.want {
				t.Errorf("GetConfigMapName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetConfigMapNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		configMap TargetConfigMap
		want      string
	}{
		{
			name:      "default to rule namespace",
			namespace: "default",
			configMap: TargetConfigMap{},
			want:      "default",
		},
		{
			name:      "custom namespace",
			namespace: "default",
			configMap: TargetConfigMap{
				Namespace: "monitoring",
			},
			want: "monitoring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: tt.namespace,
				},
				Spec: ElastAlertRuleSpec{
					TargetConfigMap: tt.configMap,
				},
			}
			got := rule.GetConfigMapNamespace()
			if got != tt.want {
				t.Errorf("GetConfigMapNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetKeyName(t *testing.T) {
	tests := []struct {
		name     string
		ruleName string
		keyName  string
		want     string
	}{
		{
			name:     "default key name",
			ruleName: "my-rule",
			keyName:  "",
			want:     "my-rule.yaml",
		},
		{
			name:     "custom key name",
			ruleName: "my-rule",
			keyName:  "custom.yaml",
			want:     "custom.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.ruleName,
				},
				Spec: ElastAlertRuleSpec{
					KeyName: tt.keyName,
				},
			}
			got := rule.GetKeyName()
			if got != tt.want {
				t.Errorf("GetKeyName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetRuleName(t *testing.T) {
	tests := []struct {
		name     string
		crName   string
		specName string
		want     string
	}{
		{
			name:     "default rule name",
			crName:   "my-rule",
			specName: "",
			want:     "my-rule",
		},
		{
			name:     "custom rule name in spec",
			crName:   "my-rule",
			specName: "[team-platform] Custom Rule",
			want:     "[team-platform] Custom Rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.crName,
				},
				Spec: ElastAlertRuleSpec{
					Name: tt.specName,
				},
			}
			got := rule.GetRuleName()
			if got != tt.want {
				t.Errorf("GetRuleName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateSpec(t *testing.T) {
	tests := []struct {
		name    string
		spec    ElastAlertRuleSpec
		wantErr bool
	}{
		{
			name: "valid frequency rule",
			spec: ElastAlertRuleSpec{
				Type:      "frequency",
				Index:     "logs-*",
				NumEvents: intPtr(10),
			},
			wantErr: false,
		},
		{
			name: "missing type",
			spec: ElastAlertRuleSpec{
				Index: "logs-*",
			},
			wantErr: true,
		},
		{
			name: "missing index",
			spec: ElastAlertRuleSpec{
				Type: "frequency",
			},
			wantErr: true,
		},
		{
			name: "frequency rule missing num_events",
			spec: ElastAlertRuleSpec{
				Type:  "frequency",
				Index: "logs-*",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "default",
				},
				Spec: tt.spec,
			}
			err := rule.ValidateSpec()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSpec() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func intPtr(i int) *int {
	return &i
}

func TestToElastAlertYAMLWithObjectRealert(t *testing.T) {
	numEvents := 10
	// A realert object such as {minutes: 0} (to disable suppression) must
	// round-trip rather than being dropped.
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "object-realert-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &numEvents,
			Realert:   &runtime.RawExtension{Raw: []byte(`{"minutes": 0}`)},
		},
	}

	out, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}
	if !strings.Contains(out, "realert:") || !strings.Contains(out, "minutes: 0") {
		t.Errorf("YAML should contain realert with minutes: 0\nGenerated YAML:\n%s", out)
	}
}

func TestToElastAlertYAMLWithScalarRealert(t *testing.T) {
	numEvents := 10
	// A scalar realert value (not an object) must round-trip. The previous
	// decode into map[string]interface{} would fail on a scalar and silently
	// drop the field; decoding into interface{} preserves it.
	rule := &ElastAlertRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "scalar-realert-rule",
			Namespace: "default",
		},
		Spec: ElastAlertRuleSpec{
			Type:      "frequency",
			Index:     "logs-*",
			NumEvents: &numEvents,
			Realert:   &runtime.RawExtension{Raw: []byte(`0`)},
		},
	}

	out, err := rule.ToElastAlertYAML()
	if err != nil {
		t.Fatalf("ToElastAlertYAML() failed: %v", err)
	}
	if !strings.Contains(out, "realert: 0") {
		t.Errorf("YAML should contain 'realert: 0'\nGenerated YAML:\n%s", out)
	}
}

func TestToElastAlertYAMLMalformedInputReturnsError(t *testing.T) {
	numEvents := 10
	tests := []struct {
		name    string
		mutate  func(*ElastAlertRuleSpec)
		errPart string
	}{
		{
			name: "malformed filter",
			mutate: func(s *ElastAlertRuleSpec) {
				s.Filter = []runtime.RawExtension{{Raw: []byte(`{invalid`)}}
			},
			errPart: "filter",
		},
		{
			name: "malformed additionalFields",
			mutate: func(s *ElastAlertRuleSpec) {
				s.AdditionalFields = &runtime.RawExtension{Raw: []byte(`{invalid`)}
			},
			errPart: "additionalFields",
		},
		{
			name: "malformed realert",
			mutate: func(s *ElastAlertRuleSpec) {
				s.Realert = &runtime.RawExtension{Raw: []byte(`{invalid`)}
			},
			errPart: "realert",
		},
		{
			name: "malformed timeframe",
			mutate: func(s *ElastAlertRuleSpec) {
				s.Timeframe = &runtime.RawExtension{Raw: []byte(`{invalid`)}
			},
			errPart: "timeframe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := ElastAlertRuleSpec{
				Type:      "frequency",
				Index:     "logs-*",
				NumEvents: &numEvents,
			}
			tt.mutate(&spec)
			rule := &ElastAlertRule{
				ObjectMeta: metav1.ObjectMeta{Name: "bad-rule", Namespace: "default"},
				Spec:       spec,
			}

			_, err := rule.ToElastAlertYAML()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tt.name)
			}
			if !strings.Contains(err.Error(), tt.errPart) {
				t.Errorf("error %q should mention %q", err.Error(), tt.errPart)
			}
		})
	}
}
