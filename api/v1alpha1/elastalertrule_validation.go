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
	"strings"
)

// ValidateSpec validates the ElastAlertRule spec
func (r *ElastAlertRule) ValidateSpec() error {
	// Required fields
	if r.Spec.Type == "" {
		return fmt.Errorf("spec.type is required")
	}
	if r.Spec.Index == "" {
		return fmt.Errorf("spec.index is required")
	}

	// Timeframe validation is not needed since it's a RawExtension
	// ElastAlert will validate the format

	// Validate num_events for frequency rule type
	if r.Spec.Type == "frequency" {
		if r.Spec.NumEvents == nil || *r.Spec.NumEvents < 1 {
			return fmt.Errorf("spec.num_events must be >= 1 for frequency rule type")
		}
	}

	// Validate keyName doesn't contain path separators
	if r.Spec.KeyName != "" && strings.Contains(r.Spec.KeyName, "/") {
		return fmt.Errorf("spec.keyName cannot contain path separators")
	}

	return nil
}
