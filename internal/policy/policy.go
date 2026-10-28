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

// Package policy decides which targets an operator instance may write to and
// which rule content it accepts.
package policy

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	alertsv1alpha1 "github.com/AresiusXP/elastalert-rules-operator/api/v1alpha1"
)

// DefaultAlerters are the ElastAlert alerters accepted unless overridden.
// post2 is excluded: ElastAlert renders its payload with an unsandboxed Jinja template.
var DefaultAlerters = []string{"opsgenie", "post", "slack", "msteams", "email", "alertmanager", "debug"}

// allowedFields are the ElastAlert settings accepted in additionalFields and
// secretRefs. Anything else is rejected, so settings that execute code, load
// files or redirect ElastAlert elsewhere can never be reached by default.
var allowedFields = setOf(
	"aggregation", "aggregation_key", "alert_subject", "alert_subject_args", "alert_time_limit",
	"attach_related", "blacklist", "whitelist", "bcc", "cc", "category", "cardinality_field",
	"compare_key", "description", "doc_type", "email", "email_reply_to", "field", "forget_keys",
	"from_addr", "ignore_null", "is_enabled", "limit_execution", "max_cardinality", "max_query_size",
	"max_threshold", "metric_agg_key", "metric_agg_type", "metric_format_string", "min_cardinality",
	"min_doc_count", "min_threshold", "new_term_fields", "owner", "priority", "query_delay",
	"raw_count_keys", "run_every", "scan_entire_timeframe", "spike_height", "spike_type",
	"summary_prefix", "summary_table_fields", "summary_table_max_rows", "summary_table_type",
	"terms_size", "terms_window_size", "threshold", "threshold_cur", "threshold_ref",
	"timestamp_field", "top_count_keys", "top_count_number", "use_count_query", "use_keyword_postfix",
	"use_local_time", "use_strftime_index", "use_terms_query",
)

// allowedPrefixes admit the option families of the allowed alerters.
var allowedPrefixes = []string{"opsgenie_", "slack_", "msteams_", "alertmanager_", "http_post_"}

// blockedSuffixes reject options inside an allowed family that change TLS
// verification or route traffic through another endpoint.
var blockedSuffixes = []string{"_proxy", "_ca_certs", "_cert", "_certs", "_client_key", "_ignore_ssl_errors"}

// neverAllowed fields stay rejected even when listed in --allowed-additional-fields.
var neverAllowed = setOf(
	"command", "pipe_match_json", "pipe_alert_text", "import", "match_enhancements", "rule_hooks",
	"timestamp_type", "timestamp_format", "timestamp_format_expr", "timestamp_to_datetime_format_expr",
	"alert_text_type", "rules_folder", "scan_subdirectories", "use_ssl", "verify_certs", "ca_certs",
	"client_cert", "client_key", "aws_region", "profile", "type", "alert", "index", "name",
	"alertmanager_labels", "alertmanager_annotations", "opsgenie_details",
)

// endpointFields hold destinations ElastAlert connects to. With a host
// allowlist every one of them must point at an allowed host.
var endpointFields = setOf(
	"http_post_url", "http_post2_url", "opsgenie_addr", "slack_webhook_url",
	"msteams_webhook_url", "alertmanager_hosts",
)

// fileListFields support "!file <path>" entries that make ElastAlert read local files.
var fileListFields = setOf("blacklist", "whitelist")

// formatArgFields name event fields to substitute into alert text. ElastAlert falls
// back to rule settings, including global configuration, for names missing from the event.
var formatArgFields = setOf("alert_text_args", "alert_subject_args")

func setOf(items ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, i := range items {
		out[i] = struct{}{}
	}
	return out
}

// Target identifies one object the operator may write to.
type Target struct {
	Kind      string
	Namespace string
	Name      string
}

func (t Target) String() string { return t.Kind + " " + t.Namespace + "/" + t.Name }

// Policy is the immutable set of rules an operator instance enforces.
type Policy struct {
	Targets          []Target
	Alerters         map[string]struct{}
	IndexPatterns    []string
	HTTPHosts        map[string]struct{}
	ExtraFields      map[string]struct{}
	allowPost2Fields bool
}

// New builds a Policy. Empty alerters selects DefaultAlerters; empty
// indexPatterns or httpHosts means unrestricted. extraFields extends the
// additionalFields allowlist but cannot unblock the never-allowed settings.
func New(targets []Target, alerters, indexPatterns, httpHosts []string, extraFields ...string) Policy {
	if len(alerters) == 0 {
		alerters = DefaultAlerters
	}
	p := Policy{
		Targets:       targets,
		Alerters:      toSet(alerters),
		IndexPatterns: indexPatterns,
		HTTPHosts:     toSet(httpHosts),
		ExtraFields:   toSet(extraFields),
	}
	_, p.allowPost2Fields = p.Alerters["post2"]
	return p
}

// ParseTargets parses a comma separated list of "namespace/name" entries.
func ParseTargets(kind, csv string) ([]Target, error) {
	var out []Target
	for _, item := range splitList(csv) {
		ns, name, ok := strings.Cut(item, "/")
		if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
			return nil, fmt.Errorf("invalid target %q, want namespace/name", item)
		}
		out = append(out, Target{Kind: kind, Namespace: ns, Name: name})
	}
	return out, nil
}

// SplitList splits a comma separated flag value, dropping empty entries.
func SplitList(csv string) []string { return splitList(csv) }

// TargetAllowed reports whether the operator may write to the given object.
func (p Policy) TargetAllowed(kind, namespace, name string) bool {
	for _, t := range p.Targets {
		if t.Kind == kind && t.Namespace == namespace && t.Name == name {
			return true
		}
	}
	return false
}

// Namespaces returns the sorted namespaces that hold targets of the given kind.
func (p Policy) Namespaces(kind string) []string {
	seen := map[string]struct{}{}
	for _, t := range p.Targets {
		if t.Kind == kind {
			seen[t.Namespace] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ns := range seen {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

// CheckTarget returns an error when the rule's target is not on the allowlist.
func (p Policy) CheckTarget(rule *alertsv1alpha1.ElastAlertRule) error {
	kind, ns, name := rule.GetTargetKind(), rule.GetConfigMapNamespace(), rule.GetConfigMapName()
	if !p.TargetAllowed(kind, ns, name) {
		return fmt.Errorf("target %s %s/%s is not allowed by the operator", kind, ns, name)
	}
	return nil
}

// CheckRule validates rule content against the policy.
func (p Policy) CheckRule(rule *alertsv1alpha1.ElastAlertRule) error {
	spec := &rule.Spec
	for _, alerter := range spec.Alert {
		if _, ok := p.Alerters[alerter]; !ok {
			return fmt.Errorf("alerter %q is not allowed; allowed: %s", alerter, strings.Join(sortedKeys(p.Alerters), ", "))
		}
	}

	if len(p.IndexPatterns) > 0 {
		for _, idx := range splitList(spec.Index) {
			if !matchesAny(p.IndexPatterns, idx) {
				return fmt.Errorf("index %q is not allowed by the operator", idx)
			}
		}
	}

	additional, err := rule.AdditionalFieldsMap()
	if err != nil {
		return err
	}
	for key, value := range additional {
		if err := p.checkFieldName("additionalFields", key); err != nil {
			return err
		}
		if err := checkValue(key, value); err != nil {
			return err
		}
	}
	if err := checkValue("alert_text_args", stringsToAny(spec.AlertTextArgs)); err != nil {
		return err
	}

	for _, ref := range spec.SecretRefs {
		if err := p.checkFieldName("secretRefs", ref.Field); err != nil {
			return err
		}
	}
	return nil
}

// CheckRendered validates the final rule, after secret values were merged in.
func (p Policy) CheckRendered(rule map[string]interface{}) error {
	for key, value := range rule {
		if err := checkValue(key, value); err != nil {
			return err
		}
		if len(p.HTTPHosts) == 0 || !p.isEndpointField(key) {
			continue
		}
		for _, raw := range endpointValues(value) {
			if raw == "" {
				return fmt.Errorf("%s must be a string or a list of strings", key)
			}
			if err := p.checkHost(key, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p Policy) isEndpointField(key string) bool {
	if _, ok := endpointFields[key]; ok {
		return true
	}
	if _, ok := p.ExtraFields[key]; ok {
		for _, suffix := range []string{"_url", "_urls", "_hosts", "_addr"} {
			if strings.HasSuffix(key, suffix) {
				return true
			}
		}
	}
	return false
}

// checkValue rejects values that make ElastAlert read local files or expose configuration.
func checkValue(key string, value interface{}) error {
	items, _ := value.([]interface{})
	if _, ok := fileListFields[key]; ok {
		for _, item := range items {
			if str, ok := item.(string); ok && strings.HasPrefix(strings.TrimSpace(str), "!file") {
				return fmt.Errorf("%s entries must not reference files", key)
			}
		}
	}
	if _, ok := formatArgFields[key]; ok {
		for _, item := range items {
			if str, ok := item.(string); ok && isConfigReference(str) {
				return fmt.Errorf("%s must not reference ElastAlert settings such as %q", key, str)
			}
		}
	}
	return nil
}

// isConfigReference reports whether name is an ElastAlert setting rather than an event field.
func isConfigReference(name string) bool {
	if _, never := neverAllowed[name]; never {
		return true
	}
	for _, prefix := range append([]string{"es_", "smtp_", "aws_"}, allowedPrefixes...) {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func stringsToAny(in []string) []interface{} {
	out := make([]interface{}, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

// endpointValues flattens a string or list of strings. Any other shape yields a
// single empty string so the caller rejects it.
func endpointValues(value interface{}) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return []string{""}
			}
			out = append(out, s)
		}
		return out
	case []string:
		return v
	default:
		return []string{""}
	}
}

func (p Policy) checkHost(field, raw string) error {
	if len(p.HTTPHosts) == 0 {
		return nil
	}
	candidate := raw
	if !strings.Contains(candidate, "://") {
		candidate = "//" + candidate
	}
	u, err := url.Parse(candidate)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%s is not a valid URL", field)
	}
	if _, ok := p.HTTPHosts[strings.ToLower(u.Hostname())]; !ok {
		return fmt.Errorf("%s points at a host that is not allowed by the operator", field)
	}
	return nil
}

func (p Policy) checkFieldName(where, key string) error {
	if _, blocked := neverAllowed[key]; blocked || strings.HasPrefix(key, "es_") {
		return fmt.Errorf("%s key %q is not allowed", where, key)
	}
	if p.fieldAllowed(key) {
		return nil
	}
	return fmt.Errorf("%s key %q is not on the allowed field list", where, key)
}

func (p Policy) fieldAllowed(key string) bool {
	if _, ok := allowedFields[key]; ok {
		return true
	}
	if _, ok := p.ExtraFields[key]; ok {
		return true
	}
	prefixes := allowedPrefixes
	if p.allowPost2Fields {
		prefixes = append(append([]string{}, prefixes...), "http_post2_")
	}
	for _, prefix := range prefixes {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for _, suffix := range blockedSuffixes {
			if strings.HasSuffix(key, suffix) {
				return false
			}
		}
		return true
	}
	return false
}

func matchesAny(patterns []string, value string) bool {
	for _, pat := range patterns {
		if ok, err := path.Match(pat, value); err == nil && ok {
			return true
		}
	}
	return false
}

func toSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, i := range items {
		out[strings.ToLower(i)] = struct{}{}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func splitList(csv string) []string {
	var out []string
	for _, item := range strings.Split(csv, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
