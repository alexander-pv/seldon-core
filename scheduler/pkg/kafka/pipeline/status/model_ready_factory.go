/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package status

import (
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
)

const (
	// ModelReadyProbeGrpc probes step models via OIP gRPC through Envoy (recommended).
	ModelReadyProbeGrpc = "grpc"
	// ModelReadyProbeHttp probes step models via HTTP GET /v2/models/{name}/ready.
	ModelReadyProbeHttp = "http"
)

// NewModelReadyCaller returns the step-model readiness implementation for pipeline ModelReady.
func NewModelReadyCaller(logger logrus.FieldLogger, probe string, host string, port int) (ModelReadyCaller, error) {
	switch strings.ToLower(strings.TrimSpace(probe)) {
	case ModelReadyProbeHttp:
		return NewModelRestStatusCaller(logger, host, port)
	case ModelReadyProbeGrpc, "":
		return NewModelGrpcCaller(logger, host, port)
	default:
		return nil, fmt.Errorf("unknown model-ready-probe %q (use %q or %q)", probe, ModelReadyProbeGrpc, ModelReadyProbeHttp)
	}
}
