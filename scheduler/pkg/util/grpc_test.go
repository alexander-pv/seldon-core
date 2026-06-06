/*
Copyright (c) 2024 Seldon Technologies Ltd.

Use of this software is governed by
(1) the license included in the LICENSE file or
(2) if the license included in the LICENSE file is the Business Source License 1.1,
the Change License after the Change Date as each is defined in accordance with the LICENSE file.
*/

// Package util tests must run as a package (they need grpc.go in the same package).
// From scheduler/: go test ./pkg/util/ -run TestIsGRPCModelNotReadyError
package util

import (
	"testing"

	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIsGRPCModelNotReadyError(t *testing.T) {
	g := NewGomegaWithT(t)
	g.Expect(IsGRPCModelNotReadyError(status.Error(codes.Unimplemented, ""))).To(BeTrue())
	g.Expect(IsGRPCModelNotReadyError(status.Error(codes.NotFound, ""))).To(BeTrue())
	g.Expect(IsGRPCModelNotReadyError(status.Error(codes.Internal, ""))).To(BeFalse())
	g.Expect(IsGRPCModelNotReadyError(nil)).To(BeFalse())
}

func TestGRPCModelNotFoundError(t *testing.T) {
	g := NewGomegaWithT(t)
	err := GRPCModelNotFoundError("model-d")
	st, ok := status.FromError(err)
	g.Expect(ok).To(BeTrue())
	g.Expect(st.Code()).To(Equal(codes.NotFound))
	g.Expect(st.Message()).To(ContainSubstring("model-d"))
}

func TestEnrichGRPCModelReadyError(t *testing.T) {
	g := NewGomegaWithT(t)
	err := EnrichGRPCModelReadyError("model-x", status.Error(codes.Unimplemented, ""))
	st, ok := status.FromError(err)
	g.Expect(ok).To(BeTrue())
	g.Expect(st.Code()).To(Equal(codes.NotFound))
	g.Expect(st.Message()).To(ContainSubstring("model-x"))
}
