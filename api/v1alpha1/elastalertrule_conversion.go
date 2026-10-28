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
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

// ToElastAlertYAML converts the ElastAlertRule spec to ElastAlert YAML format.
// Fields are directly mapped to ElastAlert format.
//
// All RawExtension fields contain JSON (Kubernetes serializes them as JSON) and
// are decoded with encoding/json into generic values so scalars, lists and
// objects all round-trip correctly. A malformed RawExtension is reported as an
// error rather than silently dropped, so the controller can surface it as a
// status condition.
func (r *ElastAlertRule) ToElastAlertYAML() (string, error) {
	return r.RenderRule(nil)
}

// RenderRule renders the rule like ToElastAlertYAML. resolved holds the values
// for spec.secretRefs, keyed by field; every secretRef must have an entry.
func (r *ElastAlertRule) RenderRule(resolved map[string]interface{}) (string, error) {
	rule, err := r.BuildRule(resolved)
	if err != nil {
		return "", err
	}
	return MarshalRule(rule)
}

// MarshalRule renders a rule map as YAML.
func MarshalRule(rule map[string]interface{}) (string, error) {
	yamlBytes, err := yaml.Marshal(rule)
	if err != nil {
		return "", err
	}
	return string(yamlBytes), nil
}

// BuildRule assembles the final rule map: typed fields, resolved secret refs
// and additionalFields. Conflicting keys are an error.
func (r *ElastAlertRule) BuildRule(resolved map[string]interface{}) (map[string]interface{}, error) {
	rule := make(map[string]interface{})

	rule["name"] = r.GetRuleName()
	rule["type"] = r.Spec.Type
	rule["index"] = r.Spec.Index

	if err := r.addOptionalFields(rule); err != nil {
		return nil, err
	}
	r.addOpsgenieFields(rule)
	if err := r.addHTTPPostFields(rule); err != nil {
		return nil, err
	}
	if err := r.addPercentageMatchFields(rule); err != nil {
		return nil, err
	}
	if err := r.addFilterField(rule); err != nil {
		return nil, err
	}
	if err := r.addSecretRefFields(rule, resolved); err != nil {
		return nil, err
	}
	if err := r.addAdditionalFields(rule); err != nil {
		return nil, err
	}
	return rule, nil
}

// decodeRawExtension decodes a RawExtension's JSON payload into a generic value.
// Returns (nil, false, nil) when there is nothing to decode, and an error when
// the payload is present but malformed.
func decodeRawExtension(field string, ext *runtime.RawExtension) (interface{}, bool, error) {
	if ext == nil || len(ext.Raw) == 0 {
		return nil, false, nil
	}
	var value interface{}
	if err := json.Unmarshal(ext.Raw, &value); err != nil {
		return nil, false, fmt.Errorf("invalid %s: %w", field, err)
	}
	return value, true, nil
}

// addOptionalFields adds optional fields to the rule map
func (r *ElastAlertRule) addOptionalFields(rule map[string]interface{}) error {
	if value, ok, err := decodeRawExtension("timeframe", r.Spec.Timeframe); err != nil {
		return err
	} else if ok {
		rule["timeframe"] = value
	}

	if r.Spec.NumEvents != nil {
		rule["num_events"] = *r.Spec.NumEvents
	}

	if r.Spec.QueryKey != "" {
		rule["query_key"] = r.Spec.QueryKey
	}

	if r.Spec.AlertText != "" {
		rule["alert_text"] = r.Spec.AlertText
	}

	if len(r.Spec.AlertTextArgs) > 0 {
		rule["alert_text_args"] = r.Spec.AlertTextArgs
	}

	if len(r.Spec.Alert) > 0 {
		rule["alert"] = r.Spec.Alert
	}

	if value, ok, err := decodeRawExtension("realert", r.Spec.Realert); err != nil {
		return err
	} else if ok {
		rule["realert"] = value
	}

	if value, ok, err := decodeRawExtension("exponential_realert", r.Spec.ExponentialRealert); err != nil {
		return err
	} else if ok {
		rule["exponential_realert"] = value
	}

	if len(r.Spec.Include) > 0 {
		rule["include"] = r.Spec.Include
	}

	return nil
}

// addOpsgenieFields adds Opsgenie-specific fields to the rule map
func (r *ElastAlertRule) addOpsgenieFields(rule map[string]interface{}) {
	if r.Spec.OpsgenieKey != "" {
		rule["opsgenie_key"] = r.Spec.OpsgenieKey
	}

	if r.Spec.OpsgenieMessage != "" {
		rule["opsgenie_message"] = r.Spec.OpsgenieMessage
	}

	if r.Spec.OpsgeniePriority != "" {
		rule["opsgenie_priority"] = r.Spec.OpsgeniePriority
	}

	if len(r.Spec.OpsgenieTags) > 0 {
		rule["opsgenie_tags"] = r.Spec.OpsgenieTags
	}

	if len(r.Spec.OpsgenieTeams) > 0 {
		rule["opsgenie_teams"] = r.Spec.OpsgenieTeams
	}
}

// addHTTPPostFields adds HTTP POST alerter fields to the rule map
func (r *ElastAlertRule) addHTTPPostFields(rule map[string]interface{}) error {
	if r.Spec.HTTPPostURL != "" {
		rule["http_post_url"] = r.Spec.HTTPPostURL
	}

	if value, ok, err := decodeRawExtension("http_post_payload", r.Spec.HTTPPostPayload); err != nil {
		return err
	} else if ok {
		rule["http_post_payload"] = value
	}

	if value, ok, err := decodeRawExtension("http_post_headers", r.Spec.HTTPPostHeaders); err != nil {
		return err
	} else if ok {
		rule["http_post_headers"] = value
	}

	return nil
}

// addPercentageMatchFields adds percentage_match and new_term rule fields to the rule map
func (r *ElastAlertRule) addPercentageMatchFields(rule map[string]interface{}) error {
	if value, ok, err := decodeRawExtension("buffer_time", r.Spec.BufferTime); err != nil {
		return err
	} else if ok {
		rule["buffer_time"] = value
	}

	if value, ok, err := decodeRawExtension("bucket_interval", r.Spec.BucketInterval); err != nil {
		return err
	} else if ok {
		rule["bucket_interval"] = value
	}

	if r.Spec.MinDenominator != nil {
		rule["min_denominator"] = *r.Spec.MinDenominator
	}

	if r.Spec.MinPercentage != nil {
		rule["min_percentage"] = *r.Spec.MinPercentage
	}

	if filterList, err := decodeRawExtensionList("match_bucket_filter", r.Spec.MatchBucketFilter); err != nil {
		return err
	} else if len(filterList) > 0 {
		rule["match_bucket_filter"] = filterList
	}

	if len(r.Spec.Fields) > 0 {
		rule["fields"] = r.Spec.Fields
	}

	return nil
}

// decodeRawExtensionList decodes a slice of RawExtensions, failing on the first
// malformed entry. Empty entries are skipped.
func decodeRawExtensionList(field string, exts []runtime.RawExtension) ([]interface{}, error) {
	if len(exts) == 0 {
		return nil, nil
	}
	list := make([]interface{}, 0, len(exts))
	for i := range exts {
		if len(exts[i].Raw) == 0 {
			continue
		}
		var item interface{}
		if err := json.Unmarshal(exts[i].Raw, &item); err != nil {
			return nil, fmt.Errorf("invalid %s[%d]: %w", field, i, err)
		}
		list = append(list, item)
	}
	return list, nil
}

// addFilterField adds the filter field to the rule map
func (r *ElastAlertRule) addFilterField(rule map[string]interface{}) error {
	filterList, err := decodeRawExtensionList("filter", r.Spec.Filter)
	if err != nil {
		return err
	}
	if len(filterList) > 0 {
		rule["filter"] = filterList
	}
	return nil
}

// addAdditionalFields merges additional fields into the rule map
func (r *ElastAlertRule) addAdditionalFields(rule map[string]interface{}) error {
	if r.Spec.AdditionalFields == nil || len(r.Spec.AdditionalFields.Raw) == 0 {
		return nil
	}

	var additional map[string]interface{}
	if err := json.Unmarshal(r.Spec.AdditionalFields.Raw, &additional); err != nil {
		return fmt.Errorf("invalid additionalFields: %w", err)
	}
	for k, v := range additional {
		if _, exists := rule[k]; exists {
			return fmt.Errorf("additionalFields key %q conflicts with a field already set by the spec", k)
		}
		rule[k] = v
	}
	return nil
}

// addSecretRefFields sets the fields backed by Secret values.
func (r *ElastAlertRule) addSecretRefFields(rule, resolved map[string]interface{}) error {
	for _, ref := range r.Spec.SecretRefs {
		value, ok := resolved[ref.Field]
		if !ok {
			return fmt.Errorf("secretRef for field %q was not resolved", ref.Field)
		}
		if _, exists := rule[ref.Field]; exists {
			return fmt.Errorf("secretRef field %q conflicts with a field already set by the spec", ref.Field)
		}
		rule[ref.Field] = value
	}
	return nil
}

// AdditionalFieldsMap decodes spec.additionalFields.
func (r *ElastAlertRule) AdditionalFieldsMap() (map[string]interface{}, error) {
	if r.Spec.AdditionalFields == nil || len(r.Spec.AdditionalFields.Raw) == 0 {
		return nil, nil
	}
	var additional map[string]interface{}
	if err := json.Unmarshal(r.Spec.AdditionalFields.Raw, &additional); err != nil {
		return nil, fmt.Errorf("invalid additionalFields: %w", err)
	}
	return additional, nil
}
