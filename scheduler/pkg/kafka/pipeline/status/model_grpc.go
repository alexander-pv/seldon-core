/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

package status

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	v2 "github.com/seldonio/seldon-core/apis/go/v2/mlops/v2_dataplane"

	"github.com/seldonio/seldon-core/scheduler/v2/pkg/util"
)

// ModelGrpcCaller probes step model readiness via OIP gRPC (same path clients use through Envoy).
type ModelGrpcCaller struct {
	client v2.GRPCInferenceServiceClient
	conn   *grpc.ClientConn
	logger logrus.FieldLogger
}

func NewModelGrpcCaller(logger logrus.FieldLogger, host string, port int) (*ModelGrpcCaller, error) {
	tlsOptions, err := util.CreateTLSClientOptions()
	if err != nil {
		return nil, err
	}

	target := net.JoinHostPort(host, strconv.Itoa(port))
	opts := []grpc.DialOption{
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.DefaultConfig,
		}),
		grpc.WithKeepaliveParams(util.GetClientKeepAliveParameters()),
	}
	if tlsOptions.TLS {
		opts = append(opts, grpc.WithTransportCredentials(tlsOptions.Cert.CreateClientTransportCredentials()))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("create gRPC client for model ready probe at %s: %w", target, err)
	}

	return &ModelGrpcCaller{
		client: v2.NewGRPCInferenceServiceClient(conn),
		conn:   conn,
		logger: logger.WithField("source", "ModelGrpcCaller"),
	}, nil
}

func (mg *ModelGrpcCaller) CheckModelReady(ctx context.Context, modelName string, requestId string) (bool, error) {
	logger := mg.logger.WithField("func", "CheckModelReady")

	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		util.SeldonModelHeader, modelName,
		util.RequestIdHeader, requestId,
	))
	resp, err := mg.client.ModelReady(ctx, &v2.ModelReadyRequest{Name: modelName})
	if err != nil {
		if util.IsGRPCModelNotReadyError(err) {
			logger.WithError(err).Warnf("gRPC ModelReady for model %s not ready", modelName)
			return false, nil
		}
		logger.WithError(err).Warnf("gRPC ModelReady failed for model %s", modelName)
		return false, util.EnrichGRPCModelReadyError(modelName, err)
	}
	logger.Infof("gRPC ModelReady for model %s ready=%v", modelName, resp.GetReady())
	return resp.GetReady(), nil
}
