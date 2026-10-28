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
	"fmt"
	"regexp"
	"strings"
)

var (
	keyNamePattern   = regexp.MustCompile(`^[-._a-zA-Z0-9]+\.ya?ml$`)
	fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	credentialSuffix = []string{"_key", "_token", "_password", "_passwd", "_secret", "webhook_url"}
)

// BuiltinRuleTypes are the ElastAlert rule types the operator accepts.
var BuiltinRuleTypes = []string{
	"any", "blacklist", "whitelist", "change", "frequency", "spike", "flatline",
	"new_term", "cardinality", "metric_aggregation", "spike_aggregation", "percentage_match",
}

// ValidateSpec validates the ElastAlertRule spec.
func (r *ElastAlertRule) ValidateSpec() error {
	if r.Spec.Type == "" {
		return fmt.Errorf("spec.type is required")
	}
	if !isBuiltinRuleType(r.Spec.Type) {
		return fmt.Errorf("spec.type %q is not a supported built-in rule type", r.Spec.Type)
	}
	if r.Spec.Index == "" {
		return fmt.Errorf("spec.index is required")
	}

	if r.Spec.Type == "frequency" {
		if r.Spec.NumEvents == nil || *r.Spec.NumEvents < 1 {
			return fmt.Errorf("spec.num_events must be >= 1 for frequency rule type")
		}
	}

	if r.Spec.MinPercentage != nil && (*r.Spec.MinPercentage < 0 || *r.Spec.MinPercentage > 100) {
		return fmt.Errorf("spec.min_percentage must be between 0 and 100")
	}

	if r.Spec.KeyName != "" {
		if strings.Contains(r.Spec.KeyName, "/") {
			return fmt.Errorf("spec.keyName cannot contain path separators")
		}
		if len(r.Spec.KeyName) > 253 || !keyNamePattern.MatchString(r.Spec.KeyName) {
			return fmt.Errorf("spec.keyName must match %s", keyNamePattern)
		}
	}

	if kind := r.GetTargetKind(); kind != TargetKindConfigMap && kind != TargetKindSecret {
		return fmt.Errorf("spec.targetConfigMap.kind must be ConfigMap or Secret")
	}

	return r.validateSecretRefs()
}

func (r *ElastAlertRule) validateSecretRefs() error {
	if len(r.Spec.SecretRefs) == 0 {
		return nil
	}
	if r.GetTargetKind() != TargetKindSecret {
		return fmt.Errorf("rules that use secretRefs must set spec.targetConfigMap.kind to Secret")
	}
	seen := make(map[string]struct{}, len(r.Spec.SecretRefs))
	for i, ref := range r.Spec.SecretRefs {
		if !fieldNamePattern.MatchString(ref.Field) {
			return fmt.Errorf("spec.secretRefs[%d].field %q is not a valid field name", i, ref.Field)
		}
		if ref.Name == "" || ref.Key == "" {
			return fmt.Errorf("spec.secretRefs[%d] requires name and key", i)
		}
		if ref.Format != "" && ref.Format != "string" && ref.Format != "yaml" {
			return fmt.Errorf("spec.secretRefs[%d].format must be string or yaml", i)
		}
		if _, dup := seen[ref.Field]; dup {
			return fmt.Errorf("spec.secretRefs has duplicate field %q", ref.Field)
		}
		seen[ref.Field] = struct{}{}
	}
	return nil
}

// HasPlaintextCredentials reports whether the spec carries credentials inline.
func (r *ElastAlertRule) HasPlaintextCredentials() bool {
	if r.Spec.OpsgenieKey != "" || (r.Spec.HTTPPostHeaders != nil && len(r.Spec.HTTPPostHeaders.Raw) > 0) {
		return true
	}
	additional, err := r.AdditionalFieldsMap()
	if err != nil {
		return false
	}
	for k := range additional {
		for _, suffix := range credentialSuffix {
			if strings.HasSuffix(k, suffix) {
				return true
			}
		}
	}
	return false
}

func isBuiltinRuleType(t string) bool {
	for _, b := range BuiltinRuleTypes {
		if t == b {
			return true
		}
	}
	return false
}
