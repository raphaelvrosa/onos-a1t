// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

package sbclient

import (
	"context"
	"fmt"
	"github.com/onosproject/onos-a1t/pkg/stream"
	"github.com/onosproject/onos-lib-go/pkg/grpc/retry"
	"github.com/onosproject/onos-ric-sdk-go/pkg/utils/creds"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"time"
)

const GRPCTimeout = time.Second * 5

func createGRPCConn(ipAddress string, port uint32) (*grpc.ClientConn, error) {
	tlsConfig, err := creds.GetClientCredentials()
	if err != nil {
		return nil, err
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
		grpc.WithUnaryInterceptor(retry.RetryingUnaryClientInterceptor()),
	}

	return grpc.Dial(fmt.Sprintf("%s:%d", ipAddress, port), opts...)
}

func createStream(ctx context.Context, xAppID string, a1Service stream.A1Service, streamBroker stream.Broker) {
	switch a1Service {
	case stream.PolicyManagement:
		sbID, nbID := stream.GetStreamID(stream.A1PController, stream.GetEndpointIDWithTargetXAppID(xAppID, stream.PolicyManagement))
		streamBroker.AddStream(ctx, nbID)
		streamBroker.AddStream(ctx, sbID)
	case stream.EnrichmentInformation:
		sbID, nbID := stream.GetStreamID(stream.A1EIController, stream.GetEndpointIDWithTargetXAppID(xAppID, stream.PolicyManagement))
		streamBroker.AddStream(ctx, nbID)
		streamBroker.AddStream(ctx, sbID)
	}
}

func deleteStream(xAppID string, a1Service stream.A1Service, streamBroker stream.Broker) {
	switch a1Service {
	case stream.PolicyManagement:
		sbID, nbID := stream.GetStreamID(stream.A1PController, stream.GetEndpointIDWithTargetXAppID(xAppID, stream.PolicyManagement))
		streamBroker.Close(nbID)
		streamBroker.Close(sbID)
	case stream.EnrichmentInformation:
		sbID, nbID := stream.GetStreamID(stream.A1EIController, stream.GetEndpointIDWithTargetXAppID(xAppID, stream.PolicyManagement))
		streamBroker.Close(nbID)
		streamBroker.Close(sbID)
	}
}
