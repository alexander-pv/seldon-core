/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package util

import (
	"github.com/cenkalti/backoff/v4"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"
)

func GetClientKeepAliveParameters() keepalive.ClientParameters {
	return keepalive.ClientParameters{
		Time:                gRPCKeepAliveTime,
		Timeout:             clientKeepAliveTimeout,
		PermitWithoutStream: gRPCKeepAlivePermit,
	}
}

func GetServerKeepAliveEnforcementPolicy() keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{
		MinTime:             gRPCKeepAliveTime,
		PermitWithoutStream: gRPCKeepAlivePermit,
	}
}

func GetClientExponentialBackoff() *backoff.ExponentialBackOff {
	backOffExp := backoff.NewExponentialBackOff()
	backOffExp.MaxElapsedTime = backOffExpMaxElapsedTime
	backOffExp.MaxInterval = backOffExpMaxInterval
	backOffExp.InitialInterval = backOffExpInitialInterval
	return backOffExp
}

// GRPCModelNotFoundError returns a NotFound status naming the model (unknown / unrouted).
func GRPCModelNotFoundError(modelName string) error {
	return status.Errorf(codes.NotFound, "model %q does not exist or is not routed in the mesh", modelName)
}

// EnrichGRPCMeshModelError maps mesh-side gRPC errors for an unknown/unrouted model to NotFound.
// UNIMPLEMENTED from Envoy (no route match) becomes NotFound with the model name in the message.
func EnrichGRPCMeshModelError(modelName string, err error) error {
	return enrichGRPCModelError(modelName, err)
}

// EnrichGRPCModelReadyError adds the model name to client-facing readiness errors.
func EnrichGRPCModelReadyError(modelName string, err error) error {
	return enrichGRPCModelError(modelName, err)
}

func enrichGRPCModelError(modelName string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return status.Errorf(codes.Unknown, "model %q: %v", modelName, err)
	}
	switch st.Code() {
	case codes.Unimplemented:
		return GRPCModelNotFoundError(modelName)
	case codes.NotFound:
		if st.Message() != "" {
			return status.Errorf(codes.NotFound, "model %q: %s", modelName, st.Message())
		}
		return GRPCModelNotFoundError(modelName)
	default:
		if st.Message() != "" {
			return status.Errorf(st.Code(), "model %q: %s", modelName, st.Message())
		}
		return status.Errorf(st.Code(), "model %q readiness check failed", modelName)
	}
}

// IsGRPCModelNotReadyError reports whether err from ModelReady should be treated as not-ready
// (return Ready=false) rather than propagated as a gRPC error to clients.
func IsGRPCModelNotReadyError(err error) bool {
	if err == nil {
		return false
	}
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.NotFound, codes.Unimplemented, codes.Unavailable, codes.FailedPrecondition:
		return true
	default:
		return false
	}
}
