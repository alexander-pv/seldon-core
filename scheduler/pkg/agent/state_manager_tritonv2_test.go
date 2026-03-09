/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package agent

import (
	"testing"

	log "github.com/sirupsen/logrus"
)

func TestModelNameForInferenceBackend(t *testing.T) {
	logger := log.StandardLogger()
	modelState := NewModelState()

	tests := []struct {
		name                      string
		useLogicalNameModelLayout bool
		internalModelName         string
		want                      string
	}{
		{"logical layout versioned name", true, "model-a_1", "model-a"},
		{"logical layout logical name", true, "model-a", "model-a"},
		{"versioned layout versioned name", false, "model-a_1", "model-a_1"},
		{"versioned layout logical name", false, "model-a", "model-a"},
		{"logical layout model-b_2", true, "model-b_2", "model-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewLocalStateManager(
				modelState,
				logger,
				nil,
				1024,
				0,
				nil,
				tt.useLogicalNameModelLayout,
			)
			got := manager.ModelNameForInferenceBackend(tt.internalModelName)
			if got != tt.want {
				t.Errorf("ModelNameForInferenceBackend(useLogical=%v, %q) = %q, want %q",
					tt.useLogicalNameModelLayout, tt.internalModelName, got, tt.want)
			}
		})
	}
}
