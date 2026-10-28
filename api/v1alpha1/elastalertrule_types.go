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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Target kinds supported for TargetConfigMap.Kind.
const (
	TargetKindConfigMap = "ConfigMap"
	TargetKindSecret    = "Secret"
)

// TargetConfigMap defines the ConfigMap or Secret where the ElastAlert rule is written.
// The operator only writes to targets on its allowlist.
type TargetConfigMap struct {
	// Name of the target object
	// +kubebuilder:default="elastalert-rules"
	// +kubebuilder:validation:MaxLength=253
	// +optional
	Name string `json:"name,omitempty"`

	// Namespace of the target object. Defaults to the CR namespace if not specified
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Kind of the target object. Rules that use secretRefs must target a Secret
	// +kubebuilder:validation:Enum=ConfigMap;Secret
	// +kubebuilder:default=ConfigMap
	// +optional
	Kind string `json:"kind,omitempty"`
}

// SecretRef injects the value of a Secret key into a top-level rule field.
// The Secret must be in the rule namespace and carry the label
// alerts.example.dev/allow-source=true.
type SecretRef struct {
	// Field is the top-level ElastAlert field to set (e.g. opsgenie_key, http_post_headers)
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9_]*$`
	// +kubebuilder:validation:MaxLength=63
	Field string `json:"field"`

	// Name of the Secret
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// Key within the Secret
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`

	// Format of the value: "string" uses it as is, "yaml" parses it into a structured value
	// +kubebuilder:validation:Enum=string;yaml
	// +kubebuilder:default=string
	// +optional
	Format string `json:"format,omitempty"`
}

// ElastAlertRuleSpec defines the desired state of ElastAlertRule
// Fields are named to match ElastAlert's YAML format for consistency
// +kubebuilder:validation:XValidation:rule="self.type != 'frequency' || has(self.num_events)",message="num_events is required for frequency rules"
// +kubebuilder:validation:XValidation:rule="!has(self.secretRefs) || size(self.secretRefs) == 0 || (has(self.targetConfigMap) && has(self.targetConfigMap.kind) && self.targetConfigMap.kind == 'Secret')",message="rules that use secretRefs must set targetConfigMap.kind to Secret"
type ElastAlertRuleSpec struct {
	// Type is the ElastAlert built-in rule type
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=any;blacklist;whitelist;change;frequency;spike;flatline;new_term;cardinality;metric_aggregation;spike_aggregation;percentage_match
	Type string `json:"type"`

	// Index is the OpenSearch/Elasticsearch index pattern to query
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Index string `json:"index"`

	// Filter is the query filter as a list of filter clauses
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Filter []runtime.RawExtension `json:"filter,omitempty"`

	// Timeframe is the time window for rule evaluation
	// Can be specified as an object (e.g., {minutes: 5}, {hours: 1}) or string (e.g., "5m", "1h")
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Timeframe *runtime.RawExtension `json:"timeframe,omitempty"`

	// NumEvents is the number of events to trigger the alert (used by frequency rule type)
	// +kubebuilder:validation:Minimum=1
	// +optional
	NumEvents *int `json:"num_events,omitempty"`

	// QueryKey is the field to group by
	// +optional
	QueryKey string `json:"query_key,omitempty"`

	// AlertText is the text to include in the alert
	// +optional
	AlertText string `json:"alert_text,omitempty"`

	// AlertTextArgs are the fields to use in alert_text formatting
	// +optional
	AlertTextArgs []string `json:"alert_text_args,omitempty"`

	// Alert is the list of alerters to use (e.g., ["email"], ["opsgenie"])
	// +optional
	Alert []string `json:"alert,omitempty"`

	// Realert defines the minimum time between alerts
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Realert *runtime.RawExtension `json:"realert,omitempty"`

	// ExponentialRealert defines exponential backoff for realerting
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	ExponentialRealert *runtime.RawExtension `json:"exponential_realert,omitempty"`

	// Include is the list of fields to include in alerts
	// +optional
	Include []string `json:"include,omitempty"`

	// OpsgenieKey is the Opsgenie API key
	// +optional
	OpsgenieKey string `json:"opsgenie_key,omitempty"`

	// OpsgenieMessage is the Opsgenie alert message
	// +optional
	OpsgenieMessage string `json:"opsgenie_message,omitempty"`

	// OpsgeniePriority is the Opsgenie alert priority (P1-P5)
	// +optional
	OpsgeniePriority string `json:"opsgenie_priority,omitempty"`

	// OpsgenieTags are the tags to add to Opsgenie alerts
	// +optional
	OpsgenieTags []string `json:"opsgenie_tags,omitempty"`

	// OpsgenieTeams are the Opsgenie teams to notify
	// +optional
	OpsgenieTeams []string `json:"opsgenie_teams,omitempty"`

	// HTTPPostURL is the URL to POST alerts to
	// +optional
	HTTPPostURL string `json:"http_post_url,omitempty"`

	// HTTPPostPayload defines the payload structure for HTTP POST alerts
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	HTTPPostPayload *runtime.RawExtension `json:"http_post_payload,omitempty"`

	// HTTPPostHeaders defines headers for HTTP POST alerts
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	HTTPPostHeaders *runtime.RawExtension `json:"http_post_headers,omitempty"`

	// BufferTime is the time buffer for queries (percentage_match, metric aggregation rules)
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	BufferTime *runtime.RawExtension `json:"buffer_time,omitempty"`

	// BucketInterval is the time bucket interval for percentage calculation (percentage_match rules)
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	BucketInterval *runtime.RawExtension `json:"bucket_interval,omitempty"`

	// MinDenominator is the minimum number of documents for percentage calculation (percentage_match rules)
	// +optional
	MinDenominator *int `json:"min_denominator,omitempty"`

	// MinPercentage is the minimum percentage threshold to trigger alert (percentage_match rules)
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +optional
	MinPercentage *int `json:"min_percentage,omitempty"`

	// MatchBucketFilter defines the filter for the match bucket in percentage calculations (percentage_match rules)
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	MatchBucketFilter []runtime.RawExtension `json:"match_bucket_filter,omitempty"`

	// Fields is the list of fields to monitor (new_term rules)
	// +optional
	Fields []string `json:"fields,omitempty"`

	// Name is the rule name (optional, defaults to <name>)
	// +optional
	Name string `json:"name,omitempty"`

	// TargetConfigMap specifies where to write the rule. It cannot be changed after creation
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="targetConfigMap is immutable; delete and recreate the rule to move it"
	// +kubebuilder:default={}
	// +optional
	TargetConfigMap TargetConfigMap `json:"targetConfigMap,omitempty"`

	// KeyName is the target key name. Defaults to "<namespace>_<name>.yaml"
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[-._a-zA-Z0-9]+\.ya?ml$`
	// +optional
	KeyName string `json:"keyName,omitempty"`

	// SecretRefs inject rule fields from Secrets so credentials stay out of the resource
	// +kubebuilder:validation:MaxItems=20
	// +listType=atomic
	// +optional
	SecretRefs []SecretRef `json:"secretRefs,omitempty"`

	// AdditionalFields allows specifying any other ElastAlert fields not explicitly defined above.
	// Keys that conflict with spec fields or are blocked by the operator policy are rejected
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Type=object
	// +optional
	AdditionalFields *runtime.RawExtension `json:"additionalFields,omitempty"`
}

// ElastAlertRuleStatus defines the observed state of ElastAlertRule.
type ElastAlertRuleStatus struct {
	// Conditions represent the current state of the ElastAlertRule resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastReconciled is the timestamp of the last time the written rule changed
	// +optional
	LastReconciled *metav1.Time `json:"lastReconciled,omitempty"`

	// ObservedGeneration is the generation the conditions were computed from
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// RuleChecksum is the SHA-256 of the rendered rule last written to the target
	// +optional
	RuleChecksum string `json:"ruleChecksum,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ear
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Index",type=string,JSONPath=`.spec.index`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ElastAlertRule is the Schema for the elastalertrules API
type ElastAlertRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of ElastAlertRule
	// +required
	Spec ElastAlertRuleSpec `json:"spec"`

	// status defines the observed state of ElastAlertRule
	// +optional
	Status ElastAlertRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// ElastAlertRuleList contains a list of ElastAlertRule
type ElastAlertRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElastAlertRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ElastAlertRule{}, &ElastAlertRuleList{})
}

// Condition types
const (
	// ConditionTypeReady indicates the rule has been successfully written to the ConfigMap
	ConditionTypeReady = "Ready"
	// ConditionTypeValidationFailed indicates the rule spec failed validation
	ConditionTypeValidationFailed = "ValidationFailed"
	// ConditionTypeError indicates an error occurred during reconciliation
	ConditionTypeError = "Error"
	// ConditionTypePlaintextCredentials indicates the spec carries credentials inline instead of via secretRefs
	ConditionTypePlaintextCredentials = "PlaintextCredentials"
)

// Finalizer name
const (
	FinalizerName = "alerts.example.dev/finalizer"
)

// GetTargetKind returns the target kind, defaulting to ConfigMap
func (r *ElastAlertRule) GetTargetKind() string {
	if r.Spec.TargetConfigMap.Kind != "" {
		return r.Spec.TargetConfigMap.Kind
	}
	return TargetKindConfigMap
}

// GetConfigMapName returns the target ConfigMap name, defaulting to "elastalert-rules"
func (r *ElastAlertRule) GetConfigMapName() string {
	if r.Spec.TargetConfigMap.Name != "" {
		return r.Spec.TargetConfigMap.Name
	}
	return "elastalert-rules"
}

// GetConfigMapNamespace returns the target ConfigMap namespace, defaulting to the CR namespace
func (r *ElastAlertRule) GetConfigMapNamespace() string {
	if r.Spec.TargetConfigMap.Namespace != "" {
		return r.Spec.TargetConfigMap.Namespace
	}
	return r.Namespace
}

// GetKeyName returns the target key name, defaulting to "<namespace>_<name>.yaml"
func (r *ElastAlertRule) GetKeyName() string {
	if r.Spec.KeyName != "" {
		return r.Spec.KeyName
	}
	return r.Namespace + "_" + r.Name + ".yaml"
}

// GetRuleName returns the ElastAlert rule name, using spec.name if set, otherwise "<namespace>-<name>"
func (r *ElastAlertRule) GetRuleName() string {
	if r.Spec.Name != "" {
		return r.Spec.Name
	}
	return r.Namespace + "-" + r.Name
}
