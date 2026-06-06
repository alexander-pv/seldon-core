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
	"net"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	v2 "github.com/seldonio/seldon-core/apis/go/v2/mlops/v2_dataplane"

	"github.com/seldonio/seldon-core/scheduler/v2/pkg/util"
)

const bufSize = 1024 * 1024

type fakeInferenceServer struct {
	v2.UnimplementedGRPCInferenceServiceServer
	ready        bool
	lastModelHdr string
}

func (f *fakeInferenceServer) ModelReady(ctx context.Context, _ *v2.ModelReadyRequest) (*v2.ModelReadyResponse, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if v := md.Get(util.SeldonModelHeader); len(v) > 0 {
			f.lastModelHdr = v[0]
		}
	}
	return &v2.ModelReadyResponse{Ready: f.ready}, nil
}

func startBufconnServer(t *testing.T, ready bool) (*grpc.ClientConn, func()) {
	conn, cleanup, _ := startBufconnServerWithFake(t, ready)
	return conn, cleanup
}

func startBufconnServerWithFake(t *testing.T, ready bool) (*grpc.ClientConn, func(), *fakeInferenceServer) {
	t.Helper()
	g := NewGomegaWithT(t)
	lis := bufconn.Listen(bufSize)
	fake := &fakeInferenceServer{ready: ready}
	srv := grpc.NewServer()
	v2.RegisterGRPCInferenceServiceServer(srv, fake)
	go func() {
		_ = srv.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}
	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	g.Expect(err).To(BeNil())

	return conn, func() {
		srv.Stop()
		_ = conn.Close()
	}, fake
}

func TestModelGrpcCaller_CheckModelReady(t *testing.T) {
	g := NewGomegaWithT(t)

	conn, cleanup, fake := startBufconnServerWithFake(t, true)
	defer cleanup()

	caller := &ModelGrpcCaller{
		client: v2.NewGRPCInferenceServiceClient(conn),
		logger: logrus.New(),
	}
	ready, err := caller.CheckModelReady(context.Background(), "model-c", "req-1")
	g.Expect(err).To(BeNil())
	g.Expect(ready).To(BeTrue())
	g.Expect(fake.lastModelHdr).To(Equal("model-c"))
}

func TestModelGrpcCaller_CheckModelReady_not_ready(t *testing.T) {
	g := NewGomegaWithT(t)

	conn, cleanup := startBufconnServer(t, false)
	defer cleanup()

	caller := &ModelGrpcCaller{
		client: v2.NewGRPCInferenceServiceClient(conn),
		logger: logrus.New(),
	}
	ready, err := caller.CheckModelReady(context.Background(), "model-d", "req-2")
	g.Expect(err).To(BeNil())
	g.Expect(ready).To(BeFalse())
}

func TestNewModelReadyCaller_defaults_to_grpc(t *testing.T) {
	g := NewGomegaWithT(t)
	_, err := NewModelReadyCaller(logrus.New(), "grpc", "127.0.0.1", 1)
	g.Expect(err).To(BeNil())
}

func TestNewModelReadyCaller_unknown_probe(t *testing.T) {
	g := NewGomegaWithT(t)
	_, err := NewModelReadyCaller(logrus.New(), "invalid", "127.0.0.1", 1)
	g.Expect(err).To(HaveOccurred())
}

