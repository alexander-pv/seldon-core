/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package main

import "testing"

func TestServerTypeUsesLogicalModelLayout(t *testing.T) {
	tests := []struct {
		serverType string
		want      bool
	}{
		{"tritonv2", true},
		{"triton", false},
		{"mlserver", false},
		{"", false},
		{"TritonV2", false},
	}
	for _, tt := range tests {
		t.Run(tt.serverType, func(t *testing.T) {
			if got := serverTypeUsesLogicalModelLayout(tt.serverType); got != tt.want {
				t.Errorf("serverTypeUsesLogicalModelLayout(%q) = %v, want %v", tt.serverType, got, tt.want)
			}
		})
	}
}
