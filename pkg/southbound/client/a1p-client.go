// SPDX-FileCopyrightText: 2020-present Open Networking Foundation <info@opennetworking.org>
//
// SPDX-License-Identifier: Apache-2.0

package sbclient

import (
	"context"
	"github.com/google/uuid"
	"github.com/onosproject/onos-a1t/pkg/stream"
	"github.com/onosproject/onos-api/go/onos/a1t/a1"
	"github.com/onosproject/onos-lib-go/pkg/errors"
	"github.com/onosproject/onos-lib-go/pkg/logging"
	"io"
	"time"
)

var a1pLog = logging.GetLogger("southbound", "a1p-client")

func NewA1PClient(ctx context.Context, targetXAppID string, ipAddress string, port uint32, streamBroker stream.Broker) (Client, error) {
	// create gRPC client
	conn, err := createGRPCConn(ipAddress, port)
	if err != nil {
		return nil, err
	}

	// create broker session
	createStream(ctx, targetXAppID, stream.PolicyManagement, streamBroker)

	return &a1pClient{
		targetXAppID: targetXAppID,
		ipAddress:    ipAddress,
		port:         port,
		grpcClient:   a1.NewPolicyServiceClient(conn),
		streamBroker: streamBroker,
		sessions:     make(map[stream.A1SBIRPCType]interface{}),
	}, nil
}

type a1pClient struct {
	targetXAppID string
	ipAddress    string
	port         uint32
	grpcClient   a1.PolicyServiceClient
	streamBroker stream.Broker
	sessions     map[stream.A1SBIRPCType]interface{}
}

func (a *a1pClient) Run(ctx context.Context) error {
	err := a.createSessions(ctx)
	if err != nil {
		return err
	}

	a.runIncomingMsgForwarder(ctx)

	err = a.runOutgoingMsgDispatcher(ctx)

	if err != nil {
		a1pLog.Warn(err)
		a.Close()
		return err
	}
	return nil
}

func (a *a1pClient) createSessions(ctx context.Context) error {
	err := a.createPolicyStatusSession(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (a *a1pClient) createPolicyStatusSession(ctx context.Context) error {
	s, err := a.grpcClient.PolicyStatus(ctx)
	if err != nil {
		return err
	}

	a.sessions[stream.PolicyStatus] = s
	return nil
}

func (a *a1pClient) runIncomingMsgForwarder(ctx context.Context) {
	go a.incomingPolicyStatusForwarder(ctx)
}

func (a *a1pClient) incomingPolicyStatusForwarder(ctx context.Context) {
	defer a.Close()
	for {
		select {
		case <-ctx.Done():
			a1pLog.Warn("A1P SBI client incoming forwarder for Policy Status service is just closed")
			return
		default:
			if _, ok := a.sessions[stream.PolicyStatus]; !ok {
				a1pLog.Warn("A1P SBI client incoming forwarder for Policy Status service is just closed")
				return
			}
			msg, err := a.sessions[stream.PolicyStatus].(a1.PolicyService_PolicyStatusClient).Recv()
			if err == io.EOF || err == context.Canceled {
				a1pLog.Warn("A1P SBI client incoming forwarder for Policy Status service is just closed")
				return
			}
			if err != nil {
				a1pLog.Warn(err)
				return
			}
			sbMessage := stream.NewSBStreamMessage(a.targetXAppID, stream.PolicyStatusMessage, stream.PolicyStatus, stream.PolicyManagement, msg)
			_, nbID := stream.GetStreamID(stream.A1PController, stream.GetEndpointIDWithTargetXAppID(a.targetXAppID, stream.PolicyManagement))
			err = a.streamBroker.Send(nbID, sbMessage)
			if err != nil {
				a1pLog.Warn(err)
			}
		}
	}
}

func (a *a1pClient) runOutgoingMsgDispatcher(ctx context.Context) error {
	msgCh := make(chan *stream.SBStreamMessage)
	sbID, _ := stream.GetStreamID(stream.A1PController, stream.GetEndpointIDWithTargetXAppID(a.targetXAppID, stream.PolicyManagement))
	watcherID := uuid.New()
	err := a.streamBroker.Watch(sbID, msgCh, watcherID)
	if err != nil {
		return err
	}

	go func(msgCh chan *stream.SBStreamMessage) {
		for msg := range msgCh {
			go a.outgoingMsgDispatcher(ctx, msg)
		}
	}(msgCh)

	<-ctx.Done()
	return errors.NewCanceled("A1P SBI client outgoing message dispatcher is just closed - due to the context done")
}

func (a *a1pClient) outgoingMsgDispatcher(ctx context.Context, msg *stream.SBStreamMessage) {
	a1pLog.Infof("Received message from controller: %v", *msg)
	tCtx, tCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer tCancel()
	var err error
	switch msg.A1SBIRPCType {
	case stream.PolicySetup:
		a1pLog.Info("Sending PolicySetup Request message")
		result, err := a.grpcClient.PolicySetup(tCtx, msg.Payload.(*a1.PolicyRequestMessage))
		if err != nil {
			a1pLog.Warn(err)
		}
		a.forwardResponseMsg(result, stream.PolicyResultMessage, stream.PolicySetup)
	case stream.PolicyUpdate:
		a1pLog.Info("Sending PolicyUpdate Request message")
		result, err := a.grpcClient.PolicyUpdate(tCtx, msg.Payload.(*a1.PolicyRequestMessage))
		if err != nil {
			a1pLog.Warn(err)
		}
		a.forwardResponseMsg(result, stream.PolicyResultMessage, stream.PolicyUpdate)
	case stream.PolicyDelete:
		a1pLog.Info("Sending PolicyDelete Request message")
		result, err := a.grpcClient.PolicyDelete(tCtx, msg.Payload.(*a1.PolicyRequestMessage))
		if err != nil {
			a1pLog.Warn(err)
		}
		a.forwardResponseMsg(result, stream.PolicyResultMessage, stream.PolicyDelete)
	case stream.PolicyQuery:
		a1pLog.Info("Sending PolicyQuery Request message")
		result, err := a.grpcClient.PolicyQuery(tCtx, msg.Payload.(*a1.PolicyRequestMessage))
		if err != nil {
			a1pLog.Warn(err)
		}
		a.forwardResponseMsg(result, stream.PolicyResultMessage, stream.PolicyQuery)
	case stream.PolicyStatus:
		a1pLog.Info("Sending PolicAck message")
		err = a.sessions[stream.PolicyStatus].(a1.PolicyService_PolicyStatusClient).Send(msg.Payload.(*a1.PolicyAckMessage))
		if err != nil {
			a1pLog.Warn(err)
		}
	}
}

func (a *a1pClient) forwardResponseMsg(msg interface{}, messageType stream.A1SBIMessageType, rpcType stream.A1SBIRPCType) {
	a1pLog.Info("Forwarding response message")
	sbMessage := stream.NewSBStreamMessage(a.targetXAppID, messageType, rpcType, stream.PolicyManagement, msg)
	_, nbID := stream.GetStreamID(stream.A1PController, stream.GetEndpointIDWithTargetXAppID(a.targetXAppID, stream.PolicyManagement))
	err := a.streamBroker.Send(nbID, sbMessage)
	if err != nil {
		a1pLog.Warn(err)
	}
}

func (a *a1pClient) Close() {
	defer delete(a.sessions, stream.PolicyStatus)
	deleteStream(a.targetXAppID, stream.PolicyManagement, a.streamBroker)
}

var _ Client = &a1pClient{}
